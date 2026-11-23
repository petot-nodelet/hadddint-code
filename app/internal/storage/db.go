// Package storage provides the SQLite data layer.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/sukamoto/app/db"
	"github.com/sukamoto/app/db/generated"
	_ "modernc.org/sqlite"
)

type DB struct {
	SQL     *sql.DB
	Queries *generated.Queries
	// UploadDir is the private filesystem directory holding payment proof
	// images. It is never served directly over HTTP.
	UploadDir string
}

var migrationMu sync.Mutex
var gooseSetup sync.Once
var gooseSetupErr error

func Open(ctx context.Context, path string) (*DB, error) {
	return OpenWithUploads(ctx, path, "")
}

// OpenWithUploads opens SQLite and configures the private payment proof
// directory. An empty uploads path disables filesystem-backed payments.
func OpenWithUploads(ctx context.Context, path, uploadsPath string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: database path cannot be empty")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("storage: create database directory: %w", err)
		}
	}
	var absoluteUploads string
	if uploadsPath != "" {
		resolvedUploads, err := filepath.Abs(uploadsPath)
		if err != nil {
			return nil, fmt.Errorf("storage: resolve upload directory: %w", err)
		}
		absoluteUploads = resolvedUploads
		if err := os.MkdirAll(absoluteUploads, 0o700); err != nil {
			return nil, fmt.Errorf("storage: create upload directory: %w", err)
		}
		if err := os.Chmod(absoluteUploads, 0o700); err != nil {
			return nil, fmt.Errorf("storage: restrict upload directory permissions: %w", err)
		}
	}

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	closeOnError := func(err error) (*DB, error) {
		_ = sqlDB.Close()
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return closeOnError(fmt.Errorf("storage: ping sqlite: %w", err))
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := sqlDB.ExecContext(ctx, pragma); err != nil {
			return closeOnError(fmt.Errorf("storage: set sqlite pragma: %w", err))
		}
	}
	if path != ":memory:" {
		if _, err := sqlDB.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
			return closeOnError(fmt.Errorf("storage: enable WAL: %w", err))
		}
		if err := restrictSQLiteFilePermissions(path); err != nil {
			return closeOnError(err)
		}
	}

	migrationMu.Lock()
	gooseSetup.Do(func() {
		gooseSetupErr = goose.SetDialect("sqlite3")
		if gooseSetupErr == nil {
			goose.SetBaseFS(db.Migrations)
		}
	})
	migrationErr := gooseSetupErr
	if migrationErr == nil {
		if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
			migrationErr = fmt.Errorf("storage: run migrations: %w", err)
		}
	}
	migrationMu.Unlock()
	if migrationErr != nil {
		return closeOnError(migrationErr)
	}
	if path != ":memory:" {
		if err := restrictSQLiteFilePermissions(path); err != nil {
			return closeOnError(err)
		}
	}

	queries := generated.New(sqlDB)
	if err := queries.DeleteExpiredSessions(ctx, time.Now().Unix()); err != nil {
		return closeOnError(fmt.Errorf("storage: clean expired sessions: %w", err))
	}
	return &DB{SQL: sqlDB, Queries: queries, UploadDir: absoluteUploads}, nil
}

func restrictSQLiteFilePermissions(path string) error {
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(file, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("storage: restrict SQLite file permissions for %q: %w", file, err)
		}
	}
	return nil
}

func (db *DB) Close() error {
	if err := db.SQL.Close(); err != nil {
		return fmt.Errorf("storage: close sqlite: %w", err)
	}
	return nil
}

func (db *DB) Backup(ctx context.Context, destination string) error {
	if destination == "" || destination == ":memory:" {
		return fmt.Errorf("storage: backup destination must be a filesystem path")
	}
	absoluteDestination, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("storage: resolve backup destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absoluteDestination), 0o750); err != nil {
		return fmt.Errorf("storage: create backup directory: %w", err)
	}
	if _, err := db.SQL.ExecContext(ctx, "VACUUM INTO ?", absoluteDestination); err != nil {
		return fmt.Errorf("storage: create backup: %w", err)
	}
	return nil
}

func Restore(ctx context.Context, source, destination string) error {
	if source == "" || destination == "" || source == ":memory:" || destination == ":memory:" {
		return fmt.Errorf("storage: restore paths must be filesystem paths")
	}
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("storage: resolve restore source: %w", err)
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("storage: resolve restore destination: %w", err)
	}
	if sourcePath == destinationPath {
		return fmt.Errorf("storage: restore source and destination must differ")
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return fmt.Errorf("storage: restore destination already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("storage: inspect restore destination: %w", err)
	}

	sourceDB, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return fmt.Errorf("storage: open restore source: %w", err)
	}
	sourceDB.SetMaxOpenConns(1)
	var integrity string
	if err := sourceDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		_ = sourceDB.Close()
		return fmt.Errorf("storage: check restore source integrity: %w", err)
	}
	if integrity != "ok" {
		_ = sourceDB.Close()
		return fmt.Errorf("storage: restore source integrity check failed: %s", integrity)
	}
	if _, err := sourceDB.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)"); err != nil {
		_ = sourceDB.Close()
		return fmt.Errorf("storage: checkpoint restore source: %w", err)
	}
	if err := sourceDB.Close(); err != nil {
		return fmt.Errorf("storage: close restore source: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o750); err != nil {
		return fmt.Errorf("storage: create restore directory: %w", err)
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("storage: open restore file: %w", err)
	}
	defer sourceFile.Close()
	destinationFile, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("storage: create restore file: %w", err)
	}
	_, copyErr := io.Copy(destinationFile, sourceFile)
	closeErr := destinationFile.Close()
	if copyErr != nil {
		_ = os.Remove(destinationPath)
		return fmt.Errorf("storage: copy restore file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(destinationPath)
		return fmt.Errorf("storage: close restore file: %w", closeErr)
	}
	return nil
}
