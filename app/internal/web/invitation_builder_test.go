package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sukamoto/app/internal/storage"
)

var draftVersionInput = regexp.MustCompile(`name="draft_version" value="([0-9]+)"`)

func TestInvitationBuilderRequiresOrderAccessAndSavesModulesIndependently(t *testing.T) {
	db, server := testServer(t)
	client := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, client, server, "S01", "silver")
	if _, err := db.SQL.Exec(`UPDATE package_features SET couple_story = 0 WHERE package_id = 'silver'`); err != nil {
		t.Fatal(err)
	}
	builderURL := server.URL + "/orders/" + orderNumber + "/builder"

	unownedClient := newTestClient(t)
	response, err := unownedClient.Get(builderURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("builder without order cookie status = %d, want 404", response.StatusCode)
	}

	basicURL := builderURL + "/basic"
	csrf := getCSRF(t, client, basicURL)
	response = postForm(t, client, basicURL, url.Values{
		"csrf_token": {csrf}, "draft_version": {"1"},
		"field_bride_name": {"Ayu"}, "field_bride_parent": {"Keluarga A"},
		"field_groom_name": {"Bayu"}, "field_groom_parent": {"Keluarga B"},
	})
	body := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Perubahan berhasil disimpan") {
		t.Fatalf("save basic module status = %d, body: %s", response.StatusCode, body)
	}

	coverURL := builderURL + "/cover"
	csrf = getCSRF(t, client, coverURL)
	response, err = client.Get(coverURL)
	if err != nil {
		t.Fatal(err)
	}
	coverPage, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	version := draftVersionInput.FindStringSubmatch(string(coverPage))
	if len(version) != 2 || version[1] != "2" {
		t.Fatalf("cover page draft version = %v, want 2", version)
	}
	response = postForm(t, client, coverURL, url.Values{
		"csrf_token": {csrf}, "draft_version": {version[1]},
		"field_title": {"Undangan Ayu & Bayu"}, "field_subtitle": {"Dengan penuh rasa syukur"},
	})
	body = readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Perubahan berhasil disimpan") {
		t.Fatalf("save cover module status = %d, body: %s", response.StatusCode, body)
	}

	order, err := db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	var draft InvitationDraft
	if err := json.Unmarshal([]byte(order.DraftData), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Modules["basic"].Fields["bride_name"] != "Ayu" || draft.Modules["cover"].Fields["title"] != "Undangan Ayu & Bayu" {
		t.Fatalf("saving another module overwrote persisted draft values: %#v", draft.Modules)
	}
	if order.DraftVersion != 3 {
		t.Fatalf("draft version = %d, want 3", order.DraftVersion)
	}

	storyURL := builderURL + "/story"
	csrf = getCSRF(t, client, storyURL)
	response = postForm(t, client, storyURL, url.Values{
		"csrf_token": {csrf}, "draft_version": {"3"},
		"field_intro": {"Kisah kami"}, "item_count": {"1"},
	})
	body = readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unentitled story module status = %d, want 403: %s", response.StatusCode, body)
	}

	response = postForm(t, client, basicURL, url.Values{
		"csrf_token": {"invalid"}, "draft_version": {"3"},
		"field_bride_name": {"Changed"}, "field_groom_name": {"Changed"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid CSRF token status = %d, want 403", response.StatusCode)
	}
}

func TestInvitationBuilderDynamicItemsAutosaveAndVersionConflicts(t *testing.T) {
	db, server := testServer(t)
	client := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, client, server, "S01", "silver")
	moduleURL := server.URL + "/orders/" + orderNumber + "/builder/story"
	response, err := client.Get(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		!strings.Contains(page, "data-item-add") ||
		!strings.Contains(page, "data-item-template") ||
		!strings.Contains(page, "Hapus item") ||
		!strings.Contains(page, "Naikkan") ||
		!strings.Contains(page, "invitation-builder.js") {
		t.Fatalf("story builder has incomplete dynamic item controls: %s", page)
	}
	csrf := csrfInput.FindStringSubmatch(page)
	if len(csrf) != 2 {
		t.Fatal("builder page has no CSRF token")
	}

	saveAutosave := func(values url.Values) *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, moduleURL, strings.NewReader(values.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("X-CSRF-Token", csrf[1])
		request.Header.Set("X-Builder-Autosave", "true")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	invalidItem := url.Values{
		"draft_version": {"1"}, "item_count": {"1"},
		"item_0_title": {strings.Repeat("x", 101)},
	}
	response = saveAutosave(invalidItem)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid story item status=%d, want 400", response.StatusCode)
	}
	first := url.Values{
		"draft_version": {"1"}, "field_intro": {"Cerita kami"}, "item_count": {"2"},
		"item_0_title": {"Pertemuan"}, "item_0_description": {"Awal cerita"},
		"item_1_title": {"Lamaran"}, "item_1_description": {"Momen berikutnya"},
	}
	response = saveAutosave(first)
	body := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"version":2`) {
		t.Fatalf("first autosave status=%d body=%s", response.StatusCode, body)
	}

	order, err := db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := decodeInvitationDraft(order.DraftData)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Modules["story"].Items) != 2 || draft.Modules["story"].Items[0]["title"] != "Pertemuan" {
		t.Fatalf("multiple story items were not saved: %#v", draft.Modules["story"].Items)
	}

	reordered := url.Values{
		"draft_version": {"2"}, "field_intro": {"Cerita kami"}, "item_count": {"2"},
		"item_0_title": {"Lamaran"}, "item_0_description": {"Momen berikutnya"},
		"item_1_title": {"Pertemuan"}, "item_1_description": {"Awal cerita"},
	}
	response = saveAutosave(reordered)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reorder autosave status=%d", response.StatusCode)
	}
	order, err = db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = decodeInvitationDraft(order.DraftData)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Modules["story"].Items[0]["title"] != "Lamaran" {
		t.Fatalf("story item order was not preserved: %#v", draft.Modules["story"].Items)
	}

	deleted := url.Values{
		"draft_version": {"3"}, "field_intro": {"Cerita kami"}, "item_count": {"1"},
		"item_0_title": {"Lamaran"}, "item_0_description": {"Momen berikutnya"},
	}
	response = saveAutosave(deleted)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("delete-item autosave status=%d", response.StatusCode)
	}
	order, err = db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = decodeInvitationDraft(order.DraftData)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Modules["story"].Items) != 1 || draft.Modules["story"].Items[0]["title"] != "Lamaran" {
		t.Fatalf("deleted story item remains in saved draft: %#v", draft.Modules["story"].Items)
	}

	stale := url.Values{
		"draft_version": {"3"}, "field_intro": {"stale"}, "item_count": {"0"},
	}
	response = saveAutosave(stale)
	body = readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "versi") {
		t.Fatalf("stale autosave status=%d body=%s, want 409", response.StatusCode, body)
	}

	eventsURL := server.URL + "/orders/" + orderNumber + "/builder/events"
	csrf = csrfInput.FindStringSubmatch(page)
	response = postForm(t, client, eventsURL, url.Values{
		"csrf_token": {csrf[1]}, "draft_version": {strconv.FormatInt(order.DraftVersion, 10)},
		"item_count": {"2"}, "item_0_name": {"Akad"}, "item_1_name": {"Resepsi"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("events beyond effective package limit status=%d, want 400", response.StatusCode)
	}
}

func TestSilverAndGoldInvitationEntitlementsAndDatabaseLimits(t *testing.T) {
	db, server := testServer(t)
	for _, packageID := range []string{"silver", "gold"} {
		client := newTestClient(t)
		orderNumber, _, _ := createGuestOrderForTest(t, client, server, map[string]string{"silver": "S01", "gold": "G01"}[packageID], packageID)
		order, err := db.GetInvitationOrder(context.Background(), orderNumber)
		if err != nil {
			t.Fatal(err)
		}
		features, err := db.GetInvitationFeatures(context.Background(), order.PackageID)
		if err != nil {
			t.Fatal(err)
		}
		expectedPremium := packageID == "gold"
		assertions := map[string]bool{
			"couple story":     features.CoupleStory,
			"background music": features.BackgroundMusic,
			"RSVP":             features.RSVPEnabled,
			"guestbook":        features.GuestbookEnabled,
			"digital envelope": features.DigitalEnvelope,
			"custom colors":    features.CustomColors,
			"custom fonts":     features.CustomFonts,
			"custom layout":    features.CustomLayout,
			"custom URL":       features.CustomURL,
			"custom sections":  features.CustomSections,
			"custom music":     features.CustomMusic,
		}
		for name, enabled := range assertions {
			want := true
			if expectedPremium && (name == "couple story" || name == "background music" || name == "RSVP" || name == "guestbook" || name == "digital envelope") {
				want = true
			} else if !expectedPremium && (name == "custom colors" || name == "custom fonts" || name == "custom layout" || name == "custom URL" || name == "custom sections" || name == "custom music") {
				want = false
			}
			if enabled != want {
				t.Errorf("%s %s entitlement = %v, want %v", packageID, name, enabled, want)
			}
		}
		moduleEntitlements := map[string]bool{
			"story":     features.CoupleStory,
			"music":     features.BackgroundMusic,
			"rsvp":      features.RSVPEnabled,
			"guestbook": features.GuestbookEnabled,
			"envelope":  features.DigitalEnvelope,
		}
		for _, spec := range invitationModuleSpecs() {
			if expected, hasEntitlement := moduleEntitlements[spec.id]; hasEntitlement {
				if got := builderModuleAvailable(spec, features, order); got != expected {
					t.Errorf("%s %s availability = %v, want %v", packageID, spec.id, got, expected)
				}
			}
		}
		settings, _ := findInvitationModule("settings")
		visible := make(map[string]bool)
		for _, field := range allowedBuilderFields(settings, features) {
			visible[field.key] = true
		}
		for key, entitled := range map[string]bool{
			"background":      features.CustomColors,
			"heading_font":    features.CustomFonts,
			"layout":          features.CustomLayout,
			"slug":            features.CustomURL,
			"section_gallery": features.CustomSections,
		} {
			if visible[key] != entitled {
				t.Errorf("%s setting %s visibility = %v, entitlement = %v", packageID, key, visible[key], entitled)
			}
		}
		music, _ := findInvitationModule("music")
		musicFields := make(map[string]bool)
		for _, field := range allowedBuilderFields(music, features) {
			musicFields[field.key] = true
		}
		if musicFields["media_id"] != features.CustomMusic {
			t.Errorf("%s custom music input visibility = %v, entitlement = %v", packageID, musicFields["media_id"], features.CustomMusic)
		}
		for id, expected := range map[string]int{
			"gallery": int(order.MaxPhotos),
			"video":   int(order.MaxVideos),
			"events":  int(effectiveEventLimit(order, features)),
		} {
			spec, _ := findInvitationModule(id)
			if got := builderItemLimit(spec, order, features); got != expected {
				t.Errorf("%s %s limit = %d, want database limit %d", packageID, id, got, expected)
			}
			moduleURL := server.URL + "/orders/" + orderNumber + "/builder/" + id
			csrf := getCSRF(t, client, moduleURL)
			values := url.Values{
				"csrf_token": {csrf}, "draft_version": {"1"},
				"item_count": {strconv.Itoa(expected + 1)},
			}
			for index := 0; index <= expected; index++ {
				switch id {
				case "gallery":
					values.Set(fmt.Sprintf("item_%d_caption", index), "Foto")
				case "video":
					values.Set(fmt.Sprintf("item_%d_title", index), "Video")
					values.Set(fmt.Sprintf("item_%d_url", index), "https://youtube.com/watch?v=test")
				case "events":
					values.Set(fmt.Sprintf("item_%d_name", index), "Acara")
				}
			}
			response := postForm(t, client, moduleURL, values)
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Errorf("%s %s over-limit save status = %d, want 400", packageID, id, response.StatusCode)
			}
		}
	}
}

func TestInvitationPreviewUsesSavedDraftPrivately(t *testing.T) {
	db, server := testServer(t)
	client := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, client, server, "S01", "silver")
	target := server.URL + "/orders/" + orderNumber + "/builder/basic"
	csrf := getCSRF(t, client, target)
	response := postForm(t, client, target, url.Values{
		"csrf_token": {csrf}, "draft_version": {"1"},
		"field_bride_name": {"Ayu"}, "field_groom_name": {"Bayu"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save draft status = %d", response.StatusCode)
	}
	response, err := client.Get(server.URL + "/orders/" + orderNumber + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	preview := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(preview, "Ayu &amp; Bayu") || !strings.Contains(preview, "Pratinjau privat") {
		t.Fatalf("private preview status=%d does not show saved names: %s", response.StatusCode, preview)
	}

	other := newTestClient(t)
	response, err = other.Get(server.URL + "/orders/" + orderNumber + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("preview without order access status = %d, want 404", response.StatusCode)
	}
	if _, err := db.GetInvitationOrder(context.Background(), orderNumber); err != nil {
		t.Fatal(err)
	}
}

func TestValidateInvitationMediaSignatures(t *testing.T) {
	pngData := validPNG(t)
	mimeType, extension, err := validateInvitationMedia("gallery", pngData)
	if err != nil || mimeType != "image/png" || extension != ".png" {
		t.Fatalf("valid PNG result = %q %q %v", mimeType, extension, err)
	}
	if _, _, err := validateInvitationMedia("gallery", []byte("not an image")); err == nil {
		t.Fatal("invalid image accepted")
	}
	if _, _, err := validateInvitationMedia("audio", []byte("not audio")); err == nil {
		t.Fatal("invalid audio accepted")
	}
	if _, _, err := validateInvitationMedia("audio", []byte("ID3")); err == nil {
		t.Fatal("truncated MP3 header accepted")
	}
	if _, _, err := validateInvitationMedia("audio", []byte("OggS")); err == nil {
		t.Fatal("truncated OGG header accepted")
	}
}

func TestBuilderModuleMetadata(t *testing.T) {
	for _, module := range invitationModuleSpecs() {
		if module.id == "" || module.title == "" || len(module.fields)+len(module.itemFields) == 0 {
			t.Fatalf("incomplete builder module metadata: %#v", module)
		}
	}
	if len(invitationModuleSpecs()) != 13 {
		t.Fatalf("builder module count = %d, want 13", len(invitationModuleSpecs()))
	}
	if _, ok := findInvitationModule("unknown"); ok {
		t.Fatal("unknown module should not be routable")
	}
	if !storage.ValidOrderNumber("SKM-ORD-20250101-ABCDEFGH") {
		t.Fatal("test order number should be valid")
	}
}

func TestAdminInvitationReviewAndPrivateMediaAccess(t *testing.T) {
	db, server := testServer(t)
	customer := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, customer, server, "S01", "silver")
	order, err := db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	mediaData := validPNG(t)
	storedName := "0123456789abcdef0123456789abcdefabcd.png"
	mediaID, err := db.CreateInvitationMedia(context.Background(), storage.InvitationMedia{
		OrderID: order.ID, MediaType: "gallery", StoredName: storedName,
		MimeType: "image/png", SizeBytes: int64(len(mediaData)),
	}, order.MaxPhotos, false)
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(db.UploadDir, "invitation-media", storedName)
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, mediaData, 0o600); err != nil {
		t.Fatal(err)
	}

	admin := newTestClient(t)
	loginTestAdmin(t, db, server, admin)
	reviewURL := server.URL + "/admin/orders/" + orderNumber + "/invitation"
	response, err := admin.Get(reviewURL)
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(page, "Review undangan") || !strings.Contains(page, "Media privat") {
		t.Fatalf("admin review status=%d page missing draft UI: %s", response.StatusCode, page)
	}
	csrf := csrfInput.FindStringSubmatch(page)
	if len(csrf) != 2 {
		t.Fatal("admin invitation review page has no CSRF token")
	}
	response = postForm(t, admin, reviewURL+"/review", url.Values{
		"csrf_token": {csrf[1]}, "review_status": {"revision_requested"},
		"review_note": {"Mohon perbaiki detail acara."},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update review status = %d, want redirected page 200", response.StatusCode)
	}
	reloaded, err := db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.InvitationReviewStatus != "revision_requested" || reloaded.InvitationReviewNote != "Mohon perbaiki detail acara." {
		t.Fatalf("review status/note not persisted: %#v", reloaded)
	}

	mediaURL := server.URL + "/admin/orders/" + orderNumber + "/invitation/media/" + strconv.FormatInt(mediaID, 10)
	response, err = admin.Get(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(body, mediaData) {
		t.Fatalf("admin private media response status=%d", response.StatusCode)
	}
	unauthenticated := newTestClient(t)
	response, err = unauthenticated.Get(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/admin/login" {
		t.Fatalf("private admin media unauthenticated access ended at %s status %d", response.Request.URL, response.StatusCode)
	}
}

func TestInvitationMediaUploadSelectionAndDeletion(t *testing.T) {
	db, server := testServer(t)
	client := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, client, server, "S01", "silver")
	moduleURL := server.URL + "/orders/" + orderNumber + "/builder/gallery"
	csrf := getCSRF(t, client, moduleURL)

	uploadURL := server.URL + "/orders/" + orderNumber + "/builder/media"
	invalid := postInvitationMedia(t, client, uploadURL, csrf, "gallery", "bad.png", []byte("not an image"))
	invalidBody := readBody(t, invalid)
	invalid.Body.Close()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid media status=%d body=%s", invalid.StatusCode, invalidBody)
	}
	mediaDirectory := filepath.Join(db.UploadDir, "invitation-media")
	temporaryFiles, err := os.ReadDir(mediaDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(temporaryFiles) != 0 {
		t.Fatalf("invalid upload left temporary files: %#v", temporaryFiles)
	}
	oversized := postInvitationMedia(t, client, uploadURL, csrf, "gallery", "large.png", make([]byte, maxInvitationMediaSize+1))
	oversized.Body.Close()
	if oversized.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized media status=%d, want 413", oversized.StatusCode)
	}

	data := validPNG(t)
	response := postInvitationMedia(t, client, uploadURL, csrf, "gallery", "photo.png", data)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid media upload status=%d", response.StatusCode)
	}
	order, err := db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	media, err := db.ListInvitationMedia(context.Background(), order.ID)
	if err != nil || len(media) != 1 {
		t.Fatalf("stored media count=%d err=%v", len(media), err)
	}
	filePath := filepath.Join(db.UploadDir, "invitation-media", media[0].StoredName)
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("uploaded media mode = %o, want 600", info.Mode().Perm())
	}

	csrf = getCSRF(t, client, moduleURL)
	response = postForm(t, client, moduleURL, url.Values{
		"csrf_token": {csrf}, "draft_version": {"1"}, "item_count": {"1"},
		"item_0_caption": {"Foto berdua"}, "item_0_media_id": {strconv.FormatInt(media[0].ID, 10)},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save gallery selection status=%d", response.StatusCode)
	}
	order, err = db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	var draft InvitationDraft
	if err := json.Unmarshal([]byte(order.DraftData), &draft); err != nil {
		t.Fatal(err)
	}
	if len(draft.Modules["gallery"].Items) != 1 || draft.Modules["gallery"].Items[0]["caption"] != "Foto berdua" {
		t.Fatalf("gallery selection not saved: %#v", draft.Modules["gallery"])
	}

	imageURL := server.URL + "/orders/" + orderNumber + "/builder/media/" + strconv.FormatInt(media[0].ID, 10)
	response, err = client.Get(imageURL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(got, data) {
		t.Fatalf("private image response status=%d content mismatch", response.StatusCode)
	}

	csrf = getCSRF(t, client, moduleURL)
	mediaURL := server.URL + "/orders/" + orderNumber + "/builder/media/" + strconv.FormatInt(media[0].ID, 10)
	response = postForm(t, client, mediaURL, url.Values{
		"csrf_token": {csrf}, "draft_version": {"2"},
		"media_caption": {"Foto pilihan"}, "media_sort_order": {"0"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save media caption and order status=%d", response.StatusCode)
	}
	updatedMedia, err := db.GetInvitationMedia(context.Background(), order.ID, media[0].ID)
	if err != nil || updatedMedia.Caption != "Foto pilihan" || updatedMedia.SortOrder != 0 {
		t.Fatalf("media caption/order = %q/%d err=%v", updatedMedia.Caption, updatedMedia.SortOrder, err)
	}

	csrf = getCSRF(t, client, moduleURL)
	response = postForm(t, client, imageURL+"/delete", url.Values{
		"csrf_token": {csrf}, "draft_version": {"3"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("delete invitation media status=%d", response.StatusCode)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("media file remains after deletion: %v", err)
	}
	order, err = db.GetInvitationOrder(context.Background(), orderNumber)
	if err != nil {
		t.Fatal(err)
	}
	if order.DraftVersion != 4 {
		t.Fatalf("media deletion draft version=%d, want 4", order.DraftVersion)
	}
	draft, err = decodeInvitationDraft(order.DraftData)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Modules["gallery"].Items) != 0 {
		t.Fatalf("deleted photo remains linked in draft: %#v", draft.Modules["gallery"])
	}
}

func TestAdminMusicLibraryAndOrderMusicSelection(t *testing.T) {
	db, server := testServer(t)
	customer := newTestClient(t)
	orderNumber, _, _ := createGuestOrderForTest(t, customer, server, "S01", "silver")
	storedName := "abcdef0123456789abcdef0123456789abcd.mp3"
	trackID, err := db.InsertMusicTrack(context.Background(), "Lagu Uji", storedName, "audio/mpeg", 11)
	if err != nil {
		t.Fatal(err)
	}
	musicDir := filepath.Join(db.UploadDir, "music-library")
	if err := os.MkdirAll(musicDir, 0o700); err != nil {
		t.Fatal(err)
	}
	audio := []byte("test audio!")
	if err := os.WriteFile(filepath.Join(musicDir, storedName), audio, 0o600); err != nil {
		t.Fatal(err)
	}

	moduleURL := server.URL + "/orders/" + orderNumber + "/builder/music"
	csrf := getCSRF(t, customer, moduleURL)
	response, err := customer.Get(moduleURL)
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(page, "Lagu Uji") {
		t.Fatalf("customer music options status=%d page=%s", response.StatusCode, page)
	}
	response = postForm(t, customer, moduleURL, url.Values{
		"csrf_token": {csrf}, "draft_version": {"1"},
		"field_enabled": {"true"}, "field_track_id": {strconv.FormatInt(trackID, 10)},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save selected music status=%d", response.StatusCode)
	}
	response, err = customer.Get(server.URL + "/orders/" + orderNumber + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	preview := readBody(t, response)
	response.Body.Close()
	if !strings.Contains(preview, "<audio") || !strings.Contains(preview, "/builder/music/") {
		t.Fatalf("private preview is missing the selected music control: %s", preview)
	}
	audioURL := server.URL + "/orders/" + orderNumber + "/builder/music/" + strconv.FormatInt(trackID, 10)
	response, err = customer.Get(audioURL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(got, audio) {
		t.Fatalf("private music response status=%d", response.StatusCode)
	}

	admin := newTestClient(t)
	loginTestAdmin(t, db, server, admin)
	adminURL := server.URL + "/admin/music"
	response, err = admin.Get(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	adminPage := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(adminPage, "Lagu Uji") {
		t.Fatalf("admin music page status=%d", response.StatusCode)
	}
	adminCSRF := csrfInput.FindStringSubmatch(adminPage)
	if len(adminCSRF) != 2 {
		t.Fatal("admin music page has no CSRF token")
	}
	invalidUpload := postAdminMusicUpload(t, admin, adminURL, adminCSRF[1], []byte("not audio"))
	invalidUpload.Body.Close()
	if invalidUpload.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid admin music upload status=%d", invalidUpload.StatusCode)
	}
	response = postForm(t, admin, adminURL+"/"+strconv.FormatInt(trackID, 10)+"/active", url.Values{
		"csrf_token": {adminCSRF[1]}, "active": {"0"},
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("deactivate music track status=%d", response.StatusCode)
	}
	track, err := db.GetMusicTrack(context.Background(), trackID)
	if err != nil || track.IsActive != 0 {
		t.Fatalf("music track active state=%d err=%v", track.IsActive, err)
	}
}

func TestInvitationSettingsApplyPackageOptionsAndRejectDuplicateSlug(t *testing.T) {
	db, server := testServer(t)
	first := newTestClient(t)
	firstOrder, _, _ := createGuestOrderForTest(t, first, server, "G01", "gold")
	firstURL := server.URL + "/orders/" + firstOrder + "/builder/settings"
	csrf := getCSRF(t, first, firstURL)
	settings := url.Values{
		"csrf_token": {csrf}, "draft_version": {"1"},
		"field_slug":       {"ayu-bayu-2026"},
		"field_background": {"#111111"}, "field_surface": {"#222222"},
		"field_accent": {"#d4af69"}, "field_text": {"#fffaf2"},
		"field_heading_font": {"Cormorant Garamond"}, "field_body_font": {"Plus Jakarta Sans"},
		"field_layout": {"classic"}, "field_cover_style": {"framed"},
		"field_section_cover": {"true"}, "field_section_couple": {"true"},
		"field_section_story": {"false"}, "field_section_events": {"true"},
		"field_section_gallery": {"false"}, "field_section_rsvp": {"true"},
		"field_section_gift": {"false"},
	}
	response := postForm(t, first, firstURL, settings)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save invitation settings status=%d", response.StatusCode)
	}
	order, err := db.GetInvitationOrder(context.Background(), firstOrder)
	if err != nil {
		t.Fatal(err)
	}
	if order.InvitationSlug != "ayu-bayu-2026" || order.DraftVersion != 2 {
		t.Fatalf("saved slug/version = %q/%d", order.InvitationSlug, order.DraftVersion)
	}
	response, err = first.Get(server.URL + "/orders/" + firstOrder + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	preview := readBody(t, response)
	response.Body.Close()
	if !strings.Contains(preview, "--preview-bg:#111111") || strings.Contains(preview, "preview-section-title text-xs font-semibold uppercase tracking-[0.2em]\">Galeri") {
		t.Fatalf("custom settings were not applied to preview: %s", preview)
	}

	second := newTestClient(t)
	secondOrder, _, _ := createGuestOrderForTest(t, second, server, "G01", "gold")
	secondURL := server.URL + "/orders/" + secondOrder + "/builder/settings"
	secondCSRF := getCSRF(t, second, secondURL)
	settings.Set("csrf_token", secondCSRF)
	settings.Set("draft_version", "1")
	response = postForm(t, second, secondURL, settings)
	body := readBody(t, response)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict || !strings.Contains(body, "Slug tersebut sudah digunakan") {
		t.Fatalf("duplicate slug save status=%d body=%s", response.StatusCode, body)
	}
}

func postAdminMusicUpload(t *testing.T, client *http.Client, target, csrf string, data []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", csrf); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("title", "Lagu tidak valid"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("audio", "invalid.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
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

func postInvitationMedia(t *testing.T, client *http.Client, target, csrf, mediaType, filename string, data []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", csrf); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("media_type", mediaType); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("media", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
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
