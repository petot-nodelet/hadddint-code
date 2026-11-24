package web

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/sukamoto/app/internal/storage"
)

const (
	invitationDraftSchemaVersion = 1
	maxInvitationMediaSize       = 5 << 20
	maxInvitationDraftSize       = 64 << 10
)

var invitationSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,30}[a-z0-9])$`)

type InvitationDraft struct {
	SchemaVersion int                         `json:"schema_version"`
	Modules       map[string]InvitationModule `json:"modules"`
}

type InvitationModule struct {
	Fields map[string]string   `json:"fields"`
	Items  []map[string]string `json:"items,omitempty"`
}

type BuilderField struct {
	Key          string
	Label        string
	Type         string
	MaxLength    int
	Options      []BuilderOption
	MediaOptions []BuilderMediaOption
	Value        string
}

type BuilderOption struct {
	Value string
	Label string
}

type BuilderMediaOption struct {
	ID      int64
	Label   string
	MediaID string
}

type BuilderItem struct {
	Fields []BuilderField
}

type BuilderModule struct {
	ID        string
	Title     string
	Complete  bool
	Available bool
}

type BuilderPageData struct {
	Page       PageData
	Order      storage.InvitationOrder
	Draft      InvitationDraft
	Module     BuilderModule
	Modules    []BuilderModule
	Fields     []BuilderField
	Items      []BuilderItem
	ItemLimit  int
	Media      []storage.InvitationMedia
	PhotoLimit int64
	PhotoCount int64
	Error      string
	Saved      bool
	Preview    bool
}

type builderFieldSpec struct {
	key       string
	label     string
	kind      string
	maxLength int
	options   []BuilderOption
}

type builderModuleSpec struct {
	id          string
	title       string
	description string
	fields      []builderFieldSpec
	itemFields  []builderFieldSpec
	entitlement string
	completeOn  []string
}

func invitationModuleSpecs() []builderModuleSpec {
	return []builderModuleSpec{
		{id: "basic", title: "Informasi dasar", description: "Nama pasangan dan informasi keluarga.", fields: []builderFieldSpec{
			{key: "bride_name", label: "Nama mempelai wanita", kind: "text", maxLength: 100},
			{key: "bride_parent", label: "Nama orang tua mempelai wanita", kind: "text", maxLength: 160},
			{key: "groom_name", label: "Nama mempelai pria", kind: "text", maxLength: 100},
			{key: "groom_parent", label: "Nama orang tua mempelai pria", kind: "text", maxLength: 160},
		}, completeOn: []string{"bride_name", "groom_name"}},
		{id: "cover", title: "Sampul", description: "Judul, teks pembuka, dan foto sampul.", fields: []builderFieldSpec{
			{key: "title", label: "Judul sampul", kind: "text", maxLength: 100},
			{key: "subtitle", label: "Teks pembuka", kind: "textarea", maxLength: 500},
			{key: "cover_media_id", label: "Foto sampul", kind: "media"},
		}},
		{id: "quotes", title: "Kutipan & doa", description: "Kutipan, sumber, dan doa yang ingin ditampilkan.", fields: []builderFieldSpec{
			{key: "quote_text", label: "Kutipan", kind: "textarea", maxLength: 1200},
			{key: "quote_source", label: "Sumber kutipan", kind: "text", maxLength: 160},
			{key: "prayer", label: "Doa", kind: "textarea", maxLength: 1200},
			{key: "quote_extra", label: "Catatan tambahan", kind: "textarea", maxLength: 500},
		}},
		{id: "story", title: "Kisah pasangan", description: "Tulis pengantar dan momen penting perjalanan kalian.", entitlement: "couple_story", fields: []builderFieldSpec{
			{key: "intro", label: "Pengantar kisah", kind: "textarea", maxLength: 1200},
		}, itemFields: []builderFieldSpec{
			{key: "title", label: "Judul momen", kind: "text", maxLength: 100},
			{key: "date", label: "Tanggal (opsional)", kind: "date", maxLength: 10},
			{key: "description", label: "Cerita", kind: "textarea", maxLength: 1000},
			{key: "media_id", label: "Foto (opsional)", kind: "media"},
		}},
		{id: "events", title: "Acara & lokasi", description: "Atur waktu dan lokasi acara pernikahan.", itemFields: []builderFieldSpec{
			{key: "name", label: "Nama acara", kind: "text", maxLength: 100},
			{key: "date", label: "Tanggal", kind: "date", maxLength: 10},
			{key: "start_time", label: "Waktu mulai", kind: "time", maxLength: 5},
			{key: "end_time", label: "Waktu selesai", kind: "time", maxLength: 5},
			{key: "timezone", label: "Zona waktu", kind: "select", options: options("Asia/Jakarta", "WIB", "Asia/Makassar", "WITA", "Asia/Jayapura", "WIT")},
			{key: "location", label: "Nama tempat", kind: "text", maxLength: 160},
			{key: "address", label: "Alamat", kind: "textarea", maxLength: 500},
			{key: "maps_url", label: "Tautan Google Maps (opsional)", kind: "url", maxLength: 500},
			{key: "description", label: "Catatan acara", kind: "textarea", maxLength: 500},
		}, completeOn: []string{"name", "date", "location"}},
		{id: "gallery", title: "Galeri foto", description: "Pilih foto yang telah diunggah untuk galeri.", itemFields: []builderFieldSpec{
			{key: "caption", label: "Keterangan foto", kind: "text", maxLength: 160},
			{key: "media_id", label: "Foto", kind: "media"},
		}},
		{id: "video", title: "Video", description: "Tambahkan tautan video dari YouTube atau Vimeo.", itemFields: []builderFieldSpec{
			{key: "title", label: "Judul video", kind: "text", maxLength: 120},
			{key: "url", label: "Tautan YouTube atau Vimeo", kind: "url", maxLength: 500},
		}},
		{id: "music", title: "Musik", description: "Pilih audio yang diunggah atau matikan musik latar.", entitlement: "background_music", fields: []builderFieldSpec{
			{key: "enabled", label: "Musik latar", kind: "select", options: options("false", "Tidak digunakan", "true", "Aktif")},
			{key: "track_id", label: "Musik dari daftar admin", kind: "track"},
			{key: "media_id", label: "Audio kustom (paket yang mendukung)", kind: "audio"},
		}},
		{id: "rsvp", title: "Konfirmasi kehadiran", description: "Pengaturan tampilan RSVP untuk undangan.", entitlement: "rsvp_enabled", fields: []builderFieldSpec{
			{key: "enabled", label: "Tampilkan RSVP", kind: "select", options: options("false", "Tidak", "true", "Ya")},
			{key: "title", label: "Judul", kind: "text", maxLength: 100},
			{key: "description", label: "Petunjuk", kind: "textarea", maxLength: 500},
			{key: "deadline", label: "Batas konfirmasi", kind: "date", maxLength: 10},
			{key: "max_guests", label: "Maksimal tamu per konfirmasi", kind: "number", maxLength: 2},
		}},
		{id: "guestbook", title: "Buku tamu", description: "Atur bagian buku tamu pada undangan.", entitlement: "guestbook_enabled", fields: []builderFieldSpec{
			{key: "enabled", label: "Tampilkan buku tamu", kind: "select", options: options("false", "Tidak", "true", "Ya")},
			{key: "title", label: "Judul", kind: "text", maxLength: 100},
			{key: "description", label: "Petunjuk", kind: "textarea", maxLength: 500},
			{key: "moderation", label: "Moderasi pesan", kind: "select", options: options("true", "Aktif", "false", "Nonaktif")},
		}},
		{id: "envelope", title: "Amplop digital", description: "Informasi rekening yang akan ditampilkan.", entitlement: "digital_envelope", fields: []builderFieldSpec{
			{key: "enabled", label: "Tampilkan amplop digital", kind: "select", options: options("false", "Tidak", "true", "Ya")},
			{key: "bank_name", label: "Nama bank", kind: "text", maxLength: 100},
			{key: "account_holder", label: "Nama pemilik rekening", kind: "text", maxLength: 100},
			{key: "account_number", label: "Nomor rekening", kind: "text", maxLength: 50},
			{key: "note", label: "Catatan", kind: "textarea", maxLength: 500},
		}},
		{id: "greetings", title: "Ucapan tamu", description: "Atur sapaan dan nama tamu pada undangan.", entitlement: "guest_greeting", fields: []builderFieldSpec{
			{key: "enabled", label: "Aktifkan sapaan tamu", kind: "select", options: options("false", "Tidak", "true", "Ya")},
			{key: "opening", label: "Kalimat pembuka", kind: "text", maxLength: 200},
			{key: "guest_name", label: "Format nama tamu", kind: "text", maxLength: 100},
		}},
		{id: "settings", title: "Tampilan & tautan", description: "Sesuaikan gaya visual dan tautan undangan sesuai paket.", fields: []builderFieldSpec{
			{key: "slug", label: "Slug / tautan undangan", kind: "slug", maxLength: 32},
			{key: "background", label: "Warna latar", kind: "color", maxLength: 7},
			{key: "surface", label: "Warna bidang", kind: "color", maxLength: 7},
			{key: "accent", label: "Warna aksen", kind: "color", maxLength: 7},
			{key: "text", label: "Warna teks", kind: "color", maxLength: 7},
			{key: "heading_font", label: "Font judul", kind: "select", options: options("Cormorant Garamond", "Cormorant Garamond", "Alex Brush", "Alex Brush", "Plus Jakarta Sans", "Plus Jakarta Sans")},
			{key: "body_font", label: "Font isi", kind: "select", options: options("Plus Jakarta Sans", "Plus Jakarta Sans", "Cormorant Garamond", "Cormorant Garamond")},
			{key: "layout", label: "Tata letak", kind: "select", options: options("classic", "Klasik", "editorial", "Editorial", "story-first", "Kisah lebih dulu")},
			{key: "cover_style", label: "Gaya sampul", kind: "select", options: options("framed", "Bingkai", "torii", "Torii", "split", "Terbagi", "fullbleed", "Penuh", "parallax", "Parallax")},
			{key: "section_cover", label: "Tampilkan sampul", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
			{key: "section_couple", label: "Tampilkan mempelai", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
			{key: "section_story", label: "Tampilkan kisah pasangan", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
			{key: "section_events", label: "Tampilkan acara", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
			{key: "section_gallery", label: "Tampilkan galeri", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
			{key: "section_rsvp", label: "Tampilkan RSVP", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
			{key: "section_gift", label: "Tampilkan amplop digital", kind: "select", options: options("true", "Tampilkan", "false", "Sembunyikan")},
		}},
	}
}

func options(values ...string) []BuilderOption {
	result := make([]BuilderOption, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		result = append(result, BuilderOption{Value: values[index], Label: values[index+1]})
	}
	return result
}

func builderDescription(moduleID string) string {
	spec, ok := findInvitationModule(moduleID)
	if !ok {
		return ""
	}
	return spec.description
}

func builderHTMLInputType(kind string) string {
	switch kind {
	case "date", "time", "number", "color", "email", "url":
		return kind
	default:
		return "text"
	}
}

func invitationReviewStatuses() []BuilderOption {
	return []BuilderOption{
		{Value: "in_progress", Label: "Dalam pengerjaan"},
		{Value: "revision_requested", Label: "Perlu revisi"},
		{Value: "ready", Label: "Siap ditinjau"},
	}
}

func invitationReviewStatusLabel(status string) string {
	for _, option := range invitationReviewStatuses() {
		if option.Value == status {
			return option.Label
		}
	}
	return "Status tidak diketahui"
}

func decodeInvitationDraft(raw string) (InvitationDraft, error) {
	draft := InvitationDraft{SchemaVersion: invitationDraftSchemaVersion, Modules: map[string]InvitationModule{}}
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return draft, nil
	}
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		return InvitationDraft{}, fmt.Errorf("decode invitation draft: %w", err)
	}
	if draft.SchemaVersion == 0 {
		draft.SchemaVersion = invitationDraftSchemaVersion
	}
	if draft.SchemaVersion != invitationDraftSchemaVersion {
		return InvitationDraft{}, fmt.Errorf("unsupported invitation draft schema version %d", draft.SchemaVersion)
	}
	if draft.Modules == nil {
		draft.Modules = map[string]InvitationModule{}
	}
	return draft, nil
}

func (s *Server) getBuilderOrder(w http.ResponseWriter, r *http.Request) (storage.GuestOrder, storage.InvitationOrder, storage.InvitationFeatures, bool) {
	orderNumber := chi.URLParam(r, "orderNumber")
	if !storage.ValidOrderNumber(orderNumber) {
		http.NotFound(w, r)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	order, err := s.DB.GetGuestOrderByNumber(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	if err != nil {
		s.fail(w, "load invitation order access", err)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	if !s.validOrderAccess(w, r, order) {
		http.NotFound(w, r)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	invitationOrder, err := s.DB.GetInvitationOrder(r.Context(), orderNumber)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	if err != nil {
		s.fail(w, "load invitation draft", err)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	features, err := s.DB.GetInvitationFeatures(r.Context(), invitationOrder.PackageID)
	if err != nil {
		s.fail(w, "load invitation entitlements", err)
		return storage.GuestOrder{}, storage.InvitationOrder{}, storage.InvitationFeatures{}, false
	}
	return *order, invitationOrder, features, true
}

func (s *Server) handleInvitationBuilder(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
	if !ok {
		return
	}
	moduleID := chi.URLParam(r, "module")
	if moduleID == "" {
		moduleID = "basic"
	}
	s.renderInvitationBuilder(w, r, order, features, moduleID, "", false, http.StatusOK)
}

func (s *Server) handleSaveInvitationModule(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
	if !ok {
		return
	}
	moduleID := chi.URLParam(r, "module")
	spec, found := findInvitationModule(moduleID)
	if !found {
		http.NotFound(w, r)
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode invitation draft", err)
		return
	}
	if !builderModuleAvailable(spec, features, order) {
		s.renderInvitationBuilder(w, r, order, features, moduleID, "Fitur ini tidak termasuk dalam paket pesanan Anda.", false, http.StatusForbidden)
		return
	}
	if r.ContentLength > maxInvitationDraftSize {
		http.Error(w, "Data modul terlalu besar.", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxInvitationDraftSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Data modul tidak valid.", http.StatusBadRequest)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("draft_version"), 10, 64)
	if err != nil || version != order.DraftVersion {
		s.renderInvitationBuilder(w, r, order, features, moduleID, "Draft berubah di tab atau perangkat lain. Muat ulang sebelum menyimpan.", false, http.StatusConflict)
		return
	}
	allowedFields := allowedBuilderFields(spec, features)
	if moduleID == "music" {
		tracks, err := s.DB.ListMusicTracks(r.Context(), true)
		if err != nil {
			s.fail(w, "load invitation music options", err)
			return
		}
		for index := range allowedFields {
			if allowedFields[index].key == "track_id" {
				for _, track := range tracks {
					allowedFields[index].options = append(allowedFields[index].options, BuilderOption{Value: strconv.FormatInt(track.ID, 10), Label: track.Title})
				}
			}
		}
	}
	module := draft.Modules[moduleID]
	if module.Fields == nil {
		module.Fields = map[string]string{}
	}
	for _, field := range allowedFields {
		value := strings.TrimSpace(r.PostForm.Get("field_" + field.key))
		if utf8.RuneCountInString(value) > field.maxLength && field.maxLength > 0 {
			s.renderInvitationBuilder(w, r, order, features, moduleID, field.label+" terlalu panjang.", false, http.StatusBadRequest)
			return
		}
		if err := validateBuilderField(field, value); err != nil {
			s.renderInvitationBuilder(w, r, order, features, moduleID, err.Error(), false, http.StatusBadRequest)
			return
		}
		module.Fields[field.key] = value
	}
	itemFields := allowedBuilderItemFields(spec, features)
	itemCount := 0
	if len(itemFields) > 0 {
		itemCount, err = strconv.Atoi(r.PostForm.Get("item_count"))
		if err != nil || itemCount < 0 || itemCount > builderItemLimit(spec, order, features) {
			s.renderInvitationBuilder(w, r, order, features, moduleID, "Jumlah item melebihi batas paket.", false, http.StatusBadRequest)
			return
		}
	}
	items := make([]map[string]string, 0, itemCount)
	for index := 0; index < itemCount; index++ {
		item := make(map[string]string, len(itemFields))
		empty := true
		for _, field := range itemFields {
			key := fmt.Sprintf("item_%d_%s", index, field.key)
			value := strings.TrimSpace(r.PostForm.Get(key))
			if utf8.RuneCountInString(value) > field.maxLength && field.maxLength > 0 {
				s.renderInvitationBuilder(w, r, order, features, moduleID, field.label+" terlalu panjang.", false, http.StatusBadRequest)
				return
			}
			if err := validateBuilderField(field, value); err != nil {
				s.renderInvitationBuilder(w, r, order, features, moduleID, err.Error(), false, http.StatusBadRequest)
				return
			}
			item[field.key] = value
			empty = empty && value == ""
		}
		if !empty {
			items = append(items, item)
		}
	}
	if moduleID == "gallery" && int64(len(items)) > order.MaxPhotos {
		s.renderInvitationBuilder(w, r, order, features, moduleID, "Jumlah foto melebihi batas paket.", false, http.StatusBadRequest)
		return
	}
	if moduleID == "video" && int64(len(items)) > order.MaxVideos {
		s.renderInvitationBuilder(w, r, order, features, moduleID, "Jumlah video melebihi batas paket.", false, http.StatusBadRequest)
		return
	}
	if moduleID == "events" && int64(len(items)) > effectiveEventLimit(order, features) {
		s.renderInvitationBuilder(w, r, order, features, moduleID, "Jumlah acara melebihi batas paket.", false, http.StatusBadRequest)
		return
	}
	module.Items = items
	draft.Modules[moduleID] = module
	if err := validateDraftMediaReferences(r, s, order, moduleID, module); err != nil {
		s.renderInvitationBuilder(w, r, order, features, moduleID, err.Error(), false, http.StatusBadRequest)
		return
	}
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		s.fail(w, "encode invitation draft", err)
		return
	}
	if len(draftJSON) > maxInvitationDraftSize {
		http.Error(w, "Draft undangan terlalu besar.", http.StatusRequestEntityTooLarge)
		return
	}
	slug := order.InvitationSlug
	if moduleID == "settings" && features.CustomURL {
		slug = module.Fields["slug"]
		if slug != "" && !invitationSlugPattern.MatchString(slug) {
			s.renderInvitationBuilder(w, r, order, features, moduleID, "Slug hanya boleh berisi 3-32 huruf kecil, angka, atau tanda hubung.", false, http.StatusBadRequest)
			return
		}
		if slug != "" {
			available, err := s.DB.InvitationSlugAvailable(r.Context(), slug, order.ID)
			if err != nil {
				s.fail(w, "check invitation slug availability", err)
				return
			}
			if !available {
				s.renderInvitationBuilder(w, r, order, features, moduleID, "Slug tersebut sudah digunakan pesanan lain.", false, http.StatusConflict)
				return
			}
		}
	}
	if moduleID == "music" && module.Fields["track_id"] != "" {
		trackID, err := strconv.ParseInt(module.Fields["track_id"], 10, 64)
		if err != nil {
			s.renderInvitationBuilder(w, r, order, features, moduleID, "Pilih musik yang tersedia dari daftar.", false, http.StatusBadRequest)
			return
		}
		track, err := s.DB.GetMusicTrack(r.Context(), trackID)
		if errors.Is(err, storage.ErrNotFound) || (err == nil && track.IsActive != 1) {
			s.renderInvitationBuilder(w, r, order, features, moduleID, "Musik yang dipilih tidak lagi tersedia.", false, http.StatusBadRequest)
			return
		}
		if err != nil {
			s.fail(w, "validate invitation music selection", err)
			return
		}
	}
	if err := s.DB.SaveInvitationDraft(r.Context(), order.ID, version, string(draftJSON), slug); err != nil {
		if errors.Is(err, storage.ErrDraftConflict) {
			if r.Header.Get("X-Builder-Autosave") == "true" {
				http.Error(w, "Draft berubah di tab atau perangkat lain. Perubahan lokal dipertahankan.", http.StatusConflict)
				return
			}
			s.renderInvitationBuilder(w, r, order, features, moduleID, "Draft berubah di tab atau perangkat lain. Muat ulang sebelum menyimpan.", false, http.StatusConflict)
			return
		}
		s.fail(w, "save invitation module", err)
		return
	}
	if r.Header.Get("X-Builder-Autosave") == "true" {
		responseBody, err := json.Marshal(struct {
			Version int64 `json:"version"`
		}{Version: version + 1})
		if err != nil {
			s.fail(w, "encode invitation autosave response", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(append(responseBody, '\n')); err != nil {
			s.fail(w, "write invitation autosave response", err)
		}
		return
	}
	http.Redirect(w, r, "/orders/"+url.PathEscape(order.OrderNumber)+"/builder/"+url.PathEscape(moduleID)+"?saved=1", http.StatusSeeOther)
}

func (s *Server) renderInvitationBuilder(w http.ResponseWriter, r *http.Request, order storage.InvitationOrder, features storage.InvitationFeatures, moduleID, message string, preview bool, status int) {
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode invitation draft", err)
		return
	}
	media, err := s.DB.ListInvitationMedia(r.Context(), order.ID)
	if err != nil {
		s.fail(w, "list invitation media", err)
		return
	}
	spec, found := findInvitationModule(moduleID)
	if !found {
		http.NotFound(w, r)
		return
	}
	if !builderModuleAvailable(spec, features, order) {
		message = "Fitur ini belum termasuk dalam paket pesanan."
	}
	module := draft.Modules[moduleID]
	page, err := BuildPageData(r, s.Auth, spec.title+" | Builder Undangan", "Editor undangan pribadi untuk pesanan "+order.OrderNumber+".")
	if err != nil {
		s.fail(w, "build invitation builder page", err)
		return
	}
	data := BuilderPageData{
		Page: page, Order: order, Draft: draft, Module: BuilderModule{ID: spec.id, Title: spec.title, Available: builderModuleAvailable(spec, features, order), Complete: moduleIsComplete(spec, module)},
		Modules: builderModuleNav(draft, features, order), Error: message, Saved: r.URL.Query().Get("saved") == "1",
		Media: media, Preview: preview, PhotoLimit: order.MaxPhotos,
	}
	for _, item := range media {
		if item.MediaType != "audio" {
			data.PhotoCount++
		}
	}
	fields := allowedBuilderFields(spec, features)
	settingDefaults := map[string]string{}
	if moduleID == "settings" {
		settingDefaults, err = invitationSettingDefaults(order, features)
		if err != nil {
			s.fail(w, "load invitation setting defaults", err)
			return
		}
	}
	if moduleID == "music" {
		tracks, err := s.DB.ListMusicTracks(r.Context(), true)
		if err != nil {
			s.fail(w, "load invitation music options", err)
			return
		}
		for index := range fields {
			if fields[index].key == "track_id" {
				for _, track := range tracks {
					fields[index].options = append(fields[index].options, BuilderOption{Value: strconv.FormatInt(track.ID, 10), Label: track.Title})
				}
			}
		}
	}
	for _, field := range fields {
		value := module.Fields[field.key]
		if value == "" {
			value = settingDefaults[field.key]
		}
		data.Fields = append(data.Fields, makeBuilderField(field, value, media))
	}
	if len(spec.itemFields) > 0 {
		fields := allowedBuilderItemFields(spec, features)
		rows := module.Items
		limit := builderItemLimit(spec, order, features)
		data.ItemLimit = limit
		if len(rows) < limit {
			rows = append(append([]map[string]string(nil), rows...), map[string]string{})
		}
		for _, row := range rows {
			item := BuilderItem{}
			for _, field := range fields {
				item.Fields = append(item.Fields, makeBuilderField(field, row[field.key], media))
			}
			data.Items = append(data.Items, item)
		}
	}
	if moduleID == "settings" && features.CustomURL {
		for index := range data.Fields {
			if data.Fields[index].Key == "slug" && data.Fields[index].Value == "" {
				data.Fields[index].Value = order.InvitationSlug
			}
		}
	}
	s.render(w, r, status, InvitationBuilderPage(data))
}

func makeBuilderField(field builderFieldSpec, value string, media []storage.InvitationMedia) BuilderField {
	result := BuilderField{Key: field.key, Label: field.label, Type: field.kind, MaxLength: field.maxLength, Options: field.options, Value: value}
	if field.kind == "media" || field.kind == "audio" {
		for _, item := range media {
			if field.kind == "audio" && item.MediaType != "audio" {
				continue
			}
			if field.kind == "media" && item.MediaType == "audio" {
				continue
			}
			result.MediaOptions = append(result.MediaOptions, BuilderMediaOption{ID: item.ID, Label: fmt.Sprintf("%s · %s", item.MediaType, item.Caption), MediaID: strconv.FormatInt(item.ID, 10)})
		}
	}
	return result
}

func builderModuleNav(draft InvitationDraft, features storage.InvitationFeatures, order storage.InvitationOrder) []BuilderModule {
	modules := make([]BuilderModule, 0, len(invitationModuleSpecs()))
	for _, spec := range invitationModuleSpecs() {
		module := draft.Modules[spec.id]
		modules = append(modules, BuilderModule{
			ID: spec.id, Title: spec.title, Complete: moduleIsComplete(spec, module),
			Available: builderModuleAvailable(spec, features, order),
		})
	}
	return modules
}

func findInvitationModule(id string) (builderModuleSpec, bool) {
	for _, spec := range invitationModuleSpecs() {
		if spec.id == id {
			return spec, true
		}
	}
	return builderModuleSpec{}, false
}

func moduleIsComplete(spec builderModuleSpec, module InvitationModule) bool {
	if len(spec.completeOn) == 0 {
		for _, value := range module.Fields {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
		for _, item := range module.Items {
			for _, value := range item {
				if strings.TrimSpace(value) != "" {
					return true
				}
			}
		}
		return false
	}
	if len(spec.itemFields) > 0 {
		for _, item := range module.Items {
			complete := true
			for _, key := range spec.completeOn {
				complete = complete && strings.TrimSpace(item[key]) != ""
			}
			if complete {
				return true
			}
		}
		return false
	}
	for _, key := range spec.completeOn {
		if strings.TrimSpace(module.Fields[key]) == "" {
			return false
		}
	}
	return true
}

func builderModuleAvailable(spec builderModuleSpec, features storage.InvitationFeatures, order storage.InvitationOrder) bool {
	switch spec.entitlement {
	case "":
		if spec.id == "video" {
			return order.MaxVideos > 0
		}
		return true
	case "couple_story":
		return features.CoupleStory
	case "background_music":
		return features.BackgroundMusic
	case "rsvp_enabled":
		return features.RSVPEnabled
	case "guestbook_enabled":
		return features.GuestbookEnabled
	case "digital_envelope":
		return features.DigitalEnvelope
	case "guest_greeting":
		return features.GuestGreeting
	default:
		return false
	}
}

func allowedBuilderFields(spec builderModuleSpec, features storage.InvitationFeatures) []builderFieldSpec {
	fields := make([]builderFieldSpec, 0, len(spec.fields))
	for _, field := range spec.fields {
		if spec.id == "music" && field.key == "media_id" && !features.CustomMusic {
			continue
		}
		if spec.id == "quotes" && field.key == "quote_extra" && !features.CustomWording {
			continue
		}
		if spec.id == "settings" {
			if strings.HasPrefix(field.key, "section_") && !features.CustomSections {
				continue
			}
			switch field.key {
			case "slug":
				if features.CustomURL {
					fields = append(fields, field)
				}
				continue
			case "background", "surface", "accent", "text":
				if features.CustomColors {
					fields = append(fields, field)
				}
				continue
			case "heading_font", "body_font":
				if features.CustomFonts {
					fields = append(fields, field)
				}
				continue
			case "layout":
				if features.CustomLayout {
					fields = append(fields, field)
				}
				continue
			case "cover_style":
				if features.CustomCover {
					fields = append(fields, field)
				}
				continue
			}
		}
		if spec.id == "cover" && field.key == "cover_media_id" && !features.CustomBackground {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

func allowedBuilderItemFields(spec builderModuleSpec, features storage.InvitationFeatures) []builderFieldSpec {
	if spec.id != "events" || features.EventCustomization {
		return spec.itemFields
	}
	fields := make([]builderFieldSpec, 0, len(spec.itemFields))
	for _, field := range spec.itemFields {
		if field.key == "end_time" || field.key == "timezone" || field.key == "maps_url" || field.key == "description" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

func invitationSettingDefaults(order storage.InvitationOrder, features storage.InvitationFeatures) (map[string]string, error) {
	template, err := decodePreviewConfig(storage.Template{ID: order.TemplateID, ConfigJSON: []byte(order.TemplateConfig)})
	if err != nil {
		return nil, err
	}
	defaults := map[string]string{}
	if features.CustomURL {
		defaults["slug"] = order.InvitationSlug
	}
	if features.CustomColors {
		defaults["background"] = template.Palette.Background
		defaults["surface"] = template.Palette.Surface
		defaults["accent"] = template.Palette.Accent
		defaults["text"] = template.Palette.Text
	}
	if features.CustomFonts {
		defaults["heading_font"] = template.Typography.Heading
		defaults["body_font"] = template.Typography.Body
	}
	if features.CustomLayout {
		defaults["layout"] = template.Layout
	}
	if features.CustomCover {
		defaults["cover_style"] = template.CoverStyle
	}
	if features.CustomSections {
		selected := make(map[string]bool, len(template.Sections))
		for _, section := range template.Sections {
			selected[section] = true
		}
		for _, section := range []string{"cover", "couple", "story", "events", "gallery", "rsvp", "gift"} {
			defaults["section_"+section] = strconv.FormatBool(selected[section])
		}
	}
	return defaults, nil
}

func builderItemLimit(spec builderModuleSpec, order storage.InvitationOrder, features storage.InvitationFeatures) int {
	switch spec.id {
	case "events":
		return int(effectiveEventLimit(order, features))
	case "gallery":
		return int(order.MaxPhotos)
	case "video":
		return int(order.MaxVideos)
	case "story":
		return 50
	default:
		return 1
	}
}

func effectiveEventLimit(order storage.InvitationOrder, features storage.InvitationFeatures) int64 {
	limit := order.MaxEvents
	if limit < 1 {
		limit = 1
	}
	if !features.MultipleEvents && limit > 1 {
		return 1
	}
	return limit
}

func validateBuilderField(field builderFieldSpec, value string) error {
	if value == "" {
		return nil
	}
	switch field.kind {
	case "select":
		for _, option := range field.options {
			if value == option.Value {
				return nil
			}
		}
		return fmt.Errorf("Pilihan untuk %s tidak valid.", field.label)
	case "url":
		parsed, err := url.ParseRequestURI(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("Tautan pada %s harus menggunakan URL HTTP atau HTTPS yang valid.", field.label)
		}
		if field.key == "url" {
			host := strings.ToLower(parsed.Hostname())
			if host != "youtube.com" && host != "www.youtube.com" && host != "youtu.be" && host != "vimeo.com" && host != "www.vimeo.com" && !strings.HasSuffix(host, ".vimeo.com") {
				return errors.New("Video hanya boleh berasal dari YouTube atau Vimeo.")
			}
		}
	case "color":
		if !previewColor.MatchString(value) {
			return fmt.Errorf("Warna pada %s harus menggunakan format #RRGGBB.", field.label)
		}
	case "date":
		if _, err := timeParseDate(value); err != nil {
			return fmt.Errorf("Tanggal pada %s tidak valid.", field.label)
		}
	case "time":
		if _, err := time.Parse("15:04", value); err != nil {
			return fmt.Errorf("Waktu pada %s tidak valid.", field.label)
		}
	case "number":
		number, err := strconv.Atoi(value)
		if err != nil || number < 0 || number > 99 {
			return fmt.Errorf("Nilai pada %s harus berupa angka 0 sampai 99.", field.label)
		}
	case "slug":
		if !invitationSlugPattern.MatchString(value) {
			return errors.New("Slug hanya boleh berisi 3-32 huruf kecil, angka, atau tanda hubung.")
		}
	}
	return nil
}

func timeParseDate(value string) (string, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", err
	}
	return parsed.Format("2006-01-02"), nil
}

func validateDraftMediaReferences(r *http.Request, s *Server, order storage.InvitationOrder, moduleID string, module InvitationModule) error {
	keys := []string{"cover_media_id", "media_id"}
	for _, key := range keys {
		if value := module.Fields[key]; value != "" {
			if err := validateMediaReference(r, s, order.ID, value, key == "media_id" && moduleID == "music"); err != nil {
				return err
			}
		}
	}
	for _, item := range module.Items {
		if value := item["media_id"]; value != "" {
			if err := validateMediaReference(r, s, order.ID, value, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMediaReference(r *http.Request, s *Server, orderID int64, raw string, audio bool) error {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return errors.New("Pilih media yang valid.")
	}
	media, err := s.DB.GetInvitationMedia(r.Context(), orderID, id)
	if errors.Is(err, storage.ErrNotFound) {
		return errors.New("Media tidak ditemukan pada pesanan ini.")
	}
	if err != nil {
		return fmt.Errorf("memeriksa media pesanan: %w", err)
	}
	if audio != (media.MediaType == "audio") {
		return errors.New("Jenis media yang dipilih tidak sesuai.")
	}
	return nil
}

func (s *Server) handleInvitationMediaUpload(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
	if !ok {
		return
	}
	if r.ContentLength > maxInvitationMediaSize+(1<<20) {
		http.Error(w, "File terlalu besar.", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxInvitationMediaSize+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, "Form unggahan terlalu besar atau tidak valid.", http.StatusRequestEntityTooLarge)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	mediaType := strings.TrimSpace(r.PostForm.Get("media_type"))
	if mediaType != "portrait" && mediaType != "cover" && mediaType != "story" && mediaType != "gallery" && mediaType != "audio" {
		http.Error(w, "Jenis media tidak valid.", http.StatusBadRequest)
		return
	}
	if !invitationMediaTypeAvailable(mediaType, features) {
		http.Error(w, "Jenis media ini tidak termasuk dalam paket pesanan.", http.StatusForbidden)
		return
	}
	file, header, err := r.FormFile("media")
	if err != nil {
		http.Error(w, "Pilih file media.", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxInvitationMediaSize {
		http.Error(w, "File media maksimal 5 MiB.", http.StatusRequestEntityTooLarge)
		return
	}
	if s.DB.UploadDir == "" {
		http.Error(w, "Penyimpanan media belum dikonfigurasi.", http.StatusServiceUnavailable)
		return
	}
	directory := filepath.Join(s.DB.UploadDir, "invitation-media")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		s.fail(w, "create invitation media directory", err)
		return
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		s.fail(w, "restrict invitation media directory", err)
		return
	}
	temporary, err := os.CreateTemp(directory, ".invitation-upload-*")
	if err != nil {
		s.fail(w, "create temporary invitation media file", err)
		return
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.fail(w, "clean temporary invitation media", err)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		s.fail(w, "restrict temporary invitation media", err)
		return
	}
	size, err := io.Copy(temporary, io.LimitReader(file, maxInvitationMediaSize+1))
	if err != nil {
		_ = temporary.Close()
		s.fail(w, "stream invitation upload", err)
		return
	}
	if size == 0 || size > maxInvitationMediaSize {
		_ = temporary.Close()
		http.Error(w, "File media maksimal 5 MiB.", http.StatusRequestEntityTooLarge)
		return
	}
	if err := temporary.Close(); err != nil {
		s.fail(w, "close temporary invitation media", err)
		return
	}
	contentType, extension, err := validateInvitationMediaFile(mediaType, temporaryPath)
	if err != nil {
		http.Error(w, "Format file media tidak didukung atau isi file tidak valid.", http.StatusBadRequest)
		return
	}
	var randomName [18]byte
	if _, err := rand.Read(randomName[:]); err != nil {
		s.fail(w, "generate invitation media name", err)
		return
	}
	storedName := hex.EncodeToString(randomName[:]) + extension
	path := filepath.Join(directory, storedName)
	if err := os.Rename(temporaryPath, path); err != nil {
		s.fail(w, "move validated invitation media", err)
		return
	}
	mediaID, err := s.DB.CreateInvitationMedia(r.Context(), storage.InvitationMedia{
		OrderID: order.ID, MediaType: mediaType, StoredName: storedName,
		MimeType: contentType, SizeBytes: size, Caption: "", SortOrder: 0,
	}, order.MaxPhotos, features.CustomMusic)
	if err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			s.fail(w, "clean rejected invitation media", fmt.Errorf("%v; cleanup: %w", err, removeErr))
			return
		}
		if errors.Is(err, storage.ErrInvitationMediaLimit) {
			http.Error(w, "Batas jumlah media untuk paket ini telah tercapai.", http.StatusConflict)
			return
		}
		s.fail(w, "save invitation media metadata", err)
		return
	}
	_ = mediaID
	http.Redirect(w, r, "/orders/"+url.PathEscape(order.OrderNumber)+"/builder/gallery?saved=1", http.StatusSeeOther)
}

func invitationMediaTypeAvailable(mediaType string, features storage.InvitationFeatures) bool {
	switch mediaType {
	case "audio":
		return features.BackgroundMusic && features.CustomMusic
	case "cover":
		return features.CustomBackground
	case "story":
		return features.CoupleStory
	default:
		return mediaType == "portrait" || mediaType == "gallery"
	}
}

func (s *Server) handleInvitationMediaDetails(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
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
		s.fail(w, "load invitation media details", err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form media tidak valid.", http.StatusBadRequest)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("draft_version"), 10, 64)
	if err != nil || version != order.DraftVersion {
		s.renderInvitationBuilder(w, r, order, features, "gallery", "Draft berubah di tab atau perangkat lain. Muat ulang sebelum menyimpan media.", false, http.StatusConflict)
		return
	}
	caption := strings.TrimSpace(r.PostForm.Get("media_caption"))
	if utf8.RuneCountInString(caption) > 200 {
		s.renderInvitationBuilder(w, r, order, features, "gallery", "Caption media maksimal 200 karakter.", false, http.StatusBadRequest)
		return
	}
	sortOrder, err := strconv.ParseInt(r.PostForm.Get("media_sort_order"), 10, 64)
	mediaItems, listErr := s.DB.ListInvitationMedia(r.Context(), order.ID)
	if listErr != nil {
		s.fail(w, "list invitation media for ordering", listErr)
		return
	}
	if err != nil || sortOrder < 0 || sortOrder >= int64(len(mediaItems)) {
		s.renderInvitationBuilder(w, r, order, features, "gallery", "Urutan media tidak valid.", false, http.StatusBadRequest)
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode invitation draft for media details", err)
		return
	}
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		s.fail(w, "encode invitation draft for media details", err)
		return
	}
	if err := s.DB.SaveInvitationMediaDetails(r.Context(), order.ID, media.ID, version, string(draftJSON), order.InvitationSlug, caption, sortOrder); err != nil {
		if errors.Is(err, storage.ErrDraftConflict) {
			s.renderInvitationBuilder(w, r, order, features, "gallery", "Draft berubah di tab atau perangkat lain. Muat ulang sebelum menyimpan media.", false, http.StatusConflict)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, "save invitation media details", err)
		return
	}
	http.Redirect(w, r, "/orders/"+url.PathEscape(order.OrderNumber)+"/builder/gallery?saved=1", http.StatusSeeOther)
}

func (s *Server) handleInvitationMediaDelete(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
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
		s.fail(w, "load invitation media for deletion", err)
		return
	}
	if !safeInvitationStoredName(media.StoredName) || s.DB.UploadDir == "" {
		s.fail(w, "delete invitation media file", errors.New("invalid private media path"))
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form penghapusan media tidak valid.", http.StatusBadRequest)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("draft_version"), 10, 64)
	if err != nil || version != order.DraftVersion {
		s.renderInvitationBuilder(w, r, order, features, "gallery", "Draft berubah di tab atau perangkat lain. Muat ulang sebelum menghapus media.", false, http.StatusConflict)
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode draft before media deletion", err)
		return
	}
	for moduleID, module := range draft.Modules {
		for key, value := range module.Fields {
			if value == strconv.FormatInt(mediaID, 10) && (key == "media_id" || key == "cover_media_id") {
				module.Fields[key] = ""
			}
		}
		filtered := module.Items[:0]
		for _, item := range module.Items {
			if item["media_id"] == strconv.FormatInt(mediaID, 10) && moduleID == "gallery" {
				continue
			}
			if item["media_id"] == strconv.FormatInt(mediaID, 10) {
				item["media_id"] = ""
			}
			filtered = append(filtered, item)
		}
		module.Items = filtered
		draft.Modules[moduleID] = module
	}
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		s.fail(w, "encode draft after media deletion", err)
		return
	}
	if err := s.DB.DeleteInvitationMediaWithDraft(r.Context(), order.ID, mediaID, version, string(draftJSON), order.InvitationSlug); err != nil {
		if errors.Is(err, storage.ErrDraftConflict) {
			s.renderInvitationBuilder(w, r, order, features, "gallery", "Draft berubah di tab atau perangkat lain. Muat ulang sebelum menghapus media.", false, http.StatusConflict)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, "unlink and delete invitation media", err)
		return
	}
	path := filepath.Join(s.DB.UploadDir, "invitation-media", media.StoredName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.fail(w, "delete invitation media file", err)
		return
	}
	http.Redirect(w, r, "/orders/"+url.PathEscape(order.OrderNumber)+"/builder/gallery?saved=1", http.StatusSeeOther)
}

func validateInvitationMedia(mediaType string, data []byte) (string, string, error) {
	if mediaType != "audio" {
		switch {
		case len(data) >= 3 && string(data[:3]) == "\xff\xd8\xff":
			if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
				return "", "", err
			}
			return "image/jpeg", ".jpg", nil
		case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
			if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
				return "", "", err
			}
			return "image/png", ".png", nil
		case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" &&
			binary.LittleEndian.Uint32(data[4:8]) >= 4 && uint64(binary.LittleEndian.Uint32(data[4:8]))+8 <= uint64(len(data)):
			return "image/webp", ".webp", nil
		default:
			return "", "", errors.New("invalid image signature")
		}
	}
	if validMP3Frame(data) || validID3MP3(data) {
		return "audio/mpeg", ".mp3", nil
	}
	if validOggAudio(data) {
		return "audio/ogg", ".ogg", nil
	}
	return "", "", errors.New("invalid audio signature")
}

func validateInvitationMediaFile(mediaType, path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	header := make([]byte, 12)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", "", err
	}
	header = header[:n]
	if mediaType != "audio" {
		switch {
		case len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff}):
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return "", "", err
			}
			if _, _, err := image.DecodeConfig(file); err != nil {
				return "", "", err
			}
			return "image/jpeg", ".jpg", nil
		case len(header) >= 8 && bytes.Equal(header[:8], []byte("\x89PNG\r\n\x1a\n")):
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return "", "", err
			}
			if _, _, err := image.DecodeConfig(file); err != nil {
				return "", "", err
			}
			return "image/png", ".png", nil
		case len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP":
			info, err := file.Stat()
			if err != nil {
				return "", "", err
			}
			declaredSize := binary.LittleEndian.Uint32(header[4:8])
			if declaredSize < 4 || uint64(declaredSize)+8 > uint64(info.Size()) {
				return "", "", errors.New("invalid WEBP size")
			}
			return "image/webp", ".webp", nil
		default:
			return "", "", errors.New("invalid image signature")
		}
	}

	if validMP3Frame(header) {
		return "audio/mpeg", ".mp3", nil
	}
	if len(header) >= 10 && string(header[:3]) == "ID3" && header[3] >= 2 && header[3] <= 4 && header[4] != 0xff &&
		header[6]&0x80 == 0 && header[7]&0x80 == 0 && header[8]&0x80 == 0 && header[9]&0x80 == 0 {
		tagSize := int64(header[6])<<21 | int64(header[7])<<14 | int64(header[8])<<7 | int64(header[9])
		offset := int64(10) + tagSize
		if header[5]&0x10 != 0 {
			offset += 10
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return "", "", err
		}
		var frame [4]byte
		if _, err := io.ReadFull(file, frame[:]); err == nil && validMP3Frame(frame[:]) {
			return "audio/mpeg", ".mp3", nil
		}
	}
	if len(header) >= 4 && string(header[:4]) == "OggS" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", "", err
		}
		oggHeader := make([]byte, 27)
		if _, err := io.ReadFull(file, oggHeader); err != nil || oggHeader[4] != 0 {
			return "", "", errors.New("invalid OGG page")
		}
		segmentCount := int(oggHeader[26])
		segments := make([]byte, segmentCount)
		if _, err := io.ReadFull(file, segments); err != nil || segmentCount == 0 {
			return "", "", errors.New("invalid OGG segments")
		}
		packetSize := int(segments[0])
		packet := make([]byte, packetSize)
		if _, err := io.ReadFull(file, packet); err != nil {
			return "", "", err
		}
		if bytes.HasPrefix(packet, []byte("OpusHead")) ||
			bytes.HasPrefix(packet, []byte{0x01, 'v', 'o', 'r', 'b', 'i', 's'}) ||
			bytes.HasPrefix(packet, []byte("Speex   ")) ||
			bytes.HasPrefix(packet, []byte{0x7f, 'F', 'L', 'A', 'C'}) {
			return "audio/ogg", ".ogg", nil
		}
	}
	return "", "", errors.New("invalid audio signature")
}

func validMP3Frame(data []byte) bool {
	if len(data) < 4 || data[0] != 0xff || data[1]&0xe0 != 0xe0 {
		return false
	}
	version := (data[1] >> 3) & 0x03
	layer := (data[1] >> 1) & 0x03
	bitrate := data[2] >> 4
	sampleRate := (data[2] >> 2) & 0x03
	return version != 0x01 && layer != 0 && bitrate != 0 && bitrate != 0x0f && sampleRate != 0x03
}

func validID3MP3(data []byte) bool {
	if len(data) < 14 || string(data[:3]) != "ID3" || data[3] < 2 || data[3] > 4 || data[4] == 0xff {
		return false
	}
	if data[6]&0x80 != 0 || data[7]&0x80 != 0 || data[8]&0x80 != 0 || data[9]&0x80 != 0 {
		return false
	}
	tagSize := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
	frameOffset := 10 + tagSize
	if data[5]&0x10 != 0 {
		frameOffset += 10
	}
	return frameOffset+4 <= len(data) && validMP3Frame(data[frameOffset:])
}

func validOggAudio(data []byte) bool {
	if len(data) < 28 || string(data[:4]) != "OggS" || data[4] != 0 {
		return false
	}
	headerSize := 27 + int(data[26])
	if len(data) <= headerSize {
		return false
	}
	packet := data[headerSize:]
	return bytes.HasPrefix(packet, []byte("OpusHead")) ||
		bytes.HasPrefix(packet, []byte{0x01, 'v', 'o', 'r', 'b', 'i', 's'}) ||
		bytes.HasPrefix(packet, []byte("Speex   ")) ||
		bytes.HasPrefix(packet, []byte{0x7f, 'F', 'L', 'A', 'C'})
}

func (s *Server) handleInvitationMedia(w http.ResponseWriter, r *http.Request) {
	_, order, _, ok := s.getBuilderOrder(w, r)
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
		s.fail(w, "load private invitation media", err)
		return
	}
	if !safeInvitationStoredName(media.StoredName) || s.DB.UploadDir == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.DB.UploadDir, "invitation-media", media.StoredName)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, "open private invitation media", err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", media.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(media.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.CopyN(w, file, media.SizeBytes); err != nil && !errors.Is(err, io.EOF) {
		s.fail(w, "serve private invitation media", err)
	}
}

func applyInvitationPreviewSettings(config *PreviewConfig, draft InvitationDraft, features storage.InvitationFeatures) error {
	settings := draft.Modules["settings"].Fields
	if features.CustomColors {
		for key, target := range map[string]*string{
			"background": &config.Palette.Background, "surface": &config.Palette.Surface,
			"accent": &config.Palette.Accent, "text": &config.Palette.Text,
		} {
			if value := settings[key]; value != "" {
				color, err := configColor(value, "")
				if err != nil {
					return fmt.Errorf("draft setting %s: %w", key, err)
				}
				*target = color
			}
		}
	}
	if features.CustomFonts {
		for key, target := range map[string]*string{
			"heading_font": &config.Typography.Heading, "body_font": &config.Typography.Body,
		} {
			if value := settings[key]; value != "" {
				if !validPreviewFont(value) {
					return fmt.Errorf("draft setting %s has unsupported font %q", key, value)
				}
				*target = value
			}
		}
	}
	if features.CustomLayout && settings["layout"] != "" {
		if !oneOf(settings["layout"], "classic", "editorial", "story-first") {
			return fmt.Errorf("draft has unsupported layout %q", settings["layout"])
		}
		config.Layout = settings["layout"]
	}
	if features.CustomCover && settings["cover_style"] != "" {
		if !oneOf(settings["cover_style"], "framed", "torii", "split", "fullbleed", "parallax") {
			return fmt.Errorf("draft has unsupported cover style %q", settings["cover_style"])
		}
		config.CoverStyle = settings["cover_style"]
	}
	if features.CustomSections {
		sections := make([]string, 0, len(config.Sections))
		for _, section := range []string{"cover", "couple", "story", "events", "gallery", "rsvp", "gift"} {
			value, set := settings["section_"+section]
			if set && value == "true" {
				sections = append(sections, section)
			} else if !set {
				for _, defaultSection := range config.Sections {
					if defaultSection == section {
						sections = append(sections, section)
						break
					}
				}
			} else if value != "false" {
				return fmt.Errorf("draft setting section_%s is invalid", section)
			}
		}
		config.Sections = sections
	}
	filtered := config.Sections[:0]
	for _, section := range config.Sections {
		if section == "story" && !features.CoupleStory {
			continue
		}
		if section == "rsvp" && !features.RSVPEnabled {
			continue
		}
		if section == "gift" && !features.DigitalEnvelope {
			continue
		}
		filtered = append(filtered, section)
	}
	config.Sections = filtered
	return nil
}

func invitationPreviewSample(order storage.InvitationOrder, draft InvitationDraft, features storage.InvitationFeatures, mediaURL, musicURL func(int64) string) InvitationSample {
	basic := draft.Modules["basic"].Fields
	cover := draft.Modules["cover"].Fields
	sample := InvitationSample{
		Title: cover["title"], Couple: strings.TrimSpace(basic["bride_name"] + " & " + basic["groom_name"]),
		Subheading: cover["subtitle"],
	}
	if sample.Title == "" {
		sample.Title = "The Wedding of"
	}
	if sample.Couple == "&" || sample.Couple == "" {
		sample.Couple = order.OrderNumber
	}
	events := draft.Modules["events"].Items
	for _, event := range events {
		dateTime := strings.TrimSpace(event["date"] + " · " + event["start_time"])
		sample.Events = append(sample.Events, InvitationEventSample{
			Name: event["name"], Date: dateTime, Location: event["location"], Address: event["address"],
		})
	}
	if len(events) > 0 {
		sample.Date = strings.TrimSpace(events[0]["date"] + " · " + events[0]["start_time"])
		sample.Location = events[0]["location"]
	}
	story := draft.Modules["story"]
	sample.Story = strings.TrimSpace(story.Fields["intro"])
	for _, item := range story.Items {
		part := strings.TrimSpace(item["title"] + "\n" + item["description"])
		if part != "" {
			sample.Story = strings.TrimSpace(sample.Story + "\n\n" + part)
		}
	}
	quotes := draft.Modules["quotes"].Fields
	sample.Quote, sample.QuoteSource, sample.Prayer = quotes["quote_text"], quotes["quote_source"], quotes["prayer"]
	for _, item := range draft.Modules["gallery"].Items {
		if id, err := strconv.ParseInt(item["media_id"], 10, 64); err == nil && id > 0 {
			sample.Gallery = append(sample.Gallery, mediaURL(id))
		}
	}
	for _, item := range draft.Modules["video"].Items {
		if item["url"] != "" {
			sample.Videos = append(sample.Videos, InvitationVideoSample{Title: item["title"], URL: item["url"]})
		}
	}
	if features.GuestGreeting {
		greeting := draft.Modules["greetings"].Fields
		if greeting["enabled"] == "true" {
			sample.Greeting = strings.TrimSpace(greeting["opening"] + "\n" + greeting["guest_name"])
		}
	}
	if features.RSVPEnabled {
		rsvp := draft.Modules["rsvp"].Fields
		if rsvp["enabled"] == "true" {
			sample.RSVP = strings.TrimSpace(rsvp["title"] + " " + rsvp["description"] + " · Pengaturan konfirmasi kehadiran")
		}
	}
	if features.GuestbookEnabled {
		book := draft.Modules["guestbook"].Fields
		if book["enabled"] == "true" {
			sample.Guestbook = strings.TrimSpace(book["title"] + " " + book["description"] + " · Pengaturan buku tamu")
		}
	}
	if features.DigitalEnvelope {
		envelope := draft.Modules["envelope"].Fields
		if envelope["enabled"] == "true" {
			sample.Envelope = strings.TrimSpace(envelope["bank_name"] + "\n" + envelope["account_holder"] + "\n" + envelope["account_number"] + "\n" + envelope["note"])
		}
	}
	if features.BackgroundMusic {
		music := draft.Modules["music"].Fields
		if music["enabled"] == "true" {
			if id, err := strconv.ParseInt(music["media_id"], 10, 64); err == nil && id > 0 {
				sample.MusicURL = mediaURL(id)
			} else if id, err := strconv.ParseInt(music["track_id"], 10, 64); err == nil && id > 0 {
				sample.MusicURL = musicURL(id)
			}
		}
	}
	if cover["cover_media_id"] != "" && features.CustomBackground {
		if id, err := strconv.ParseInt(cover["cover_media_id"], 10, 64); err == nil {
			sample.Thumbnail = mediaURL(id)
		}
	}
	return sample
}

var invitationStoredNamePattern = regexp.MustCompile(`^[a-f0-9]{36}\.(jpg|png|webp|mp3|ogg)$`)

func safeInvitationStoredName(name string) bool {
	return invitationStoredNamePattern.MatchString(name) && filepath.Base(name) == name
}

func (s *Server) handleInvitationPreview(w http.ResponseWriter, r *http.Request) {
	_, order, features, ok := s.getBuilderOrder(w, r)
	if !ok {
		return
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		s.fail(w, "decode invitation preview draft", err)
		return
	}
	template, err := s.DB.GetTemplate(r.Context(), order.TemplateID)
	if err != nil {
		s.fail(w, "load invitation preview template", err)
		return
	}
	config, err := decodePreviewConfig(*template)
	if err != nil {
		s.fail(w, "decode invitation preview template", err)
		return
	}
	if err := applyInvitationPreviewSettings(&config, draft, features); err != nil {
		s.fail(w, "apply invitation draft preview settings", err)
		return
	}
	sample := invitationPreviewSample(order, draft, features, func(mediaID int64) string {
		return "/orders/" + url.PathEscape(order.OrderNumber) + "/builder/media/" + strconv.FormatInt(mediaID, 10)
	}, func(trackID int64) string {
		return "/orders/" + url.PathEscape(order.OrderNumber) + "/builder/music/" + strconv.FormatInt(trackID, 10)
	})
	if sample.Thumbnail == "" {
		sample.Thumbnail = config.Media.Cover
	}
	page, err := BuildPageData(r, s.Auth, "Pratinjau undangan | SUKAMOTO", "Pratinjau pribadi yang hanya dapat dibuka dengan akses pesanan.")
	if err != nil {
		s.fail(w, "build invitation preview page", err)
		return
	}
	s.render(w, r, http.StatusOK, InvitationPreviewPage(BuilderPageData{
		Page: page, Order: order, Draft: draft, Module: BuilderModule{Title: "Pratinjau"},
		Modules: builderModuleNav(draft, features, order),
	}, config, sample))
}
