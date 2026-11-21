package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sukamoto/app/db/generated"
)

var (
	ErrInvalidOrderTransition = errors.New("invalid order status transition")
	ErrOrderUnavailable       = errors.New("order is unavailable")
)

var (
	trackingCodePattern = regexp.MustCompile(`^SKM-[A-Z2-7]{4}-[A-Z2-7]{4}$`)
	orderNumberPattern  = regexp.MustCompile(`^SKM-ORD-[0-9]{8}-[A-Z2-7]{8}$`)
	orderCodeEncoding   = base32.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567").WithPadding(base32.NoPadding)
)

type GuestOrder struct {
	ID              int64
	OrderNumber     string
	TrackingCode    string
	CustomerName    string
	CustomerContact string
	PublicName      string
	PriceCents      int64
	PaymentStatus   string
	WorkStatus      string
	PublicNote      string
	InvitationSlug  string
	DraftData       string
	DraftAccessHash []byte
	CreatedAt       string
	UpdatedAt       string
	PackageID       string
	PackageName     string
	TemplateID      string
	TemplateName    string
	RejectReason    string
	ProofStoredName string
}

type CreatedGuestOrder struct {
	OrderNumber  string
	TrackingCode string
}

type TrackedOrder struct {
	OrderNumber    string
	PublicName     string
	PriceCents     int64
	PaymentStatus  string
	WorkStatus     string
	PublicNote     string
	InvitationSlug string
	CreatedAt      string
	PackageName    string
	TemplateName   string
}

type AdminOrder struct {
	ID                  int64
	OrderNumber         string
	TrackingCode        string
	CustomerName        string
	CustomerContact     string
	PublicName          string
	PriceCents          int64
	PaymentStatus       string
	WorkStatus          string
	PublicNote          string
	InternalNote        string
	InvitationSlug      string
	CreatedAt           string
	UpdatedAt           string
	PackageName         string
	TemplateName        string
	PaymentID           int64
	PaymentRecordStatus string
	ProofStoredName     string
	ProofMimeType       string
	ProofFilename       string
	RejectReason        string
	PaymentMethodName   string
}

type PaymentMethod struct {
	ID            string
	Name          string
	BankName      string
	AccountNumber string
	AccountHolder string
	Instructions  string
}

func GenerateTrackingCode() (string, error) {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate tracking code: %w", err)
	}
	code := orderCodeEncoding.EncodeToString(raw)
	return "SKM-" + code[:4] + "-" + code[4:], nil
}

func GenerateOrderNumber() (string, error) {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate order number: %w", err)
	}
	randomPart := orderCodeEncoding.EncodeToString(raw)[:8]
	return "SKM-ORD-" + time.Now().UTC().Format("20060102") + "-" + randomPart, nil
}

func NormalizeTrackingCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func ValidTrackingCode(code string) bool {
	return trackingCodePattern.MatchString(NormalizeTrackingCode(code))
}

func ValidOrderNumber(number string) bool {
	return orderNumberPattern.MatchString(strings.TrimSpace(number))
}

func NewOrderAccessToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate order access token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, HashOrderAccessToken(token), nil
}

func HashOrderAccessToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func (db *DB) CreateGuestOrder(ctx context.Context, packageID, templateID, customerName, customerContact, publicName string, accessHash []byte) (*CreatedGuestOrder, error) {
	pkg, err := db.GetPackage(ctx, packageID)
	if errors.Is(err, ErrNotFound) || (err == nil && (!pkg.Available || pkg.ComingSoon)) {
		return nil, ErrOrderUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("validate package for guest order: %w", err)
	}
	template, err := db.GetTemplate(ctx, templateID)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrOrderUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("validate template for guest order: %w", err)
	}
	supported := false
	for _, option := range template.Packages {
		if option.ID == packageID && option.Available && !option.ComingSoon {
			supported = true
			break
		}
	}
	if !supported {
		return nil, ErrOrderUnavailable
	}

	for range 5 {
		orderNumber, err := GenerateOrderNumber()
		if err != nil {
			return nil, err
		}
		trackingCode, err := GenerateTrackingCode()
		if err != nil {
			return nil, err
		}
		id, err := db.Queries.CreateGuestOrder(ctx, generated.CreateGuestOrderParams{
			OrderNumber:     sql.NullString{String: orderNumber, Valid: true},
			TrackingCode:    sql.NullString{String: trackingCode, Valid: true},
			CustomerName:    customerName,
			CustomerContact: customerContact,
			PublicName:      publicName,
			DraftAccessHash: accessHash,
			PackageID:       packageID,
			TemplateID:      templateID,
		})
		if err == nil {
			if id < 1 {
				return nil, fmt.Errorf("create guest order: invalid database identifier")
			}
			return &CreatedGuestOrder{OrderNumber: orderNumber, TrackingCode: trackingCode}, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return nil, fmt.Errorf("create guest order: %w", err)
	}
	return nil, fmt.Errorf("create guest order: could not generate unique order codes after 5 attempts")
}

func (db *DB) GetGuestOrderByNumber(ctx context.Context, orderNumber string) (*GuestOrder, error) {
	row, err := db.Queries.GetGuestOrderByNumber(ctx, sql.NullString{String: orderNumber, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get guest order: %w", err)
	}
	return &GuestOrder{
		ID: row.ID, OrderNumber: row.OrderNumber.String, TrackingCode: row.TrackingCode.String,
		CustomerName: row.CustomerName, CustomerContact: row.CustomerContact, PublicName: row.PublicName,
		PriceCents: row.PriceCents, PaymentStatus: row.PaymentStatus, WorkStatus: row.WorkStatus,
		PublicNote: row.PublicNote, InvitationSlug: row.InvitationSlug.String, DraftData: row.DraftData,
		DraftAccessHash: row.DraftAccessHash, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		PackageID: row.PackageID, PackageName: row.PackageName,
		TemplateID: row.TemplateID, TemplateName: row.TemplateName,
		RejectReason: row.RejectReason, ProofStoredName: row.ProofStoredName,
	}, nil
}

func (db *DB) GetTrackedOrder(ctx context.Context, trackingCode string) (*TrackedOrder, error) {
	row, err := db.Queries.GetTrackedOrderByCode(ctx, sql.NullString{String: NormalizeTrackingCode(trackingCode), Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tracked order: %w", err)
	}
	return &TrackedOrder{
		OrderNumber: row.OrderNumber.String, PublicName: row.PublicName, PriceCents: row.PriceCents,
		PaymentStatus: row.PaymentStatus, WorkStatus: row.WorkStatus, PublicNote: row.PublicNote,
		InvitationSlug: row.InvitationSlug.String, CreatedAt: row.CreatedAt,
		PackageName: row.PackageName, TemplateName: row.TemplateName,
	}, nil
}

func (db *DB) ListOrdersForAdmin(ctx context.Context, search, paymentStatus, workStatus string, pageSize, pageOffset int64) ([]AdminOrder, error) {
	rows, err := db.Queries.ListOrdersForAdmin(ctx, generated.ListOrdersForAdminParams{
		Search: search, PaymentStatus: paymentStatus, WorkStatus: workStatus,
		PageSize: pageSize, PageOffset: pageOffset,
	})
	if err != nil {
		return nil, fmt.Errorf("list admin orders: %w", err)
	}
	out := make([]AdminOrder, 0, len(rows))
	for _, row := range rows {
		out = append(out, AdminOrder{
			ID: row.ID, OrderNumber: row.OrderNumber.String, TrackingCode: row.TrackingCode.String,
			CustomerName: row.CustomerName, CustomerContact: row.CustomerContact, PublicName: row.PublicName,
			PriceCents: row.PriceCents, PaymentStatus: row.PaymentStatus, WorkStatus: row.WorkStatus,
			PublicNote: row.PublicNote, InternalNote: row.InternalNote, InvitationSlug: row.InvitationSlug.String,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, PackageName: row.PackageName,
			TemplateName: row.TemplateName, PaymentID: row.PaymentID.Int64,
			PaymentRecordStatus: row.PaymentRecordStatus.String, ProofStoredName: row.ProofStoredName.String,
			RejectReason: row.RejectReason.String, PaymentMethodName: row.PaymentMethodName.String,
		})
	}
	return out, nil
}

func (db *DB) GetOrderNumberByPaymentID(ctx context.Context, paymentID int64) (string, error) {
	orderNumber, err := db.Queries.GetOrderNumberByPaymentID(ctx, paymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get payment order number: %w", err)
	}
	if !orderNumber.Valid {
		return "", ErrNotFound
	}
	return orderNumber.String, nil
}

func (db *DB) GetAdminOrder(ctx context.Context, orderNumber string) (*AdminOrder, error) {
	row, err := db.Queries.GetAdminOrderByNumber(ctx, sql.NullString{String: orderNumber, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get admin order: %w", err)
	}
	return &AdminOrder{
		ID: row.ID, OrderNumber: row.OrderNumber.String, TrackingCode: row.TrackingCode.String,
		CustomerName: row.CustomerName, CustomerContact: row.CustomerContact, PublicName: row.PublicName,
		PriceCents: row.PriceCents, PaymentStatus: row.PaymentStatus, WorkStatus: row.WorkStatus,
		PublicNote: row.PublicNote, InternalNote: row.InternalNote, InvitationSlug: row.InvitationSlug.String,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, PackageName: row.PackageName,
		TemplateName: row.TemplateName, PaymentID: row.PaymentID.Int64,
		PaymentRecordStatus: row.PaymentRecordStatus.String, ProofStoredName: row.ProofStoredName.String,
		ProofMimeType: row.ProofMimeType.String, ProofFilename: row.ProofFilename.String,
		RejectReason: row.RejectReason.String, PaymentMethodName: row.PaymentMethodName.String,
	}, nil
}

func (db *DB) ListPaymentMethods(ctx context.Context) ([]PaymentMethod, error) {
	methods, err := db.Queries.ListActivePaymentMethods(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active payment methods: %w", err)
	}
	out := make([]PaymentMethod, 0, len(methods))
	for _, method := range methods {
		out = append(out, PaymentMethod{
			ID: method.ID, Name: method.Name, BankName: method.BankName,
			AccountNumber: method.AccountNumber, AccountHolder: method.AccountHolder,
			Instructions: method.Instructions,
		})
	}
	return out, nil
}

func (db *DB) SubmitGuestPayment(ctx context.Context, orderNumber, paymentMethodID, proofFilename, proofStoredName, mimeType string, size int64) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payment submission: %w", err)
	}
	defer tx.Rollback()

	queries := db.Queries.WithTx(tx)
	order, err := queries.GetGuestOrderByNumber(ctx, sql.NullString{String: orderNumber, Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load order for payment: %w", err)
	}
	rows, err := queries.SetOrderPaymentSubmitted(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("set order payment submitted: %w", err)
	}
	if rows != 1 {
		return ErrInvalidOrderTransition
	}
	rows, err = queries.SaveGuestPaymentProof(ctx, generated.SaveGuestPaymentProofParams{
		ProofFilename: proofFilename, ProofStoredName: proofStoredName, ProofMimeType: mimeType,
		ProofSizeBytes: size, PaymentMethodID: paymentMethodID, OrderID: order.ID,
	})
	if err != nil {
		return fmt.Errorf("save payment proof metadata: %w", err)
	}
	if rows != 1 {
		return ErrInvalidOrderTransition
	}
	if err := queries.RecordOrderStatusHistory(ctx, generated.RecordOrderStatusHistoryParams{
		OrderID: order.ID, ActorType: "system",
		OldPaymentStatus: order.PaymentStatus, NewPaymentStatus: "payment_submitted",
		OldWorkStatus: order.WorkStatus, NewWorkStatus: order.WorkStatus,
		Note: "Bukti pembayaran dikirim.",
	}); err != nil {
		return fmt.Errorf("record payment submission history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payment submission: %w", err)
	}
	return nil
}

func (db *DB) ReviewGuestPayment(ctx context.Context, paymentID, adminID int64, approve bool, reason string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payment review: %w", err)
	}
	defer tx.Rollback()

	queries := db.Queries.WithTx(tx)
	payment, err := queries.GetPaymentByID(ctx, paymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load payment for review: %w", err)
	}
	if payment.Status != "payment_submitted" {
		return ErrInvalidOrderTransition
	}
	status, err := queries.GetOrderStatusesByID(ctx, payment.OrderID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load order statuses for review: %w", err)
	}
	if status.PaymentStatus != "payment_submitted" || status.WorkStatus != "waiting_payment" {
		return ErrInvalidOrderTransition
	}

	decision := "rejected"
	nextPaymentStatus, nextWorkStatus := "payment_rejected", "waiting_payment"
	if approve {
		decision = "approved"
		nextPaymentStatus, nextWorkStatus = "paid", "processing"
		if rows, err := queries.VerifyPayment(ctx, generated.VerifyPaymentParams{ReviewedBy: sql.NullInt64{Int64: adminID, Valid: true}, ID: paymentID}); err != nil {
			return fmt.Errorf("approve payment: %w", err)
		} else if rows != 1 {
			return ErrInvalidOrderTransition
		}
		rows, err := queries.SetOrderPaymentPaid(ctx, payment.OrderID)
		if err != nil {
			return fmt.Errorf("set order paid: %w", err)
		}
		if rows != 1 {
			return ErrInvalidOrderTransition
		}
	} else {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("payment rejection reason is required")
		}
		rows, err := queries.RejectPayment(ctx, generated.RejectPaymentParams{
			RejectReason: reason, ReviewedBy: sql.NullInt64{Int64: adminID, Valid: true}, ID: paymentID,
		})
		if err != nil {
			return fmt.Errorf("reject payment: %w", err)
		}
		if rows != 1 {
			return ErrInvalidOrderTransition
		}
		rows, err = queries.SetOrderPaymentRejected(ctx, payment.OrderID)
		if err != nil {
			return fmt.Errorf("set order payment rejected: %w", err)
		}
		if rows != 1 {
			return ErrInvalidOrderTransition
		}
	}

	if err := queries.RecordPaymentReview(ctx, generated.RecordPaymentReviewParams{
		PaymentID: paymentID, OrderID: payment.OrderID, AdminID: adminID, Decision: decision, Reason: reason,
	}); err != nil {
		return fmt.Errorf("record payment review: %w", err)
	}
	if err := queries.RecordOrderStatusHistory(ctx, generated.RecordOrderStatusHistoryParams{
		OrderID: payment.OrderID, ActorUserID: sql.NullInt64{Int64: adminID, Valid: true}, ActorType: "admin",
		OldPaymentStatus: status.PaymentStatus, NewPaymentStatus: nextPaymentStatus,
		OldWorkStatus: status.WorkStatus, NewWorkStatus: nextWorkStatus, Note: reason,
	}); err != nil {
		return fmt.Errorf("record reviewed order status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payment review: %w", err)
	}
	return nil
}

func (db *DB) UpdateAdminOrderNotes(ctx context.Context, orderNumber, publicNote, internalNote string) error {
	order, err := db.GetAdminOrder(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin order notes update: %w", err)
	}
	defer tx.Rollback()
	queries := db.Queries.WithTx(tx)
	if rows, err := queries.UpdateOrderPublicNote(ctx, generated.UpdateOrderPublicNoteParams{
		PublicNote: publicNote, ID: order.ID,
	}); err != nil {
		return fmt.Errorf("update public order note: %w", err)
	} else if rows != 1 {
		return ErrNotFound
	}
	if rows, err := queries.UpdateOrderInternalNote(ctx, generated.UpdateOrderInternalNoteParams{
		InternalNote: internalNote, ID: order.ID,
	}); err != nil {
		return fmt.Errorf("update internal order note: %w", err)
	} else if rows != 1 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit order notes update: %w", err)
	}
	return nil
}

func (db *DB) ChangeGuestOrderWorkStatus(ctx context.Context, orderNumber, newStatus string, adminID int64) error {
	if newStatus != "cancelled" && newStatus != "expired" {
		return ErrInvalidOrderTransition
	}
	order, err := db.GetAdminOrder(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin order status update: %w", err)
	}
	defer tx.Rollback()

	queries := db.Queries.WithTx(tx)
	rows, err := queries.SetOrderWorkStatus(ctx, generated.SetOrderWorkStatusParams{
		NewStatus: newStatus, ID: order.ID, OldStatus: order.WorkStatus,
	})
	if err != nil {
		return fmt.Errorf("change order work status: %w", err)
	}
	if rows != 1 {
		return ErrInvalidOrderTransition
	}
	if err := queries.RecordOrderStatusHistory(ctx, generated.RecordOrderStatusHistoryParams{
		OrderID: order.ID, ActorUserID: sql.NullInt64{Int64: adminID, Valid: true},
		ActorType: "admin", OldPaymentStatus: order.PaymentStatus,
		NewPaymentStatus: order.PaymentStatus, OldWorkStatus: order.WorkStatus,
		NewWorkStatus: newStatus,
	}); err != nil {
		return fmt.Errorf("record order work status history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit order work status update: %w", err)
	}
	return nil
}
