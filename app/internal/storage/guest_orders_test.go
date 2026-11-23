package storage

import (
	"bytes"
	"context"
	"errors"
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
		if len(trackingCode) != 18 {
			t.Fatalf("generated tracking code has %d characters, want 18", len(trackingCode))
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
	if !ValidTrackingCode("SKM-AAAA-BBBB") {
		t.Fatal("legacy tracking codes must remain valid")
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

func TestConcurrentPaymentReviewsCommitOnlyOnce(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "payment-review-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, accessHash, err := NewOrderAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	order, err := db.CreateGuestOrder(ctx, "silver", "S01",
		"Customer", "customer@example.com", "Ayu & Bayu", accessHash)
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := db.CreateUser(ctx, "Admin", "race-admin@example.com", "", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SubmitGuestPayment(ctx, order.OrderNumber, "bank_transfer",
		"proof.png", "0123456789abcdef01234567.png", "image/png", 128); err != nil {
		t.Fatal(err)
	}
	var paymentID int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT id FROM payments
		WHERE order_id = (SELECT id FROM orders WHERE order_number = ?)`, order.OrderNumber).Scan(&paymentID); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- db.ReviewGuestPayment(ctx, paymentID, adminID, true, "")
		}()
	}
	close(start)
	successes, rejected := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrInvalidOrderTransition):
			rejected++
		default:
			t.Fatalf("concurrent payment review returned unexpected error: %v", err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("review outcomes successes=%d rejected=%d, want 1 each", successes, rejected)
	}

	var paymentStatus, orderPaymentStatus, workStatus string
	if err := db.SQL.QueryRowContext(ctx, `SELECT pay.status, o.payment_status, o.work_status
		FROM payments AS pay JOIN orders AS o ON o.id = pay.order_id WHERE pay.id = ?`, paymentID).
		Scan(&paymentStatus, &orderPaymentStatus, &workStatus); err != nil {
		t.Fatal(err)
	}
	if paymentStatus != "verified" || orderPaymentStatus != "paid" || workStatus != "processing" {
		t.Fatalf("concurrent review left inconsistent states payment=%s order=%s work=%s",
			paymentStatus, orderPaymentStatus, workStatus)
	}
	var reviewCount int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM payment_reviews
		WHERE payment_id = ? AND decision = 'approved'`, paymentID).Scan(&reviewCount); err != nil {
		t.Fatal(err)
	}
	if reviewCount != 1 {
		t.Fatalf("approved review audit rows = %d, want 1", reviewCount)
	}
}
