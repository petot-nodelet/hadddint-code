package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	migrations "github.com/sukamoto/app/db"
)

func TestOpenMigratesAndSeedsSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "sukamoto.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var journalMode string
	if err := db.SQL.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	var foreignKeys int
	if err := db.SQL.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var busyTimeout int
	if err := db.SQL.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}

	packages, err := db.ListPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 3 {
		t.Fatalf("seeded packages = %d, want 3", len(packages))
	}
	templates, err := db.ListTemplates(ctx, "", "", "", "featured", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 5 {
		t.Fatalf("seeded templates = %d, want 5", len(templates))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
}

func TestOpenEnablesForeignKeys(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "foreign-keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.SQL.Exec(`INSERT INTO template_packages (template_id, package_id) VALUES ('missing', 'silver')`)
	if err == nil {
		t.Fatal("expected foreign-key constraint failure")
	}
}

func TestOpenRestrictsDatabaseAndUploadPermissions(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "private.db")
	uploadDir := filepath.Join(dir, "payment-proofs")
	db, err := OpenWithUploads(context.Background(), databasePath, uploadDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s permissions = %o, want 600", filepath.Base(path), got)
		}
	}
	info, err := os.Stat(uploadDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("upload directory permissions = %o, want 700", got)
	}
}

func TestMigrationsPreserveLegacyOrdersAndPayments(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "legacy.db")
	sqlDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}

	migrationMu.Lock()
	defer migrationMu.Unlock()
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.Migrations)
	if err := goose.UpToContext(ctx, sqlDB, "migrations", 4); err != nil {
		t.Fatal(err)
	}

	var userID int64
	if err := sqlDB.QueryRowContext(ctx, `INSERT INTO users (full_name, email, password_hash, role)
		VALUES ('Legacy Customer', 'legacy@example.com', 'legacy-hash', 'customer')
		RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var orderID int64
	if err := sqlDB.QueryRowContext(ctx, `INSERT INTO orders (user_id, package_id, template_id, status)
		VALUES (?, 'silver', 'S01', 'paid') RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO payments
		(order_id, user_id, payment_method_id, amount_cents, status)
		VALUES (?, ?, 'bank_transfer', 12345000, 'verified')`, orderID, userID); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpToContext(ctx, sqlDB, "migrations", 5); err != nil {
		t.Fatal(err)
	}
	var price int64
	var orderUserID sql.NullInt64
	var paymentAmount int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT o.price_cents, o.user_id, pay.amount_cents
		FROM orders AS o JOIN payments AS pay ON pay.order_id = o.id WHERE o.id = ?`, orderID).
		Scan(&price, &orderUserID, &paymentAmount); err != nil {
		t.Fatal(err)
	}
	if price == paymentAmount {
		t.Fatal("legacy-price fixture did not reproduce the package-price backfill")
	}
	if !orderUserID.Valid || orderUserID.Int64 != userID || paymentAmount != 12345000 {
		t.Fatalf("migration 00005 changed legacy order/payment identity or amount: user=%#v amount=%d", orderUserID, paymentAmount)
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, "SELECT price_cents FROM orders WHERE id = ?", orderID).Scan(&price); err != nil {
		t.Fatal(err)
	}
	if price != paymentAmount {
		t.Fatalf("historical order price = %d, want payment amount %d", price, paymentAmount)
	}
	var userCount int
	if err := sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE id = ?", userID).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 {
		t.Fatalf("legacy customer count = %d, want 1", userCount)
	}
	var foreignKeyViolations int
	if err := sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&foreignKeyViolations); err != nil {
		t.Fatal(err)
	}
	if foreignKeyViolations != 0 {
		t.Fatalf("foreign-key violations after migration = %d", foreignKeyViolations)
	}
	var foreignKeys int
	if err := sqlDB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys after migration = %d, want 1", foreignKeys)
	}
}

func TestBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	backup := filepath.Join(dir, "backup", "snapshot.db")
	restored := filepath.Join(dir, "restore", "restored.db")

	db, err := Open(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := db.CreateUser(ctx, "Test User", "restore@example.com", "", "hash", "customer")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, backup, restored); err != nil {
		t.Fatal(err)
	}

	restoredDB, err := Open(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	user, err := restoredDB.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "restore@example.com" {
		t.Fatalf("restored email = %q", user.Email)
	}

	if err := Restore(ctx, backup, restored); err == nil {
		t.Fatal("expected restore to refuse overwriting an existing file")
	}
	if err := Restore(ctx, backup, backup); err == nil {
		t.Fatal("expected restore to refuse identical source and destination")
	}
}

func TestCreateUserReportsDuplicateEmail(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.CreateUser(ctx, "First", "same@example.com", "", "hash", "customer"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(ctx, "Second", "SAME@example.com", "", "hash", "customer"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate registration error = %v, want ErrEmailTaken", err)
	}
}

func TestTemplateQueriesAndPackageEntitlements(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	templates, err := db.ListTemplates(ctx, "", "", "", "name_asc", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 || templates[0].Name != "Floral Elegant" || templates[1].Name != "Golden Kirana" {
		t.Fatalf("name-ascending page = %#v", templates)
	}
	templates, err = db.ListTemplates(ctx, "gold", "", "", "name_desc", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 || templates[0].Name != "Sakura Premium" {
		t.Fatalf("Gold package filter/name-desc sort = %#v", templates)
	}
	count, err := db.CountTemplates(ctx, "", "japanese", "")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("Japanese template count = %d, want 2", count)
	}
	supported, err := db.TemplateSupportsPackage(ctx, "G01", "silver")
	if err != nil {
		t.Fatal(err)
	}
	if supported {
		t.Fatal("Gold template incorrectly supports Silver package")
	}

	userID, err := db.CreateUser(ctx, "Order User", "order@example.com", "", "hash", "customer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateOrder(ctx, userID, "silver", "G01"); err == nil {
		t.Fatal("database accepted an order whose package does not support its template")
	}
	if _, err := db.CreateOrder(ctx, userID, "gold", "G01"); err != nil {
		t.Fatalf("database rejected a supported template package order: %v", err)
	}
}
