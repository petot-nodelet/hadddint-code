package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
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
	DB        *storage.DB
	Auth      *auth.Manager
	StaticDir string
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
	return &Server{DB: db, Auth: manager, StaticDir: absoluteStaticDir}, nil
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(s.Auth.Session.LoadAndSave)
	r.Use(s.Auth.CSRFMiddleware)

	r.Get("/", s.handleLanding)
	r.Get("/login", s.handleLoginPage)
	r.Post("/login", s.handleLogin)
	r.Get("/register", s.handleRegisterPage)
	r.Post("/register", s.handleRegister)
	r.Post("/logout", s.handleLogout)
	r.With(s.requireRole(auth.RoleCustomer)).Get("/dashboard", s.handleCustomerDashboard)
	r.With(s.requireRole(auth.RoleAdmin)).Get("/admin", s.handleAdminDashboard)
	r.Get("/templates", s.handleTemplateCatalog)
	r.Get("/templates/{id}", s.handleTemplateDetail)
	r.Get("/templates/{id}/use", s.handleTemplateSelectionPage)
	r.Post("/templates/{id}/use", s.handleTemplateUse)

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(s.StaticDir, "app", "static")))))
	r.Handle("/index/*", http.StripPrefix("/index/", http.FileServer(http.Dir(filepath.Join(s.StaticDir, "index")))))
	return r
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

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.renderAuthPage(w, r, "Masuk | SUKAMOTO", "", safeNext(r.URL.Query().Get("next")), http.StatusOK, LoginPage)
}

func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	s.renderAuthPage(w, r, "Daftar | SUKAMOTO", "", safeNext(r.URL.Query().Get("next")), http.StatusOK, RegisterPage)
}

func (s *Server) renderAuthPage(w http.ResponseWriter, r *http.Request, title, message, next string, status int, page func(AuthPageData) templ.Component) {
	pageData, err := BuildPageData(r, s.Auth, title, "Akun SUKAMOTO")
	if err != nil {
		s.fail(w, "build authentication page", err)
		return
	}
	s.render(w, r, status, page(AuthPageData{Page: pageData, Error: message, Next: next}))
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	fullName := strings.TrimSpace(r.PostForm.Get("full_name"))
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	phone := strings.TrimSpace(r.PostForm.Get("phone"))
	password := r.PostForm.Get("password")
	next := safeNext(r.PostForm.Get("next"))
	message := validateRegistration(fullName, email, phone, password)
	if message != "" {
		s.renderAuthPage(w, r, "Daftar | SUKAMOTO", message, next, http.StatusBadRequest, RegisterPage)
		return
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, "hash registration password", err)
		return
	}
	userID, err := s.DB.CreateUser(r.Context(), fullName, email, phone, passwordHash, auth.RoleCustomer)
	if errors.Is(err, storage.ErrEmailTaken) {
		s.renderAuthPage(w, r, "Daftar | SUKAMOTO", "Email sudah terdaftar.", next, http.StatusConflict, RegisterPage)
		return
	}
	if err != nil {
		s.fail(w, "create customer account", err)
		return
	}
	if err := s.Auth.Login(r.Context(), userID, auth.RoleCustomer, fullName); err != nil {
		s.fail(w, "start customer session", err)
		return
	}
	s.redirectAfterLogin(w, r, next, auth.RoleCustomer)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	password := r.PostForm.Get("password")
	next := safeNext(r.PostForm.Get("next"))
	if email == "" || password == "" {
		s.renderAuthPage(w, r, "Masuk | SUKAMOTO", "Email atau password tidak valid.", next, http.StatusUnauthorized, LoginPage)
		return
	}
	user, err := s.DB.GetUserByEmail(r.Context(), email)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && (user.Status != "active" || !auth.CheckPassword(user.PasswordHash, password))) {
		s.renderAuthPage(w, r, "Masuk | SUKAMOTO", "Email atau password tidak valid.", next, http.StatusUnauthorized, LoginPage)
		return
	}
	if err != nil {
		s.fail(w, "load login account", err)
		return
	}
	if err := s.Auth.Login(r.Context(), user.ID, user.Role, user.FullName); err != nil {
		s.fail(w, "start login session", err)
		return
	}
	s.redirectAfterLogin(w, r, next, user.Role)
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
			if !s.Auth.IsAuthenticated(r.Context()) {
				target := url.QueryEscape(r.URL.RequestURI())
				http.Redirect(w, r, "/login?next="+target, http.StatusSeeOther)
				return
			}
			if s.Auth.CurrentRole(r.Context()) != role {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) handleCustomerDashboard(w http.ResponseWriter, r *http.Request) {
	page, err := BuildPageData(r, s.Auth, "Dashboard | SUKAMOTO", "Dashboard pelanggan SUKAMOTO.")
	if err != nil {
		s.fail(w, "build customer dashboard", err)
		return
	}
	var order *OrderSummary
	if rawOrderID := r.URL.Query().Get("order_id"); rawOrderID != "" {
		orderID, err := strconv.ParseInt(rawOrderID, 10, 64)
		if err != nil || orderID < 1 {
			http.Error(w, "Nomor pesanan tidak valid", http.StatusBadRequest)
			return
		}
		userID, ok := s.Auth.CurrentUserID(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		selection, err := s.DB.GetOrderByIDForUser(r.Context(), orderID, userID)
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.fail(w, "load customer order summary", err)
			return
		}
		order = &OrderSummary{
			ID: selection.ID, PackageName: selection.PackageName,
			TemplateName: selection.TemplateName, Status: selection.Status,
		}
	}
	s.render(w, r, http.StatusOK, CustomerDashboard(page, s.Auth.CurrentName(r.Context()), order))
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
	if !s.Auth.IsAuthenticated(r.Context()) {
		s.redirectToTemplateLogin(w, r, template.ID, packageOption.ID)
		return
	}
	if s.Auth.CurrentRole(r.Context()) != auth.RoleCustomer {
		http.Error(w, "Forbidden", http.StatusForbidden)
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
	if !s.Auth.IsAuthenticated(r.Context()) {
		s.redirectToTemplateLogin(w, r, template.ID, packageOption.ID)
		return
	}
	if s.Auth.CurrentRole(r.Context()) != auth.RoleCustomer {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	userID, ok := s.Auth.CurrentUserID(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	orderID, err := s.DB.CreateOrder(r.Context(), userID, packageOption.ID, template.ID)
	if err != nil {
		s.fail(w, "save customer template selection", err)
		return
	}
	http.Redirect(w, r, "/dashboard?order_id="+strconv.FormatInt(orderID, 10), http.StatusSeeOther)
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

func (s *Server) redirectToTemplateLogin(w http.ResponseWriter, r *http.Request, templateID, packageID string) {
	next := "/templates/" + url.PathEscape(templateID) + "/use?package=" + url.QueryEscape(packageID)
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
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

func (s *Server) redirectAfterLogin(w http.ResponseWriter, r *http.Request, next, role string) {
	if next == "" {
		if role == auth.RoleAdmin {
			next = "/admin"
		} else {
			next = "/dashboard"
		}
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func safeNext(candidate string) string {
	if candidate == "" || !strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "//") || strings.ContainsAny(candidate, "\r\n") {
		return ""
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return ""
	}
	return parsed.RequestURI()
}

func validateRegistration(fullName, email, phone, password string) string {
	if len([]rune(fullName)) < 2 || len([]rune(fullName)) > 100 {
		return "Nama harus berisi 2 sampai 100 karakter."
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return "Masukkan alamat email yang valid."
	}
	if len(phone) > 30 {
		return "Nomor telepon terlalu panjang."
	}
	if len(password) < 8 || len(password) > 72 {
		return "Password harus berisi 8 sampai 72 karakter."
	}
	return ""
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
