package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
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
