package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sukamoto/app/internal/auth"
	"github.com/sukamoto/app/internal/storage"
)

var csrfInput = regexp.MustCompile(`name="csrf_token" value="([a-f0-9]{64})"`)
var nextInput = regexp.MustCompile(`name="next" value="([^"]*)"`)

func testServer(t *testing.T) (*storage.DB, *httptest.Server) {
	t.Helper()
	db, err := storage.Open(context.Background(), t.TempDir()+"/test.db")
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

func TestCustomerRegistrationSessionRefreshAndLogout(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	csrf := getCSRF(t, client, server.URL+"/register")

	response := postForm(t, client, server.URL+"/register", url.Values{
		"csrf_token": {csrf},
		"full_name":  {"Senja Test"},
		"email":      {"senja@example.com"},
		"phone":      {"08123456789"},
		"password":   {"strong-password"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("registration redirect target status = %d, want dashboard 200", response.StatusCode)
	}
	if !strings.HasSuffix(response.Request.URL.Path, "/dashboard") {
		t.Fatalf("registration ended at %s, want /dashboard", response.Request.URL.Path)
	}
	response, err := client.Get(server.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("dashboard refresh status = %d, want 200", response.StatusCode)
	}

	logoutCSRF := getCSRF(t, client, server.URL+"/login")
	response = postForm(t, client, server.URL+"/logout", url.Values{"csrf_token": {logoutCSRF}})
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/") {
		t.Fatalf("logout redirect result = %d %s, want home 200", response.StatusCode, response.Request.URL.Path)
	}
	response, err = client.Get(server.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if !strings.HasSuffix(response.Request.URL.Path, "/login") {
		t.Fatalf("dashboard after logout ended at %s, want login redirect", response.Request.URL.Path)
	}
}

func TestLoginRejectsInvalidCSRF(t *testing.T) {
	_, server := testServer(t)
	client := newTestClient(t)
	response, err := client.PostForm(server.URL+"/login", url.Values{
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
	if err := login(t, customerClient, server.URL, "customer@example.com", "customer-password"); err != nil {
		t.Fatal(err)
	}
	response, err := customerClient.Get(server.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("customer access to admin returned %d, want 403", response.StatusCode)
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
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("admin access to customer dashboard returned %d, want 403", response.StatusCode)
	}

	if adminID == customerID {
		t.Fatal("admin and customer unexpectedly share an ID")
	}
}

func login(t *testing.T, client *http.Client, baseURL, email, password string) error {
	t.Helper()
	csrf := getCSRF(t, client, baseURL+"/login")
	response, err := client.PostForm(baseURL+"/login", url.Values{
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
	response, err := http.Get(server.URL + "/login")
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
	db, server := testServer(t)
	ctx := context.Background()
	passwordHash, err := auth.HashPassword("customer-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(ctx, "Customer", "template@example.com", "", passwordHash, auth.RoleCustomer); err != nil {
		t.Fatal(err)
	}

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

	csrf := getCSRF(t, client, server.URL+"/templates/G01")
	response = postForm(t, client, server.URL+"/templates/G01/use", url.Values{
		"csrf_token": {csrf}, "package_id": {"gold"},
	})
	unauthenticatedHTML, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/login") {
		t.Fatalf("anonymous template selection ended at %s with status %d", response.Request.URL, response.StatusCode)
	}
	next := response.Request.URL.Query().Get("next")
	if next != "/templates/G01/use?package=gold" {
		t.Fatalf("template selection return path = %q", next)
	}
	if !strings.Contains(string(unauthenticatedHTML), `name="next"`) {
		t.Fatal("login form did not preserve the requested template choice")
	}
	loginCSRF := getCSRF(t, client, response.Request.URL.String())
	response = postForm(t, client, server.URL+"/login", url.Values{
		"csrf_token": {loginCSRF},
		"email":      {"template@example.com"},
		"password":   {"customer-password"},
		"next":       {next},
	})
	confirmation, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/templates/G01/use") || !strings.Contains(string(confirmation), "Konfirmasi pilihan Anda") {
		t.Fatalf("login did not resume template selection: %s (%d)", response.Request.URL, response.StatusCode)
	}

	csrf = getCSRF(t, client, response.Request.URL.String())
	response = postForm(t, client, server.URL+"/templates/G01/use", url.Values{
		"csrf_token": {csrf}, "package_id": {"silver"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported Silver package selection returned %d, want 400", response.StatusCode)
	}

	response = postForm(t, client, server.URL+"/templates/G01/use", url.Values{
		"csrf_token": {csrf}, "package_id": {"gold"},
	})
	dashboard, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/dashboard") {
		t.Fatalf("valid package selection did not reach dashboard: %s (%d)", response.Request.URL, response.StatusCode)
	}
	if !strings.Contains(string(dashboard), "Pilihan tersimpan") || !strings.Contains(string(dashboard), "Golden Kirana") || !strings.Contains(string(dashboard), "Gold") {
		t.Fatal("dashboard did not show the saved template order")
	}
	var selectedPackage, selectedTemplate string
	if err := db.SQL.QueryRow(`SELECT package_id, template_id FROM orders WHERE user_id = (SELECT id FROM users WHERE email = ?)`, "template@example.com").Scan(&selectedPackage, &selectedTemplate); err != nil {
		t.Fatal(err)
	}
	if selectedPackage != "gold" || selectedTemplate != "G01" {
		t.Fatalf("saved order selection = %s/%s, want gold/G01", selectedPackage, selectedTemplate)
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
