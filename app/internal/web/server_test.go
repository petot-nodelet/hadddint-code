package web

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sukamoto/app/internal/auth"
	"github.com/sukamoto/app/internal/storage"
)

var csrfInput = regexp.MustCompile(`name="csrf_token" value="([a-f0-9]{64})"`)
var nextInput = regexp.MustCompile(`name="next" value="([^"]*)"`)
var testOrderNumber = regexp.MustCompile(`SKM-ORD-[0-9]{8}-[A-Z2-7]{8}`)
var testTrackingCode = regexp.MustCompile(`SKM-[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}`)

func testServer(t *testing.T) (*storage.DB, *httptest.Server) {
	t.Helper()
	db, err := storage.OpenWithUploads(context.Background(), t.TempDir()+"/test.db", t.TempDir()+"/uploads")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(db.Queries, "test_session", time.Hour, false)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	server, err := NewServer(db, manager, "../../..")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Routes())
	t.Cleanup(func() {
		ts.Close()
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db, ts
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 5 * time.Second}
}

func getCSRF(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", target, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	match := csrfInput.FindStringSubmatch(string(body))
	if len(match) != 2 {
		t.Fatalf("CSRF token not found in %s response body %q", target, body)
	}
	return match[1]
}

func postForm(t *testing.T, client *http.Client, target string, values url.Values) *http.Response {
	t.Helper()
	response, err := client.PostForm(target, values)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func createGuestOrderForTest(t *testing.T, client *http.Client, server *httptest.Server, templateID, packageID string) (string, string, string) {
	t.Helper()
	path := "/templates/" + templateID + "/use?package=" + url.QueryEscape(packageID)
	csrf := getCSRF(t, client, server.URL+path)
	response := postForm(t, client, server.URL+"/templates/"+templateID+"/use", url.Values{
		"csrf_token":       {csrf},
		"package_id":       {packageID},
		"customer_name":    {"Private Customer"},
		"customer_contact": {"private@example.com"},
		"public_name":      {"Ayu & Bayu"},
	})
	body := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("guest order checkout status = %d, want 200: %s", response.StatusCode, body)
	}
	orderMatch := testOrderNumber.FindString(response.Request.URL.Path)
	if orderMatch == "" {
		t.Fatalf("checkout URL has no generated order number: %s", response.Request.URL)
	}
	trackingMatch := testTrackingCode.FindString(body)
	if trackingMatch == "" {
		t.Fatalf("checkout page has no tracking code: %s", body)
	}
	return orderMatch, trackingMatch, body
}

func validPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func postPaymentProof(t *testing.T, client *http.Client, target, csrf, method, filename, contentType string, data []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", csrf); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("payment_method_id", method); err != nil {
		t.Fatal(err)
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="payment_proof"; filename="`+filename+`"`)
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestCustomerAccountRoutesAreDeprecated(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	for _, path := range []string{"/register", "/dashboard"} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusGone {
			t.Errorf("GET %s status = %d, want 410", path, response.StatusCode)
		}
	}
	response, err := client.Get(server.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/admin/login" {
		t.Fatalf("legacy login URL ended at %s with status %d, want admin login 200", response.Request.URL, response.StatusCode)
	}
	if strings.Contains(strings.ToLower(readBody(t, response)), "daftar") {
		t.Fatal("admin login page exposes customer registration")
	}
}

func TestLoginRejectsInvalidCSRF(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	response, err := client.PostForm(server.URL+"/admin/login", url.Values{
		"csrf_token": {"invalid"},
		"email":      {"a@example.com"},
		"password":   {"password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("login without valid CSRF returned %d, want 403", response.StatusCode)
	}
}

func TestAuthenticationAndRoleSeparation(t *testing.T) {
	db, server := testServer(t)
	ctx := context.Background()
	adminHash, err := auth.HashPassword("admin-password")
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := db.CreateUser(ctx, "Admin", "admin@example.com", "", adminHash, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	customerHash, err := auth.HashPassword("customer-password")
	if err != nil {
		t.Fatal(err)
	}
	customerID, err := db.CreateUser(ctx, "Customer", "customer@example.com", "", customerHash, auth.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}

	customerClient := newTestClient(t)
	customerCSRF := getCSRF(t, customerClient, server.URL+"/admin/login")
	response := postForm(t, customerClient, server.URL+"/admin/login", url.Values{
		"csrf_token": {customerCSRF}, "email": {"customer@example.com"}, "password": {"customer-password"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("customer credentials at admin login returned %d, want 401", response.StatusCode)
	}

	adminClient := newTestClient(t)
	if err := login(t, adminClient, server.URL, "admin@example.com", "admin-password"); err != nil {
		t.Fatal(err)
	}
	response, err = adminClient.Get(server.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin access to admin returned %d, want 200", response.StatusCode)
	}
	response, err = adminClient.Get(server.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusGone {
		t.Fatalf("legacy customer dashboard returned %d, want 410", response.StatusCode)
	}

	if adminID == customerID {
		t.Fatal("admin and customer unexpectedly share an ID")
	}
}

func login(t *testing.T, client *http.Client, baseURL, email, password string) error {
	t.Helper()
	csrf := getCSRF(t, client, baseURL+"/admin/login")
	response, err := client.PostForm(baseURL+"/admin/login", url.Values{
		"csrf_token": {csrf},
		"email":      {email},
		"password":   {password},
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("login %s status = %d, want 200", email, response.StatusCode)
	}
	return nil
}

func TestLoginSessionCookieIsHttpOnly(t *testing.T) {
	_, server := testServer(t)
	response, err := http.Get(server.URL + "/admin/login")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	for _, cookie := range response.Cookies() {
		if cookie.Name == "test_session" && cookie.HttpOnly {
			return
		}
	}
	t.Fatal("session cookie is not HttpOnly")
}

func TestLandingPageAndStaticStylesheet(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	response, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("landing page status = %d, want 200", response.StatusCode)
	}
	for _, expected := range []string{"SUKAMOTO", "Floral Elegant", "Silver"} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("landing page missing database-backed content %q", expected)
		}
	}
	response, err = client.Get(server.URL + "/static/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("static stylesheet status = %d, want 200", response.StatusCode)
	}
}

func TestTemplateCatalogFiltersSortingAndPagination(t *testing.T) {
	db, server := testServer(t)
	client := newTestClient(t)
	base := server.URL + "/templates"

	response, err := client.Get(base + "?package=gold")
	if err != nil {
		t.Fatal(err)
	}
	goldHTML, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(goldHTML), "Golden Kirana") || strings.Contains(string(goldHTML), "Floral Elegant") {
		t.Fatalf("Gold catalog filter returned status %d and body %q", response.StatusCode, goldHTML)
	}

	response, err = client.Get(base + "?category=japanese")
	if err != nil {
		t.Fatal(err)
	}
	japaneseHTML, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(japaneseHTML), "2 desain ditemukan") || !strings.Contains(string(japaneseHTML), "Japanese Garden") || !strings.Contains(string(japaneseHTML), "Sakura Premium") {
		t.Fatalf("Japanese category did not return its two database-backed designs")
	}

	response, err = client.Get(base + "?q=sakura")
	if err != nil {
		t.Fatal(err)
	}
	searchHTML, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(searchHTML), "Sakura Premium") || !strings.Contains(string(searchHTML), "Japanese Garden") {
		t.Fatalf("search did not match template names/descriptions: %s", searchHTML)
	}

	response, err = client.Get(base + "?sort=name_desc")
	if err != nil {
		t.Fatal(err)
	}
	sortedHTML, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(sortedHTML), "Sakura Premium") > strings.Index(string(sortedHTML), "Minimalist Romance") {
		t.Fatal("name-descending sort did not place Sakura before Minimalist")
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("X%02d", i)
		name := fmt.Sprintf("Extra %02d", i)
		if _, err := db.SQL.Exec(`INSERT INTO templates (id, name, category, description, thumbnail, config, features, sort_order) VALUES (?, ?, 'extra', 'Pagination fixture', '/index/photo-1511285560929-80b456fea0bc.jpg', '{}', '[]', ?)`, id, name, 20+i); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`INSERT INTO template_packages (template_id, package_id) VALUES (?, 'silver')`, id); err != nil {
			t.Fatal(err)
		}
	}
	response, err = client.Get(base + "?page=2&package=silver&sort=name_asc")
	if err != nil {
		t.Fatal(err)
	}
	pageTwo, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(pageTwo), "Extra 09") || strings.Contains(string(pageTwo), "Golden Kirana") {
		t.Fatalf("catalog pagination did not preserve filters: status %d", response.StatusCode)
	}
	if !strings.Contains(string(pageTwo), "page=1") || !strings.Contains(string(pageTwo), "page=2") {
		t.Fatal("catalog pagination links do not include prior and current pages")
	}
}

func TestTemplatePreviewAndPackageAwareSelection(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	response, err := client.Get(server.URL + "/templates/G01")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("template detail status = %d, want 200", response.StatusCode)
	}
	for _, expected := range []string{"Golden Kirana", "preview-cover-fullbleed", "Alex Brush", "Amplop digital", "Paket yang didukung"} {
		if !strings.Contains(string(detail), expected) {
			t.Errorf("template detail/renderer missing %q", expected)
		}
	}
}

func loginTestAdmin(t *testing.T, db *storage.DB, server *httptest.Server, client *http.Client) int64 {
	t.Helper()
	hash, err := auth.HashPassword("admin-password")
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := db.CreateUser(context.Background(), "Admin", "admin@example.com", "", hash, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := login(t, client, server.URL, "admin@example.com", "admin-password"); err != nil {
		t.Fatal(err)
	}
	return adminID
}

func TestGuestPaymentUploadAndAdminReview(t *testing.T) {
	db, server := testServer(t)
	customer := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, customer, server, "S01", "silver")
	csrf := getCSRF(t, customer, server.URL+"/orders/"+orderNumber)
	proof := validPNG(t)
	response := postPaymentProof(t, customer, server.URL+"/orders/"+orderNumber+"/payment",
		csrf, "bank_transfer", "../../receipt.png", "image/png", proof)
	body := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "sedang diperiksa") {
		t.Fatalf("valid payment proof submission status = %d, body = %s", response.StatusCode, body)
	}

	var paymentID int64
	var paymentStatus, storedName, storedMIME string
	var fileSize int64
	var paymentUserID sql.NullInt64
	if err := db.SQL.QueryRow(`SELECT id, status, proof_stored_name, proof_mime_type, proof_size_bytes, user_id FROM payments WHERE order_id = (SELECT id FROM orders WHERE order_number = ?)`, orderNumber).Scan(&paymentID, &paymentStatus, &storedName, &storedMIME, &fileSize, &paymentUserID); err != nil {
		t.Fatal(err)
	}
	if paymentStatus != "payment_submitted" || storedMIME != "image/png" || fileSize != int64(len(proof)) || paymentUserID.Valid {
		t.Fatalf("payment metadata status=%s MIME=%s size=%d user=%v", paymentStatus, storedMIME, fileSize, paymentUserID)
	}
	if !proofStoredNamePattern.MatchString(storedName) {
		t.Fatalf("stored proof name is not random and path-safe: %q", storedName)
	}
	proofPath := filepath.Join(db.UploadDir, storedName)
	stat, err := os.Stat(proofPath)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0o600 {
		t.Fatalf("payment proof permissions = %o, want 600", stat.Mode().Perm())
	}

	unauthorized := newTestClient(t)
	response, err = unauthorized.Get(server.URL + "/admin/payments/" + strconv.FormatInt(paymentID, 10) + "/proof")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Request.URL.Path != "/admin/login" {
		t.Fatalf("unauthorized proof request ended at %s, want admin login", response.Request.URL)
	}

	admin := newTestClient(t)
	adminID := loginTestAdmin(t, db, server, admin)
	response, err = admin.Get(server.URL + "/admin/payments/" + strconv.FormatInt(paymentID, 10) + "/proof")
	if err != nil {
		t.Fatal(err)
	}
	adminProof, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/png" || !bytes.Equal(adminProof, proof) {
		t.Fatalf("admin proof response status=%d type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	guestCSRF := getCSRF(t, customer, server.URL+"/track")
	response = postForm(t, customer, server.URL+"/admin/payments/"+strconv.FormatInt(paymentID, 10)+"/approve", url.Values{
		"csrf_token": {guestCSRF},
	})
	response.Body.Close()
	if response.Request.URL.Path != "/admin/login" {
		t.Fatalf("unauthenticated payment review ended at %s, want admin login", response.Request.URL)
	}

	adminCSRF := getCSRF(t, admin, server.URL+"/admin/orders/"+orderNumber)
	response = postForm(t, admin, server.URL+"/admin/payments/"+strconv.FormatInt(paymentID, 10)+"/approve", url.Values{
		"csrf_token": {adminCSRF},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/admin/orders/"+orderNumber) {
		t.Fatalf("payment approval ended at %s with status %d", response.Request.URL, response.StatusCode)
	}
	var orderPaymentStatus, workStatus string
	if err := db.SQL.QueryRow(`SELECT payment_status, work_status FROM orders WHERE order_number = ?`, orderNumber).Scan(&orderPaymentStatus, &workStatus); err != nil {
		t.Fatal(err)
	}
	if orderPaymentStatus != "paid" || workStatus != "processing" {
		t.Fatalf("approved order states = payment:%s work:%s", orderPaymentStatus, workStatus)
	}
	var reviewCount int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM payment_reviews WHERE payment_id = ? AND decision = 'approved' AND admin_id = ?`, paymentID, adminID).Scan(&reviewCount); err != nil {
		t.Fatal(err)
	}
	if reviewCount != 1 {
		t.Fatalf("approved review records = %d, want 1", reviewCount)
	}
	response = postForm(t, admin, server.URL+"/admin/payments/"+strconv.FormatInt(paymentID, 10)+"/approve", url.Values{
		"csrf_token": {adminCSRF},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("double approval returned %d, want 409", response.StatusCode)
	}
}

func TestRejectedPaymentCanBeResubmitted(t *testing.T) {
	db, server := testServer(t)
	customer := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, customer, server, "S01", "silver")
	csrf := getCSRF(t, customer, server.URL+"/orders/"+orderNumber)
	response := postPaymentProof(t, customer, server.URL+"/orders/"+orderNumber+"/payment",
		csrf, "bank_transfer", "proof.png", "image/png", validPNG(t))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initial proof upload status = %d", response.StatusCode)
	}
	var paymentID int64
	var firstStoredName string
	if err := db.SQL.QueryRow(`SELECT id, proof_stored_name FROM payments WHERE order_id = (SELECT id FROM orders WHERE order_number = ?)`, orderNumber).Scan(&paymentID, &firstStoredName); err != nil {
		t.Fatal(err)
	}

	admin := newTestClient(t)
	loginTestAdmin(t, db, server, admin)
	adminCSRF := getCSRF(t, admin, server.URL+"/admin/orders/"+orderNumber)
	response = postForm(t, admin, server.URL+"/admin/payments/"+strconv.FormatInt(paymentID, 10)+"/reject", url.Values{
		"csrf_token": {adminCSRF}, "reason": {"Nominal pembayaran belum sesuai."},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("payment rejection status = %d", response.StatusCode)
	}

	checkoutCSRF := getCSRF(t, customer, server.URL+"/orders/"+orderNumber)
	checkout, err := customer.Get(server.URL + "/orders/" + orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	checkoutBody := readBody(t, checkout)
	checkout.Body.Close()
	if !strings.Contains(checkoutBody, "Nominal pembayaran belum sesuai") {
		t.Fatal("checkout did not show the rejection reason")
	}
	response = postPaymentProof(t, customer, server.URL+"/orders/"+orderNumber+"/payment",
		checkoutCSRF, "qris", "second-proof.png", "image/png", validPNG(t))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("payment proof reupload status = %d", response.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(db.UploadDir, firstStoredName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replaced proof still exists or stat failed: %v", err)
	}
	var paymentStatus, orderPaymentStatus, workStatus string
	var newStoredName string
	if err := db.SQL.QueryRow(`SELECT pay.status, pay.proof_stored_name, o.payment_status, o.work_status FROM payments AS pay JOIN orders AS o ON o.id = pay.order_id WHERE o.order_number = ?`, orderNumber).Scan(&paymentStatus, &newStoredName, &orderPaymentStatus, &workStatus); err != nil {
		t.Fatal(err)
	}
	if paymentStatus != "payment_submitted" || orderPaymentStatus != "payment_submitted" || workStatus != "waiting_payment" || newStoredName == firstStoredName {
		t.Fatalf("reupload states payment=%s order_payment=%s work=%s", paymentStatus, orderPaymentStatus, workStatus)
	}
	var rejectionCount int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM payment_reviews WHERE payment_id = ? AND decision = 'rejected'`, paymentID).Scan(&rejectionCount); err != nil {
		t.Fatal(err)
	}
	if rejectionCount != 1 {
		t.Fatalf("rejection audit records = %d, want 1", rejectionCount)
	}
}

func TestPaymentProofRejectsInvalidFilesAndOversize(t *testing.T) {
	db, server := testServer(t)
	customer := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, customer, server, "S01", "silver")
	csrf := getCSRF(t, customer, server.URL+"/orders/"+orderNumber)
	tests := []struct {
		name        string
		filename    string
		contentType string
		data        []byte
		wantStatus  int
	}{
		{name: "unsupported extension", filename: "proof.exe", contentType: "application/octet-stream", data: []byte("not an image"), wantStatus: http.StatusBadRequest},
		{name: "MIME mismatch", filename: "proof.png", contentType: "image/jpeg", data: validPNG(t), wantStatus: http.StatusBadRequest},
		{name: "invalid signature", filename: "proof.png", contentType: "image/png", data: []byte("not an image"), wantStatus: http.StatusBadRequest},
		{name: "invalid webp payload", filename: "proof.webp", contentType: "image/webp", data: []byte("RIFF\x04\x00\x00\x00WEBP"), wantStatus: http.StatusBadRequest},
		{name: "oversized image", filename: "large.png", contentType: "image/png", data: append(validPNG(t), bytes.Repeat([]byte{0}, maxPaymentProofSize)...), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := postPaymentProof(t, customer, server.URL+"/orders/"+orderNumber+"/payment",
				csrf, "bank_transfer", test.filename, test.contentType, test.data)
			response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("upload status = %d, want %d", response.StatusCode, test.wantStatus)
			}
		})
	}
	var paymentCount int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM payments`).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 0 {
		t.Fatalf("invalid proof uploads created %d payment records", paymentCount)
	}
	entries, err := os.ReadDir(db.UploadDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid proof uploads left %d files in private storage", len(entries))
	}
}

func TestGuestOrderCreationAndTracking(t *testing.T) {
	db, server := testServer(t)
	client := newTestClient(t)

	csrf := getCSRF(t, client, server.URL+"/templates/G01/use?package=gold")
	response := postForm(t, client, server.URL+"/templates/G01/use", url.Values{
		"csrf_token": {csrf}, "package_id": {"silver"},
		"customer_name": {"Senja Tester"}, "customer_contact": {"senja@example.com"},
		"public_name": {"Raka & Senja"},
	})
	invalidSelection := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(invalidSelection, "tidak didukung") {
		t.Fatalf("unsupported package selection returned %d and %q", response.StatusCode, invalidSelection)
	}
	response = postForm(t, client, server.URL+"/templates/G01/use", url.Values{
		"csrf_token": {csrf}, "package_id": {"gold"},
		"customer_name": {"Senja Tester"}, "customer_contact": {"senja@example.com"},
		"public_name": {"Raka & Senja"},
	})
	checkout, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Request.URL.Path, "/orders/SKM-ORD-") {
		t.Fatalf("valid guest order did not reach checkout: %s (%d)", response.Request.URL, response.StatusCode)
	}
	if !strings.Contains(string(checkout), "Golden Kirana") || !strings.Contains(string(checkout), "Gold") || !strings.Contains(string(checkout), "Kode pelacakan") {
		t.Fatal("checkout did not show the selected template, package, and tracking code")
	}
	var selectedPackage, selectedTemplate, orderNumber, trackingCode, paymentStatus, workStatus string
	var selectedPrice int64
	var userID sql.NullInt64
	if err := db.SQL.QueryRow(`SELECT package_id, template_id, order_number, tracking_code, price_cents, payment_status, work_status, user_id FROM orders WHERE order_number = ?`, response.Request.URL.Path[len("/orders/"):]).Scan(&selectedPackage, &selectedTemplate, &orderNumber, &trackingCode, &selectedPrice, &paymentStatus, &workStatus, &userID); err != nil {
		t.Fatal(err)
	}
	if selectedPackage != "gold" || selectedTemplate != "G01" {
		t.Fatalf("saved order selection = %s/%s, want gold/G01", selectedPackage, selectedTemplate)
	}
	if selectedPrice != 35000000 || paymentStatus != "pending_payment" || workStatus != "waiting_payment" || userID.Valid {
		t.Fatalf("guest order state = price:%d payment:%s work:%s user:%v", selectedPrice, paymentStatus, workStatus, userID)
	}
	if !storage.ValidTrackingCode(trackingCode) || orderNumber == "" {
		t.Fatalf("generated order codes are invalid: %q, %q", orderNumber, trackingCode)
	}
	if !strings.Contains(orderNumber, "SKM-ORD-") {
		t.Fatalf("order number unexpectedly derived from database ID: %q", orderNumber)
	}

	trackCSRF := getCSRF(t, client, server.URL+"/track")
	response = postForm(t, client, server.URL+"/track", url.Values{
		"csrf_token": {trackCSRF}, "tracking_code": {strings.ToLower(trackingCode)},
	})
	trackingPage, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/track/"+trackingCode) {
		t.Fatalf("tracking lookup ended at %s with status %d", response.Request.URL, response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("tracking response is missing private-cache/referrer protections")
	}
	for _, private := range []string{"Senja Tester", "senja@example.com"} {
		if strings.Contains(string(trackingPage), private) {
			t.Errorf("public tracking page exposed private customer data %q", private)
		}
	}
	if !strings.Contains(string(trackingPage), "Raka &amp; Senja") || !strings.Contains(string(trackingPage), "Menunggu pembayaran") {
		t.Fatal("public tracking page did not show approved order identity and status")
	}

	otherClient := newTestClient(t)
	response, err = otherClient.Get(server.URL + "/orders/" + orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("order checkout without the private access cookie returned %d, want 404", response.StatusCode)
	}
}

func TestTemplateDetailRejectsUnknownTemplateAndFilters(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	for _, target := range []string{
		server.URL + "/templates/missing",
		server.URL + "/templates?package=unknown",
		server.URL + "/templates?sort=price",
		server.URL + "/templates?page=not-a-number",
	} {
		response, err := client.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 404 or 400", target, response.StatusCode)
		}
	}
}
