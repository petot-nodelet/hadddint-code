package web

import (
	"net/http"
	"strings"

	"github.com/sukamoto/app/internal/auth"
)

// upper returns s upper-cased (used for package badges).
func upper(s string) string { return strings.ToUpper(s) }

// ifElse is a tiny ternary helper usable inside templ expressions.
func ifElse(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func statusLabel(status string) string {
	switch status {
	case "pending_payment":
		return "Menunggu pembayaran"
	case "payment_submitted":
		return "Pembayaran sedang diverifikasi"
	case "payment_rejected":
		return "Pembayaran ditolak"
	case "paid":
		return "Lunas"
	case "waiting_payment":
		return "Menunggu pembayaran"
	case "processing":
		return "Sedang diproses"
	case "published":
		return "Terbit"
	case "expired":
		return "Kedaluwarsa"
	case "cancelled":
		return "Dibatalkan"
	case "draft":
		return "Draf"
	default:
		return status
	}
}

// BuildPageData assembles the common page payload from the request context.
func BuildPageData(r *http.Request, m *auth.Manager, title, desc string) (PageData, error) {
	pd := PageData{Title: title, Description: desc}
	if m != nil {
		token, err := m.CSRFToken(r.Context())
		if err != nil {
			return PageData{}, err
		}
		pd.CSRF = token
		pd.Nav = NavProps{
			Authenticated: m.IsAuthenticated(r.Context()),
			IsAdmin:       m.IsAdmin(r.Context()),
			Name:          m.CurrentName(r.Context()),
			CSRF:          token,
		}
	}
	return pd, nil
}
