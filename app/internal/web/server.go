package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sukamoto/app/internal/auth"
	"github.com/sukamoto/app/internal/storage"
)

type Server struct {
	DB            *storage.DB
	Auth          *auth.Manager
	StaticDir     string
	SecureCookies bool
	trackLimiter  *trackLimiter
}

type redactingLogFormatter struct {
	formatter *middleware.DefaultLogFormatter
}

func (f redactingLogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	if strings.HasPrefix(r.URL.Path, "/track/") {
		redacted := r.Clone(r.Context())
		redacted.URL = new(url.URL)
		*redacted.URL = *r.URL
		redacted.URL.Path = "/track/{code}"
		redacted.URL.RawPath = ""
		redacted.URL.RawQuery = ""
		redacted.RequestURI = "/track/{code}"
		r = redacted
	}
	return f.formatter.NewLogEntry(r)
}

func NewServer(db *storage.DB, manager *auth.Manager, staticDir string) (*Server, error) {
	if db == nil || manager == nil {
		return nil, fmt.Errorf("web: database and auth manager are required")
	}
	if staticDir == "" {
		staticDir = ".."
	}
	absoluteStaticDir, err := filepath.Abs(staticDir)
	if err != nil {
		return nil, fmt.Errorf("web: resolve static directory: %w", err)
	}
	return &Server{
		DB: db, Auth: manager, StaticDir: absoluteStaticDir,
		trackLimiter: newTrackLimiter(),
	}, nil
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(redactingLogFormatter{
		formatter: &middleware.DefaultLogFormatter{Logger: log.Default(), NoColor: true},
	}))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(s.Auth.Session.LoadAndSave)
	r.Use(s.Auth.CSRFMiddleware)

	r.Get("/", s.handleLanding)
	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	})
	r.Method(http.MethodPost, "/login", http.NotFoundHandler())
	r.Method(http.MethodGet, "/register", http.HandlerFunc(gone))
	r.Method(http.MethodPost, "/register", http.HandlerFunc(gone))
	r.Method(http.MethodGet, "/dashboard", http.HandlerFunc(gone))
	r.Get("/admin/login", s.handleAdminLoginPage)
	r.Post("/admin/login", s.handleAdminLogin)
	r.With(s.requireRole(auth.RoleAdmin)).Post("/logout", s.handleLogout)
	r.With(s.requireRole(auth.RoleAdmin)).Get("/admin", s.handleAdminDashboard)
	r.With(s.requireRole(auth.RoleAdmin)).Get("/admin/orders", s.handleAdminOrders)
	r.With(s.requireRole(auth.RoleAdmin)).Get("/admin/orders/{orderNumber}", s.handleAdminOrder)
	r.With(s.requireRole(auth.RoleAdmin)).Post("/admin/orders/{orderNumber}/notes", s.handleAdminOrderNotes)
	r.With(s.requireRole(auth.RoleAdmin)).Post("/admin/orders/{orderNumber}/status", s.handleAdminOrderStatus)
	r.With(s.requireRole(auth.RoleAdmin)).Get("/admin/payments/{id}/proof", s.handleAdminPaymentProof)
	r.With(s.requireRole(auth.RoleAdmin)).Post("/admin/payments/{id}/approve", s.handleAdminPaymentApprove)
	r.With(s.requireRole(auth.RoleAdmin)).Post("/admin/payments/{id}/reject", s.handleAdminPaymentReject)
	r.Get("/templates", s.handleTemplateCatalog)
	r.Get("/templates/{id}", s.handleTemplateDetail)
	r.Get("/templates/{id}/use", s.handleTemplateSelectionPage)
	r.Post("/templates/{id}/use", s.handleTemplateUse)
	r.Get("/orders/{orderNumber}", s.handleGuestCheckout)
	r.Post("/orders/{orderNumber}/payment", s.handleGuestPaymentProof)
	r.Get("/track", s.handleTrackForm)
	r.Post("/track", s.handleTrackSearch)
	r.Get("/track/{code}", s.handleTrackOrder)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(s.StaticDir, "app", "static")))))
	r.Handle("/index/*", http.StripPrefix("/index/", http.FileServer(http.Dir(filepath.Join(s.StaticDir, "index")))))
	return r
}

func gone(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Fitur akun pelanggan sudah tidak tersedia. Pesanan dapat dibuat tanpa akun.", http.StatusGone)
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	page, err := BuildPageData(r, s.Auth, "SUKAMOTO | Undangan Pernikahan Digital", "Undangan digital elegan dan personal bersama SUKAMOTO.")
	if err != nil {
		s.fail(w, "build landing page", err)
		return
	}
	templates, err := s.DB.ListTemplates(r.Context(), "", "", "", "featured", 3, 0)
	if err != nil {
		s.fail(w, "load featured templates", err)
		return
	}
	packages, err := s.DB.ListPackages(r.Context())
	if err != nil {
		s.fail(w, "load packages", err)
		return
	}
	cards := make([]PackageCard, 0, len(packages))
	for _, pkg := range packages {
		cards = append(cards, PackageCard{Pkg: pkg, Features: packageFeatures(pkg.ID)})
	}
	s.render(w, r, http.StatusOK, LandingPage(LandingData{Page: page, Templates: templates, Packages: cards}))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.Logout(r.Context()); err != nil {
		s.fail(w, "end session", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := s.Auth.CurrentUserID(r.Context())
			if !ok {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}
			if s.Auth.CurrentRole(r.Context()) != role {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			user, err := s.DB.GetUserByID(r.Context(), userID)
			if errors.Is(err, storage.ErrNotFound) || (err == nil && (user.Role != auth.RoleAdmin || user.Status != "active")) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			if err != nil {
				s.fail(w, "validate administrator session", err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	page, err := BuildPageData(r, s.Auth, "Admin | SUKAMOTO", "Area administrasi SUKAMOTO.")
	if err != nil {
		s.fail(w, "build admin dashboard", err)
		return
	}
	s.render(w, r, http.StatusOK, AdminDashboard(page, s.Auth.CurrentName(r.Context())))
}

func (s *Server) handleTemplateCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	packages, err := s.DB.ListPackages(ctx)
	if err != nil {
		s.fail(w, "load template catalog packages", err)
		return
	}
	categories, err := s.DB.ListTemplateCategories(ctx)
	if err != nil {
		s.fail(w, "load template categories", err)
		return
	}
	pageData, err := BuildPageData(r, s.Auth, "Katalog Template | SUKAMOTO", "Jelajahi template undangan pernikahan SUKAMOTO.")
	if err != nil {
		s.fail(w, "build template catalog page", err)
		return
	}

	packageID := strings.TrimSpace(r.URL.Query().Get("package"))
	if packageID != "" && !containsPackage(packages, packageID) {
		http.Error(w, "Paket tidak dikenal", http.StatusBadRequest)
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category != "" && !containsCategory(categories, category) {
		http.Error(w, "Kategori tidak dikenal", http.StatusBadRequest)
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(search)) > 100 {
		http.Error(w, "Kata pencarian terlalu panjang", http.StatusBadRequest)
		return
	}
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "featured"
	}
	if !oneOf(sortBy, "featured", "name_asc", "name_desc", "category") {
		http.Error(w, "Pengurutan tidak dikenal", http.StatusBadRequest)
		return
	}
	currentPage := 1
	if rawPage := r.URL.Query().Get("page"); rawPage != "" {
		parsedPage, err := strconv.Atoi(rawPage)
		if err != nil || parsedPage < 1 {
			http.Error(w, "Nomor halaman tidak valid", http.StatusBadRequest)
			return
		}
		currentPage = parsedPage
	}
	const pageSize int64 = 9
	total, err := s.DB.CountTemplates(ctx, packageID, category, search)
	if err != nil {
		s.fail(w, "count catalog templates", err)
		return
	}
	totalPages := int((total + pageSize - 1) / pageSize)
	if currentPage > totalPages && totalPages > 0 {
		currentPage = totalPages
	}
	if totalPages == 0 {
		currentPage = 1
	}
	offset := int64(currentPage-1) * pageSize
	templates, err := s.DB.ListTemplates(ctx, packageID, category, search, sortBy, pageSize, offset)
	if err != nil {
		s.fail(w, "load catalog templates", err)
		return
	}

	pageURL := func(page int) string {
		query := url.Values{}
		if packageID != "" {
			query.Set("package", packageID)
		}
		if category != "" {
			query.Set("category", category)
		}
		if search != "" {
			query.Set("q", search)
		}
		if sortBy != "featured" {
			query.Set("sort", sortBy)
		}
		query.Set("page", strconv.Itoa(page))
		return "/templates?" + query.Encode()
	}
	data := CatalogData{
		Page: pageData, Templates: templates, Packages: packages, Categories: categories,
		PackageID: packageID, Category: category, Search: search, SortBy: sortBy,
		CurrentPage: currentPage, TotalPages: totalPages, TotalTemplates: total,
	}
	if currentPage > 1 {
		data.PreviousURL = pageURL(currentPage - 1)
	}
	if currentPage < totalPages {
		data.NextURL = pageURL(currentPage + 1)
	}
	if totalPages > 0 {
		start := currentPage - 3
		if start < 1 {
			start = 1
		}
		end := start + 6
		if end > totalPages {
			end = totalPages
			start = end - 6
			if start < 1 {
				start = 1
			}
		}
		for page := start; page <= end; page++ {
			data.PageLinks = append(data.PageLinks, CatalogPageLink{Number: page, URL: pageURL(page)})
		}
	}
	s.render(w, r, http.StatusOK, TemplateCatalogPage(data))
}

func (s *Server) handleTemplateDetail(w http.ResponseWriter, r *http.Request) {
	template, err := s.DB.GetTemplate(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load template detail", err)
		return
	}
	config, err := decodePreviewConfig(*template)
	if err != nil {
		s.fail(w, "decode template preview", err)
		return
	}
	page, err := BuildPageData(r, s.Auth, template.Name+" | SUKAMOTO", template.Description)
	if err != nil {
		s.fail(w, "build template detail page", err)
		return
	}
	thumbnail := config.Media.Cover
	if thumbnail == "" {
		thumbnail = template.Thumbnail
	}
	sample := InvitationSample{
		Couple: "Raka & Senja", Subheading: "Dengan penuh kebahagiaan",
		Date: "Sabtu, 12 Desember 2026", Location: "Bandung, Jawa Barat",
		Thumbnail: thumbnail,
	}
	s.render(w, r, http.StatusOK, TemplateDetailPage(TemplateDetailData{
		Page: page, Template: *template, Config: config, Sample: sample,
	}))
}

func (s *Server) handleTemplateSelectionPage(w http.ResponseWriter, r *http.Request) {
	template, packageOption, ok := s.validateTemplatePackage(w, r, chi.URLParam(r, "id"), r.URL.Query().Get("package"))
	if !ok {
		return
	}
	page, err := BuildPageData(r, s.Auth, "Konfirmasi Template | SUKAMOTO", "Konfirmasi pilihan template undangan.")
	if err != nil {
		s.fail(w, "build template selection page", err)
		return
	}
	s.render(w, r, http.StatusOK, TemplateSelectionPage(TemplateSelectionData{
		Page: page, Template: *template, Package: packageOption,
	}))
}

func (s *Server) handleTemplateUse(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "id")
	packageID := strings.TrimSpace(r.PostForm.Get("package_id"))
	template, packageOption, ok := s.validateTemplatePackage(w, r, templateID, packageID)
	if !ok {
		return
	}
	customerName := strings.TrimSpace(r.PostForm.Get("customer_name"))
	customerContact := strings.TrimSpace(r.PostForm.Get("customer_contact"))
	publicName := strings.TrimSpace(r.PostForm.Get("public_name"))
	if message := validateGuestOrder(customerName, customerContact, publicName); message != "" {
		page, err := BuildPageData(r, s.Auth, "Mulai Pesanan | SUKAMOTO", "Buat pesanan undangan tanpa akun.")
		if err != nil {
			s.fail(w, "build invalid guest order page", err)
			return
		}
		s.render(w, r, http.StatusBadRequest, TemplateSelectionPage(TemplateSelectionData{
			Page: page, Template: *template, Package: packageOption,
			CustomerName: customerName, CustomerContact: customerContact,
			PublicName: publicName, Error: message,
		}))
		return
	}

	token, accessHash, err := storage.NewOrderAccessToken()
	if err != nil {
		s.fail(w, "create guest order access token", err)
		return
	}
	order, err := s.DB.CreateGuestOrder(r.Context(), packageOption.ID, template.ID,
		customerName, customerContact, publicName, accessHash)
	if errors.Is(err, storage.ErrOrderUnavailable) {
		http.Error(w, "Paket atau template tidak lagi tersedia. Silakan pilih kembali.", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "create guest order", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: orderAccessCookieName(order.OrderNumber), Value: token,
		Path: "/orders/" + order.OrderNumber, HttpOnly: true,
		Secure: s.SecureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode,
		Expires: time.Now().Add(30 * 24 * time.Hour), MaxAge: 30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, "/orders/"+url.PathEscape(order.OrderNumber), http.StatusSeeOther)
}

func (s *Server) validateTemplatePackage(w http.ResponseWriter, r *http.Request, templateID, packageID string) (*storage.Template, storage.PackageOption, bool) {
	template, err := s.DB.GetTemplate(r.Context(), templateID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return nil, storage.PackageOption{}, false
	}
	if err != nil {
		s.fail(w, "load template for package selection", err)
		return nil, storage.PackageOption{}, false
	}
	for _, supported := range template.Packages {
		if supported.ID != packageID {
			continue
		}
		if !supported.Available || supported.ComingSoon {
			http.Error(w, "Paket ini belum tersedia", http.StatusBadRequest)
			return nil, storage.PackageOption{}, false
		}
		return template, supported, true
	}
	http.Error(w, "Paket tidak didukung oleh template ini", http.StatusBadRequest)
	return nil, storage.PackageOption{}, false
}

func containsPackage(packages []storage.PackageRow, packageID string) bool {
	for _, pkg := range packages {
		if pkg.ID == packageID {
			return true
		}
	}
	return false
}

func containsCategory(categories []string, category string) bool {
	for _, item := range categories {
		if item == category {
			return true
		}
	}
	return false
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("web: render %s: %v", r.URL.Path, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, operation string, err error) {
	log.Printf("web: %s: %v", operation, err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func packageFeatures(packageID string) []string {
	switch packageID {
	case "gold":
		return []string{"Semua fitur Silver", "Kustom warna, font, dan layout", "Amplop digital", "Galeri premium"}
	case "platinum":
		return []string{"Semua fitur Gold", "Paket eksklusif", "Kustomisasi penuh"}
	default:
		return []string{"Undangan responsive", "RSVP dan buku tamu", "Galeri foto", "Link personal"}
	}
}
