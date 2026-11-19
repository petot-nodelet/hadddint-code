package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/sukamoto/app/db/generated"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleCustomer = "customer"
	RoleAdmin    = "admin"

	userIDKey    = "user_id"
	userRoleKey  = "role"
	userNameKey  = "user_name"
	csrfTokenKey = "csrf_token"
)

type Manager struct {
	Session *scs.SessionManager
}

func NewManager(queries *generated.Queries, cookieName string, ttl time.Duration, secure bool) (*Manager, error) {
	if queries == nil {
		return nil, fmt.Errorf("auth: session queries are required")
	}
	if cookieName == "" {
		return nil, fmt.Errorf("auth: session cookie name is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("auth: session lifetime must be positive")
	}
	sm := scs.New()
	sm.Store = &sqliteStore{queries: queries}
	sm.Cookie.Name = cookieName
	sm.Cookie.Path = "/"
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = secure
	sm.Lifetime = ttl
	return &Manager{Session: sm}, nil
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(b), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (m *Manager) Login(ctx context.Context, userID int64, role, fullName string) error {
	if role != RoleCustomer && role != RoleAdmin {
		return fmt.Errorf("auth: unsupported role %q", role)
	}
	if err := m.Session.RenewToken(ctx); err != nil {
		return fmt.Errorf("auth: renew token: %w", err)
	}
	token, err := newCSRFToken()
	if err != nil {
		return err
	}
	m.Session.Put(ctx, userIDKey, userID)
	m.Session.Put(ctx, userRoleKey, role)
	m.Session.Put(ctx, userNameKey, fullName)
	m.Session.Put(ctx, csrfTokenKey, token)
	return nil
}

func (m *Manager) Logout(ctx context.Context) error {
	if err := m.Session.Destroy(ctx); err != nil {
		return fmt.Errorf("auth: destroy session: %w", err)
	}
	return nil
}

func (m *Manager) CurrentUserID(ctx context.Context) (int64, bool) {
	id := m.Session.GetInt64(ctx, userIDKey)
	return id, id > 0
}

func (m *Manager) CurrentRole(ctx context.Context) string {
	return m.Session.GetString(ctx, userRoleKey)
}

func (m *Manager) CurrentName(ctx context.Context) string {
	return m.Session.GetString(ctx, userNameKey)
}

func (m *Manager) IsAuthenticated(ctx context.Context) bool {
	_, ok := m.CurrentUserID(ctx)
	return ok
}

func (m *Manager) IsAdmin(ctx context.Context) bool {
	return m.CurrentRole(ctx) == RoleAdmin
}

func (m *Manager) CSRFToken(ctx context.Context) (string, error) {
	token := m.Session.GetString(ctx, csrfTokenKey)
	if token != "" {
		return token, nil
	}
	token, err := newCSRFToken()
	if err != nil {
		return "", err
	}
	m.Session.Put(ctx, csrfTokenKey, token)
	return token, nil
}

func (m *Manager) ValidateCSRF(ctx context.Context, submitted string) bool {
	expected := m.Session.GetString(ctx, csrfTokenKey)
	if expected == "" || submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(submitted)) == 1
}

func (m *Manager) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form submission", http.StatusBadRequest)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.PostForm.Get("csrf_token")
		}
		if !m.ValidateCSRF(r.Context(), token) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newCSRFToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("auth: generate csrf token: %w", err)
	}
	return hex.EncodeToString(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

var ErrInvalidCredentials = errors.New("invalid email or password")
