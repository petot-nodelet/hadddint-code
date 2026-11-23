package web

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sukamoto/app/internal/auth"
	"github.com/sukamoto/app/internal/storage"
)

const maxPaymentProofSize = 5 << 20

var proofStoredNamePattern = regexp.MustCompile(`^[a-f0-9]{36}\.(jpg|jpeg|png|webp)$`)

func validateGuestOrder(name, contact, publicName string) string {
	if length := len([]rune(name)); length < 2 || length > 100 {
		return "Nama pemesan harus berisi 2 sampai 100 karakter."
	}
	if len(contact) < 6 || len(contact) > 254 || strings.ContainsAny(contact, "\r\n") {
		return "Masukkan email atau nomor WhatsApp yang valid."
	}
	if strings.Contains(contact, "@") {
		address, err := mail.ParseAddress(contact)
		if err != nil || address.Address != contact {
			return "Masukkan email atau nomor WhatsApp yang valid."
		}
	} else {
		digits := 0
		for _, character := range contact {
			switch {
			case character >= '0' && character <= '9':
				digits++
			case character == '+' || character == '-' || character == '(' || character == ')' || character == ' ':
			default:
				return "Masukkan email atau nomor WhatsApp yang valid."
			}
		}
		if digits < 8 || digits > 15 {
			return "Masukkan email atau nomor WhatsApp yang valid."
		}
	}
	if length := len([]rune(publicName)); length < 2 || length > 100 {
		return "Nama pasangan harus berisi 2 sampai 100 karakter."
	}
	return ""
}

func (s *Server) handleAdminLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.Auth.IsAdmin(r.Context()) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.renderAdminLogin(w, r, "", http.StatusOK)
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	password := r.PostForm.Get("password")
	user, err := s.DB.GetUserByEmail(r.Context(), email)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && (user.Role != auth.RoleAdmin || user.Status != "active" || !auth.CheckPassword(user.PasswordHash, password))) {
		s.renderAdminLogin(w, r, "Email atau password admin tidak valid.", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.fail(w, "load admin login account", err)
		return
	}
	if err := s.Auth.Login(r.Context(), user.ID, auth.RoleAdmin, user.FullName); err != nil {
		s.fail(w, "start admin session", err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) renderAdminLogin(w http.ResponseWriter, r *http.Request, message string, status int) {
	page, err := BuildPageData(r, s.Auth, "Masuk Admin | SUKAMOTO", "Masuk ke area administrasi SUKAMOTO.")
	if err != nil {
		s.fail(w, "build admin login page", err)
		return
	}
	s.render(w, r, status, AdminLoginPage(AuthPageData{Page: page, Error: message}))
}

func (s *Server) handleGuestCheckout(w http.ResponseWriter, r *http.Request) {
	orderNumber := chi.URLParam(r, "orderNumber")
	if !storage.ValidOrderNumber(orderNumber) {
		http.NotFound(w, r)
		return
	}
	order, err := s.DB.GetGuestOrderByNumber(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load guest checkout", err)
		return
	}
	if !s.validOrderAccess(w, r, order) {
		http.NotFound(w, r)
		return
	}
	methods, err := s.DB.ListPaymentMethods(r.Context())
	if err != nil {
		s.fail(w, "load checkout payment methods", err)
		return
	}
	page, err := BuildPageData(r, s.Auth, "Pembayaran Pesanan | SUKAMOTO", "Ringkasan dan pembayaran pesanan undangan.")
	if err != nil {
		s.fail(w, "build checkout page", err)
		return
	}
	s.render(w, r, http.StatusOK, OrderCheckoutPage(OrderCheckoutData{
		Page: page, Order: *order, PaymentMethods: methods,
	}))
}

func (s *Server) validOrderAccess(w http.ResponseWriter, r *http.Request, order *storage.GuestOrder) bool {
	cookie, err := r.Cookie(orderAccessCookieName(order.OrderNumber))
	if err != nil || cookie.Value == "" || len(order.DraftAccessHash) != 32 {
		return false
	}
	hash := storage.HashOrderAccessToken(cookie.Value)
	return subtle.ConstantTimeCompare(hash, order.DraftAccessHash) == 1
}

func orderAccessCookieName(orderNumber string) string {
	return "sukamoto_order_" + orderNumber
}

func (s *Server) handleGuestPaymentProof(w http.ResponseWriter, r *http.Request) {
	orderNumber := chi.URLParam(r, "orderNumber")
	if !storage.ValidOrderNumber(orderNumber) {
		http.NotFound(w, r)
		return
	}
	order, err := s.DB.GetGuestOrderByNumber(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load order for payment proof", err)
		return
	}
	if !s.validOrderAccess(w, r, order) {
		http.NotFound(w, r)
		return
	}
	if s.DB.UploadDir == "" {
		http.Error(w, "Unggah bukti pembayaran belum dikonfigurasi.", http.StatusServiceUnavailable)
		return
	}

	methodID := strings.TrimSpace(r.PostForm.Get("payment_method_id"))
	file, header, err := r.FormFile("payment_proof")
	if err != nil {
		http.Error(w, "Pilih file bukti pembayaran.", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxPaymentProofSize {
		http.Error(w, "Ukuran bukti pembayaran maksimal 5 MB.", http.StatusRequestEntityTooLarge)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	mimeType, ok := paymentProofType(ext)
	if !ok {
		http.Error(w, "Format bukti pembayaran harus JPG, JPEG, PNG, atau WEBP.", http.StatusBadRequest)
		return
	}
	declaredType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil || declaredType != mimeType {
		http.Error(w, "Tipe file bukti pembayaran tidak sesuai.", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPaymentProofSize+1))
	if err != nil {
		s.fail(w, "read payment proof", err)
		return
	}
	if len(data) == 0 || len(data) > maxPaymentProofSize {
		http.Error(w, "Ukuran bukti pembayaran maksimal 5 MB.", http.StatusRequestEntityTooLarge)
		return
	}
	if err := validatePaymentProof(ext, data, mimeType); err != nil {
		http.Error(w, "File bukan gambar yang valid atau formatnya tidak sesuai.", http.StatusBadRequest)
		return
	}

	storedName, err := writePaymentProof(s.DB.UploadDir, ext, data)
	if err != nil {
		s.fail(w, "store payment proof", err)
		return
	}
	err = s.DB.SubmitGuestPayment(r.Context(), orderNumber, methodID,
		"payment-proof"+ext, storedName, mimeType, int64(len(data)))
	if err != nil {
		if removeErr := os.Remove(filepath.Join(s.DB.UploadDir, storedName)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Printf("web: remove uncommitted payment proof: %v", removeErr)
		}
		if errors.Is(err, storage.ErrInvalidOrderTransition) || errors.Is(err, storage.ErrNotFound) {
			http.Error(w, "Pesanan tidak dapat menerima bukti pembayaran pada status saat ini.", http.StatusConflict)
			return
		}
		s.fail(w, "save payment proof", err)
		return
	}
	if order.ProofStoredName != "" && order.ProofStoredName != storedName {
		if err := os.Remove(filepath.Join(s.DB.UploadDir, order.ProofStoredName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("web: remove replaced payment proof: %v", err)
		}
	}
	http.Redirect(w, r, "/orders/"+urlPathSegment(orderNumber), http.StatusSeeOther)
}

func paymentProofType(ext string) (string, bool) {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	case ".webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func validatePaymentProof(ext string, data []byte, expectedType string) error {
	switch ext {
	case ".jpg", ".jpeg":
		if len(data) < 3 || !bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}) {
			return fmt.Errorf("invalid JPEG signature")
		}
	case ".png":
		if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
			return fmt.Errorf("invalid PNG signature")
		}
	case ".webp":
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return fmt.Errorf("invalid WEBP signature")
		}
		if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
			return fmt.Errorf("invalid WEBP RIFF length")
		}
	default:
		return fmt.Errorf("unsupported image extension")
	}
	if http.DetectContentType(data) != expectedType {
		return fmt.Errorf("image data does not match declared media type")
	}
	if ext != ".webp" {
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 1 || config.Height < 1 {
			return fmt.Errorf("invalid image data")
		}
		if (expectedType == "image/jpeg" && format != "jpeg") || (expectedType == "image/png" && format != "png") {
			return fmt.Errorf("image data does not match extension")
		}
	}
	return nil
}

func writePaymentProof(directory, ext string, data []byte) (string, error) {
	for range 5 {
		raw := make([]byte, 18)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate payment proof name: %w", err)
		}
		name := fmt.Sprintf("%x%s", raw, ext)
		file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create private payment proof: %w", err)
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(filepath.Join(directory, name))
			return "", fmt.Errorf("write private payment proof: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(filepath.Join(directory, name))
			return "", fmt.Errorf("close private payment proof: %w", err)
		}
		return name, nil
	}
	return "", fmt.Errorf("create private payment proof: could not allocate a unique file name")
}

func (s *Server) handleTrackForm(w http.ResponseWriter, r *http.Request) {
	s.renderTrackPage(w, r, TrackingPageData{}, http.StatusOK)
}

func (s *Server) handleTrackSearch(w http.ResponseWriter, r *http.Request) {
	code := storage.NormalizeTrackingCode(r.PostForm.Get("tracking_code"))
	if !storage.ValidTrackingCode(code) {
		s.renderTrackPage(w, r, TrackingPageData{
			Code: code, Error: "Format kode tidak sesuai. Periksa kembali kode pesanan Anda.",
		}, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/track/"+urlPathSegment(code), http.StatusSeeOther)
}

func (s *Server) handleTrackOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.trackLimiter.allow(r) {
		http.Error(w, "Terlalu banyak percobaan. Coba kembali beberapa saat lagi.", http.StatusTooManyRequests)
		return
	}
	code := storage.NormalizeTrackingCode(chi.URLParam(r, "code"))
	if !storage.ValidTrackingCode(code) {
		http.NotFound(w, r)
		return
	}
	order, err := s.DB.GetTrackedOrder(r.Context(), code)
	if errors.Is(err, storage.ErrNotFound) {
		s.renderTrackPage(w, r, TrackingPageData{
			Code: code, Error: "Pesanan tidak ditemukan. Periksa kode dan coba kembali.",
		}, http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, "load tracked order", err)
		return
	}
	s.renderTrackPage(w, r, TrackingPageData{Code: code, Order: order}, http.StatusOK)
}

func (s *Server) renderTrackPage(w http.ResponseWriter, r *http.Request, data TrackingPageData, status int) {
	page, err := BuildPageData(r, s.Auth, "Cek Pesanan | SUKAMOTO", "Lihat status pesanan undangan Anda.")
	if err != nil {
		s.fail(w, "build tracking page", err)
		return
	}
	data.Page = page
	s.render(w, r, status, TrackPage(data))
}

func (s *Server) handleAdminOrders(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	paymentStatus := r.URL.Query().Get("payment_status")
	workStatus := r.URL.Query().Get("work_status")
	if len([]rune(search)) > 100 {
		http.Error(w, "Kata pencarian terlalu panjang.", http.StatusBadRequest)
		return
	}
	if paymentStatus != "" && !oneOf(paymentStatus, "pending_payment", "payment_submitted", "payment_rejected", "paid", "refunded") {
		http.Error(w, "Filter status pembayaran tidak valid.", http.StatusBadRequest)
		return
	}
	if workStatus != "" && !oneOf(workStatus, "draft", "waiting_payment", "processing", "published", "expired", "cancelled") {
		http.Error(w, "Filter status pengerjaan tidak valid.", http.StatusBadRequest)
		return
	}
	orders, err := s.DB.ListOrdersForAdmin(r.Context(), search, paymentStatus, workStatus, 100, 0)
	if err != nil {
		s.fail(w, "load admin orders", err)
		return
	}
	page, err := BuildPageData(r, s.Auth, "Pesanan | Admin SUKAMOTO", "Kelola pesanan dan pembayaran.")
	if err != nil {
		s.fail(w, "build admin orders page", err)
		return
	}
	s.render(w, r, http.StatusOK, AdminOrdersPage(AdminOrdersPageData{
		Page: page, Orders: orders, Search: search,
		PaymentStatus: paymentStatus, WorkStatus: workStatus,
	}))
}

func (s *Server) handleAdminOrder(w http.ResponseWriter, r *http.Request) {
	orderNumber := chi.URLParam(r, "orderNumber")
	if !storage.ValidOrderNumber(orderNumber) {
		http.NotFound(w, r)
		return
	}
	order, err := s.DB.GetAdminOrder(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load admin order detail", err)
		return
	}
	s.renderAdminOrder(w, r, *order, "", http.StatusOK)
}

func (s *Server) renderAdminOrder(w http.ResponseWriter, r *http.Request, order storage.AdminOrder, message string, status int) {
	page, err := BuildPageData(r, s.Auth, "Detail Pesanan | Admin SUKAMOTO", "Detail pesanan pelanggan.")
	if err != nil {
		s.fail(w, "build admin order detail", err)
		return
	}
	s.render(w, r, status, AdminOrderDetailPage(AdminOrderPageData{Page: page, Order: order, Error: message}))
}

func (s *Server) handleAdminOrderNotes(w http.ResponseWriter, r *http.Request) {
	orderNumber := chi.URLParam(r, "orderNumber")
	order, err := s.DB.GetAdminOrder(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load order before note update", err)
		return
	}
	publicNote := strings.TrimSpace(r.PostForm.Get("public_note"))
	internalNote := strings.TrimSpace(r.PostForm.Get("internal_note"))
	if len([]rune(publicNote)) > 1000 || len([]rune(internalNote)) > 2000 {
		s.renderAdminOrder(w, r, *order, "Catatan terlalu panjang.", http.StatusBadRequest)
		return
	}
	if err := s.DB.UpdateAdminOrderNotes(r.Context(), orderNumber, publicNote, internalNote); err != nil {
		s.fail(w, "update admin order notes", err)
		return
	}
	http.Redirect(w, r, "/admin/orders/"+urlPathSegment(orderNumber), http.StatusSeeOther)
}

func (s *Server) handleAdminOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderNumber := chi.URLParam(r, "orderNumber")
	adminID, ok := s.Auth.CurrentUserID(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.DB.ChangeGuestOrderWorkStatus(r.Context(), orderNumber, r.PostForm.Get("work_status"), adminID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, storage.ErrInvalidOrderTransition) {
			http.Error(w, "Perubahan status tidak diizinkan pada status pesanan saat ini.", http.StatusConflict)
			return
		}
		s.fail(w, "change admin order status", err)
		return
	}
	http.Redirect(w, r, "/admin/orders/"+urlPathSegment(orderNumber), http.StatusSeeOther)
}

func (s *Server) handleAdminPaymentProof(w http.ResponseWriter, r *http.Request) {
	paymentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || paymentID < 1 {
		http.NotFound(w, r)
		return
	}
	proof, err := s.DB.Queries.GetPaymentProofForAdmin(r.Context(), paymentID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load admin payment proof", err)
		return
	}
	if !proofStoredNamePattern.MatchString(proof.ProofStoredName) || s.DB.UploadDir == "" {
		http.NotFound(w, r)
		return
	}
	if proof.ProofMimeType != "image/jpeg" && proof.ProofMimeType != "image/png" && proof.ProofMimeType != "image/webp" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(s.DB.UploadDir, proof.ProofStoredName))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "open admin payment proof", err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		s.fail(w, "stat admin payment proof", err)
		return
	}
	w.Header().Set("Content-Type", proof.ProofMimeType)
	w.Header().Set("Content-Disposition", `inline; filename="payment-proof"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "payment-proof", stat.ModTime(), file)
}

func (s *Server) handleAdminPaymentApprove(w http.ResponseWriter, r *http.Request) {
	s.reviewAdminPayment(w, r, true)
}

func (s *Server) handleAdminPaymentReject(w http.ResponseWriter, r *http.Request) {
	s.reviewAdminPayment(w, r, false)
}

func (s *Server) reviewAdminPayment(w http.ResponseWriter, r *http.Request, approve bool) {
	paymentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || paymentID < 1 {
		http.NotFound(w, r)
		return
	}
	orderNumber, err := s.DB.GetOrderNumberByPaymentID(r.Context(), paymentID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load payment order number", err)
		return
	}
	adminID, ok := s.Auth.CurrentUserID(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	reason := strings.TrimSpace(r.PostForm.Get("reason"))
	if !approve && (len([]rune(reason)) < 3 || len([]rune(reason)) > 500) {
		order, err := s.DB.GetAdminOrder(r.Context(), orderNumber)
		if err != nil {
			s.fail(w, "reload order after invalid rejection reason", err)
			return
		}
		s.renderAdminOrder(w, r, *order, "Alasan penolakan harus berisi 3 sampai 500 karakter.", http.StatusBadRequest)
		return
	}
	if err := s.DB.ReviewGuestPayment(r.Context(), paymentID, adminID, approve, reason); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, storage.ErrInvalidOrderTransition) {
			http.Error(w, "Pembayaran sudah ditinjau atau statusnya tidak valid.", http.StatusConflict)
			return
		}
		s.fail(w, "review admin payment", err)
		return
	}
	http.Redirect(w, r, "/admin/orders/"+urlPathSegment(orderNumber), http.StatusSeeOther)
}

func urlPathSegment(value string) string {
	return url.PathEscape(value)
}
