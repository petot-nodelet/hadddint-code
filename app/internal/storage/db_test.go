package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
