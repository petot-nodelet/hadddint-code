package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/sukamoto/app/internal/storage"
)

type AdminReviewValue struct {
	Label string
	Value string
}

type AdminReviewModule struct {
	Title     string
	Complete  bool
	Available bool
	Fields    []AdminReviewValue
	Items     [][]AdminReviewValue
}

type AdminInvitationPageData struct {
	Page    PageData
	Order   storage.InvitationOrder
	Modules []AdminReviewModule
	Media   []storage.InvitationMedia
	Error   string
}

func (s *Server) getAdminInvitation(w http.ResponseWriter, r *http.Request) (storage.InvitationOrder, storage.InvitationFeatures, bool) {
	orderNumber := chi.URLParam(r, "orderNumber")
	if !storage.ValidOrderNumber(orderNumber) {
		http.NotFound(w, r)
		return storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	order, err := s.DB.GetAdminInvitationOrder(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	if err != nil {
		s.fail(w, "load admin invitation draft", err)
		return storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	features, err := s.DB.GetInvitationFeatures(r.Context(), order.PackageID)
	if err != nil {
		s.fail(w, "load admin invitation entitlements", err)
		return storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	return order, features, true
}

func (s *Server) handleAdminInvitationReview(w http.ResponseWriter, r *http.Request) {
	order, features, ok := s.getAdminInvitation(w, r)
	if !ok {
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode admin invitation draft", err)
		return
	}
	media, err := s.DB.ListInvitationMedia(r.Context(), order.ID)
	if err != nil {
		s.fail(w, "list admin invitation media", err)
		return
	}
	modules := make([]AdminReviewModule, 0, len(invitationModuleSpecs()))
	for _, spec := range invitationModuleSpecs() {
		module := draft.Modules[spec.id]
		reviewModule := AdminReviewModule{
			Title: spec.title, Complete: moduleIsComplete(spec, module),
			Available: builderModuleAvailable(spec, features, order),
		}
		for _, field := range spec.fields {
			if value := strings.TrimSpace(module.Fields[field.key]); value != "" {
				reviewModule.Fields = append(reviewModule.Fields, AdminReviewValue{Label: field.label, Value: value})
			}
		}
		for _, item := range module.Items {
			values := make([]AdminReviewValue, 0, len(spec.itemFields))
			for _, field := range spec.itemFields {
				if value := strings.TrimSpace(item[field.key]); value != "" {
					values = append(values, AdminReviewValue{Label: field.label, Value: value})
				}
			}
			if len(values) > 0 {
				reviewModule.Items = append(reviewModule.Items, values)
			}
		}
		modules = append(modules, reviewModule)
	}
	page, err := BuildPageData(r, s.Auth, "Review undangan | SUKAMOTO", "Review draft undangan pesanan.")
	if err != nil {
		s.fail(w, "build admin invitation review page", err)
		return
	}
	s.render(w, r, http.StatusOK, AdminInvitationReviewPage(AdminInvitationPageData{
		Page: page, Order: order, Modules: modules, Media: media,
	}))
}

func (s *Server) handleAdminInvitationReviewUpdate(w http.ResponseWriter, r *http.Request) {
	orderNumber := chi.URLParam(r, "orderNumber")
	if !storage.ValidOrderNumber(orderNumber) {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form review tidak valid.", http.StatusBadRequest)
		return
	}
	status := r.PostForm.Get("review_status")
	if status != "in_progress" && status != "revision_requested" && status != "ready" {
		http.Error(w, "Status review tidak valid.", http.StatusBadRequest)
		return
	}
	note := strings.TrimSpace(r.PostForm.Get("review_note"))
	if utf8.RuneCountInString(note) > 1000 {
		http.Error(w, "Catatan review maksimal 1000 karakter.", http.StatusBadRequest)
		return
	}
	if err := s.DB.UpdateInvitationReview(r.Context(), orderNumber, status, note); errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.fail(w, "save invitation review", err)
		return
	}
	http.Redirect(w, r, "/admin/orders/"+url.PathEscape(orderNumber)+"/invitation", http.StatusSeeOther)
}

func (s *Server) handleAdminInvitationMedia(w http.ResponseWriter, r *http.Request) {
	order, _, ok := s.getAdminInvitation(w, r)
	if !ok {
		return
	}
	mediaID, err := strconv.ParseInt(chi.URLParam(r, "mediaID"), 10, 64)
	if err != nil || mediaID < 1 {
		http.NotFound(w, r)
		return
	}
	media, err := s.DB.GetInvitationMedia(r.Context(), order.ID, mediaID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load admin invitation media", err)
		return
	}
	if !safeInvitationStoredName(media.StoredName) || s.DB.UploadDir == "" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(s.DB.UploadDir, "invitation-media", media.StoredName))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "open admin invitation media", err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", media.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(media.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.CopyN(w, file, media.SizeBytes); err != nil && !errors.Is(err, io.EOF) {
		s.fail(w, "serve admin invitation media", err)
	}
}

func (s *Server) handleAdminInvitationMusic(w http.ResponseWriter, r *http.Request) {
	order, features, ok := s.getAdminInvitation(w, r)
	if !ok {
		return
	}
	if !features.BackgroundMusic {
		http.NotFound(w, r)
		return
	}
	trackID, err := strconv.ParseInt(chi.URLParam(r, "trackID"), 10, 64)
	if err != nil || trackID < 1 {
		http.NotFound(w, r)
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode selected admin music", err)
		return
	}
	selectedID, err := strconv.ParseInt(draft.Modules["music"].Fields["track_id"], 10, 64)
	if err != nil || selectedID != trackID {
		http.NotFound(w, r)
		return
	}
	track, err := s.DB.GetMusicTrack(r.Context(), trackID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load admin invitation music", err)
		return
	}
	if !safeInvitationStoredName(track.StoredName) || s.DB.UploadDir == "" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(s.DB.UploadDir, "music-library", track.StoredName))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "open admin invitation music", err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", track.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(track.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.CopyN(w, file, track.SizeBytes); err != nil && !errors.Is(err, io.EOF) {
		s.fail(w, "serve admin invitation music", err)
	}
}

func (s *Server) handleAdminInvitationPreview(w http.ResponseWriter, r *http.Request) {
	order, features, ok := s.getAdminInvitation(w, r)
	if !ok {
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode admin invitation preview", err)
		return
	}
	template, err := s.DB.GetTemplate(r.Context(), order.TemplateID)
	if err != nil {
		s.fail(w, "load admin invitation preview template", err)
		return
	}
	config, err := decodePreviewConfig(*template)
	if err != nil {
		s.fail(w, "decode admin invitation preview template", err)
		return
	}
	sample := invitationPreviewSample(order, draft, features, func(mediaID int64) string {
		return fmt.Sprintf("/admin/orders/%s/invitation/media/%d", url.PathEscape(order.OrderNumber), mediaID)
	}, func(trackID int64) string {
		return fmt.Sprintf("/admin/orders/%s/invitation/music/%d", url.PathEscape(order.OrderNumber), trackID)
	})
	if sample.Thumbnail == "" {
		sample.Thumbnail = config.Media.Cover
	}
	if err := applyInvitationPreviewSettings(&config, draft, features); err != nil {
		s.fail(w, "apply admin invitation preview settings", err)
		return
	}
	page, err := BuildPageData(r, s.Auth, "Pratinjau undangan admin | SUKAMOTO", "Pratinjau draft pesanan dari area admin.")
	if err != nil {
		s.fail(w, "build admin invitation preview", err)
		return
	}
	s.render(w, r, http.StatusOK, InvitationPreviewPage(BuilderPageData{
		Page: page, Order: order, Draft: draft, Module: BuilderModule{Title: "Pratinjau"},
	}, config, sample))
}
