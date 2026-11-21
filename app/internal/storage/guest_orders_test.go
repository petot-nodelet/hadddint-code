package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackingCodeAndOrderNumberGeneration(t *testing.T) {
	trackingCodes := make(map[string]struct{}, 1000)
	orderNumbers := make(map[string]struct{}, 1000)
	for range 1000 {
		trackingCode, err := GenerateTrackingCode()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidTrackingCode(trackingCode) {
			t.Fatalf("generated tracking code has invalid format: %q", trackingCode)
		}
		if NormalizeTrackingCode(strings.ToLower(trackingCode)) != trackingCode {
			t.Fatalf("tracking code normalization failed for %q", trackingCode)
		}
		if _, exists := trackingCodes[trackingCode]; exists {
			t.Fatalf("tracking code collision: %q", trackingCode)
		}
		trackingCodes[trackingCode] = struct{}{}

		orderNumber, err := GenerateOrderNumber()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidOrderNumber(orderNumber) {
			t.Fatalf("generated order number has invalid format: %q", orderNumber)
		}
		if _, exists := orderNumbers[orderNumber]; exists {
			t.Fatalf("order number collision: %q", orderNumber)
		}
		orderNumbers[orderNumber] = struct{}{}
	}
}

func TestCreateGuestOrdersStoresDatabasePriceAndEnforcesEntitlement(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "guest-orders.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	trackingCodes := make(map[string]struct{})
	for range 20 {
		_, accessHash, err := NewOrderAccessToken()
		if err != nil {
			t.Fatal(err)
		}
		created, err := db.CreateGuestOrder(ctx, "silver", "S01",
			"Private Customer", "private@example.com", "Ayu & Bayu", accessHash)
		if err != nil {
			t.Fatal(err)
		}
		if !ValidOrderNumber(created.OrderNumber) || !ValidTrackingCode(created.TrackingCode) {
			t.Fatalf("invalid guest order codes: %#v", created)
		}
		if _, exists := trackingCodes[created.TrackingCode]; exists {
			t.Fatalf("duplicate guest order tracking code %q", created.TrackingCode)
		}
		trackingCodes[created.TrackingCode] = struct{}{}

		order, err := db.GetGuestOrderByNumber(ctx, created.OrderNumber)
		if err != nil {
			t.Fatal(err)
		}
		if order.CustomerName != "Private Customer" || order.CustomerContact != "private@example.com" || order.PublicName != "Ayu & Bayu" {
			t.Fatalf("guest order identity fields = %#v", order)
		}
		if order.PriceCents != 15000000 || order.PaymentStatus != "pending_payment" || order.WorkStatus != "waiting_payment" {
			t.Fatalf("guest order state = price:%d payment:%s work:%s", order.PriceCents, order.PaymentStatus, order.WorkStatus)
		}
		if len(order.DraftAccessHash) != 32 || !bytes.Equal(order.DraftAccessHash, accessHash) {
			t.Fatal("order access credential hash was not stored")
		}
		tracked, err := db.GetTrackedOrder(ctx, strings.ToLower(created.TrackingCode))
		if err != nil {
			t.Fatal(err)
		}
		if tracked.PublicName != "Ayu & Bayu" || tracked.PackageName != "Silver" || tracked.TemplateName != "Floral Elegant" {
			t.Fatalf("public tracking data = %#v", tracked)
		}
	}

	if _, err := db.CreateGuestOrder(ctx, "silver", "G01", "Customer", "contact@example.com", "Couple", make([]byte, 32)); err != ErrOrderUnavailable {
		t.Fatalf("unsupported template/package pair error = %v, want ErrOrderUnavailable", err)
	}
	if _, err := db.CreateGuestOrder(ctx, "platinum", "S01", "Customer", "contact@example.com", "Couple", make([]byte, 32)); err != ErrOrderUnavailable {
		t.Fatalf("unavailable package error = %v, want ErrOrderUnavailable", err)
	}
}
