package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInvitationDraftVersionAndMediaEntitlements(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "invitations.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()

	_, accessHash, err := NewOrderAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateGuestOrder(ctx, "silver", "S01", "Customer", "customer@example.com", "Ayu & Bayu", accessHash)
	if err != nil {
		t.Fatal(err)
	}
	order, err := db.GetInvitationOrder(ctx, created.OrderNumber)
	if err != nil {
		t.Fatal(err)
	}
	if order.DraftVersion != 1 || order.MaxPhotos != 8 || order.MaxEvents != 2 {
		t.Fatalf("unexpected invitation order limits/version: %#v", order)
	}
	features, err := db.GetInvitationFeatures(ctx, order.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if !features.CoupleStory || features.CustomMusic {
		t.Fatalf("silver package unexpectedly includes premium entitlements: %#v", features)
	}
	if err := db.SaveInvitationDraft(ctx, order.ID, 1, `{"schema_version":1,"modules":{"basic":{"fields":{"bride_name":"Ayu"}}}}`, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveInvitationDraft(ctx, order.ID, 1, `{"schema_version":1,"modules":{}}`, ""); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("stale draft save error = %v, want ErrDraftConflict", err)
	}

	for index := int64(0); index < order.MaxPhotos; index++ {
		name := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + string(rune('a'+index)) + ".jpg"
		if _, err := db.CreateInvitationMedia(ctx, InvitationMedia{
			OrderID: order.ID, MediaType: "gallery", StoredName: name,
			MimeType: "image/jpeg", SizeBytes: 64,
		}, order.MaxPhotos, false); err != nil {
			t.Fatalf("create media %d: %v", index, err)
		}
	}
	if _, err := db.CreateInvitationMedia(ctx, InvitationMedia{
		OrderID: order.ID, MediaType: "gallery", StoredName: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.jpg",
		MimeType: "image/jpeg", SizeBytes: 64,
	}, order.MaxPhotos, false); !errors.Is(err, ErrInvitationMediaLimit) {
		t.Fatalf("over-limit image error = %v, want ErrInvitationMediaLimit", err)
	}
	if _, err := db.CreateInvitationMedia(ctx, InvitationMedia{
		OrderID: order.ID, MediaType: "audio", StoredName: "cccccccccccccccccccccccccccccccccccc.mp3",
		MimeType: "audio/mpeg", SizeBytes: 64,
	}, order.MaxPhotos, false); !errors.Is(err, ErrInvitationMediaLimit) {
		t.Fatalf("unentitled audio error = %v, want ErrInvitationMediaLimit", err)
	}

	otherHash := make([]byte, 32)
	other, err := db.CreateGuestOrder(ctx, "gold", "G01", "Other Customer", "other@example.com", "Other Couple", otherHash)
	if err != nil {
		t.Fatal(err)
	}
	otherOrder, err := db.GetInvitationOrder(ctx, other.OrderNumber)
	if err != nil {
		t.Fatal(err)
	}
	available, err := db.InvitationSlugAvailable(ctx, "our-wedding", order.ID)
	if err != nil || !available {
		t.Fatalf("new slug availability = %v, err %v", available, err)
	}
	if err := db.SaveInvitationDraft(ctx, order.ID, 2, `{"schema_version":1,"modules":{}}`, "our-wedding"); err != nil {
		t.Fatal(err)
	}
	available, err = db.InvitationSlugAvailable(ctx, "our-wedding", otherOrder.ID)
	if err != nil || available {
		t.Fatalf("duplicate slug availability = %v, err %v", available, err)
	}
}

func TestCleanupOrphanedInvitationMedia(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := OpenWithUploads(ctx, filepath.Join(root, "cleanup.db"), filepath.Join(root, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, accessHash, err := NewOrderAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateGuestOrder(ctx, "silver", "S01", "Customer", "customer@example.com", "Ayu & Bayu", accessHash)
	if err != nil {
		t.Fatal(err)
	}
	order, err := db.GetInvitationOrder(ctx, created.OrderNumber)
	if err != nil {
		t.Fatal(err)
	}
	linked := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	if _, err := db.CreateInvitationMedia(ctx, InvitationMedia{
		OrderID: order.ID, MediaType: "gallery", StoredName: linked, MimeType: "image/png", SizeBytes: 64,
	}, order.MaxPhotos, false); err != nil {
		t.Fatal(err)
	}

	directory := filepath.Join(db.UploadDir, "invitation-media")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.jpg"
	fresh := "cccccccccccccccccccccccccccccccccccc.webp"
	unrecognized := "keep-me.tmp"
	for _, name := range []string{linked, orphan, fresh, unrecognized} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(directory, orphan), old, old); err != nil {
		t.Fatal(err)
	}
	if err := db.CleanupOrphanedInvitationMedia(ctx, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{linked, fresh, unrecognized} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("expected media file %q to remain: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, orphan)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale orphan still exists: %v", err)
	}
}
