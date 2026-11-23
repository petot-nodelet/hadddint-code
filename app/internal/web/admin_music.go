package web

import (
	"crypto/rand"
	"encoding/hex"
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

type AdminMusicPageData struct {
	Page   PageData
	Tracks []storage.MusicTrack
	Error  string
}

func (s *Server) handleAdminMusic(w http.ResponseWriter, r *http.Request) {
	tracks, err := s.DB.ListMusicTracks(r.Context(), false)
	if err != nil {
		s.fail(w, "list admin music tracks", err)
		return
	}
	page, err := BuildPageData(r, s.Auth, "Kelola Musik | SUKAMOTO", "Kelola audio yang dapat dipilih pada undangan.")
	if err != nil {
		s.fail(w, "build admin music page", err)
		return
	}
	s.render(w, r, http.StatusOK, AdminMusicPage(AdminMusicPageData{Page: page, Tracks: tracks}))
}

func (s *Server) handleAdminMusicUpload(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxInvitationMediaSize+(1<<20) {
		http.Error(w, "File terlalu besar.", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxInvitationMediaSize+(1<<20))
	if err := r.ParseMultipartForm(maxInvitationMediaSize); err != nil {
		http.Error(w, "Form unggahan terlalu besar atau tidak valid.", http.StatusRequestEntityTooLarge)
		return
	}
	title := strings.TrimSpace(r.PostForm.Get("title"))
	if length := utf8.RuneCountInString(title); length < 1 || length > 120 {
		http.Error(w, "Judul harus berisi 1 sampai 120 karakter.", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "Pilih file audio.", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxInvitationMediaSize {
		http.Error(w, "File audio maksimal 5 MiB.", http.StatusRequestEntityTooLarge)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxInvitationMediaSize+1))
	if err != nil {
		s.fail(w, "read admin music upload", err)
		return
	}
	if len(data) == 0 || len(data) > maxInvitationMediaSize {
		http.Error(w, "File audio maksimal 5 MiB.", http.StatusRequestEntityTooLarge)
		return
	}
	mimeType, extension, err := validateInvitationMedia("audio", data)
	if err != nil {
		http.Error(w, "File harus berupa MP3 atau OGG yang valid.", http.StatusBadRequest)
		return
	}
	if s.DB.UploadDir == "" {
		http.Error(w, "Penyimpanan media belum dikonfigurasi.", http.StatusServiceUnavailable)
		return
	}
	directory := filepath.Join(s.DB.UploadDir, "music-library")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		s.fail(w, "create music library directory", err)
		return
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		s.fail(w, "restrict music library directory", err)
		return
	}
	storedName, err := randomInvitationStoredName(extension)
	if err != nil {
		s.fail(w, "generate music track name", err)
		return
	}
	path := filepath.Join(directory, storedName)
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		s.fail(w, "create music track", err)
		return
	}
	if _, err := output.Write(data); err != nil {
		_ = output.Close()
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			s.fail(w, "clean failed music track write", fmt.Errorf("%v; cleanup: %w", err, removeErr))
			return
		}
		s.fail(w, "write music track", err)
		return
	}
	if err := output.Close(); err != nil {
		s.fail(w, "close music track", err)
		return
	}
	if _, err := s.DB.InsertMusicTrack(r.Context(), title, storedName, mimeType, int64(len(data))); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			s.fail(w, "clean rejected music track", fmt.Errorf("%v; cleanup: %w", err, removeErr))
			return
		}
		s.fail(w, "save music track", err)
		return
	}
	http.Redirect(w, r, "/admin/music", http.StatusSeeOther)
}

func randomInvitationStoredName(extension string) (string, error) {
	var randomName [18]byte
	if _, err := rand.Read(randomName[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomName[:]) + extension, nil
}

func (s *Server) handleAdminMusicActive(w http.ResponseWriter, r *http.Request) {
	trackID, err := strconv.ParseInt(chi.URLParam(r, "trackID"), 10, 64)
	if err != nil || trackID < 1 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid.", http.StatusBadRequest)
		return
	}
	active := r.PostForm.Get("active")
	if active != "0" && active != "1" {
		http.Error(w, "Status audio tidak valid.", http.StatusBadRequest)
		return
	}
	if err := s.DB.SetMusicTrackActive(r.Context(), trackID, active == "1"); errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.fail(w, "update music track state", err)
		return
	}
	http.Redirect(w, r, "/admin/music", http.StatusSeeOther)
}

func (s *Server) handleAdminMusicFile(w http.ResponseWriter, r *http.Request) {
	trackID, err := strconv.ParseInt(chi.URLParam(r, "trackID"), 10, 64)
	if err != nil || trackID < 1 {
		http.NotFound(w, r)
		return
	}
	track, err := s.DB.GetMusicTrack(r.Context(), trackID)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "load admin music file", err)
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
		s.fail(w, "open admin music file", err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", track.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(track.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.CopyN(w, file, track.SizeBytes); err != nil && !errors.Is(err, io.EOF) {
		s.fail(w, "serve admin music file", err)
	}
}

func (s *Server) handleInvitationMusicTrack(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
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
		s.fail(w, "decode selected invitation music", err)
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
		s.fail(w, "load invitation music track", err)
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
		s.fail(w, "open invitation music track", err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", track.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(track.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.CopyN(w, file, track.SizeBytes); err != nil && !errors.Is(err, io.EOF) {
		s.fail(w, "serve invitation music track", err)
	}
}

func adminMusicURL(trackID int64) string {
	return "/admin/music/" + url.PathEscape(strconv.FormatInt(trackID, 10)) + "/file"
}

func musicTrackStatus(active int64) string {
	if active == 1 {
		return "Aktif"
	}
	return "Nonaktif"
}
