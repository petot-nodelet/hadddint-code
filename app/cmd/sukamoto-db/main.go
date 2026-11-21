package main

import (
	"context"
	"fmt"
	"net/mail"
	"os"
	"strings"

	"github.com/sukamoto/app/internal/auth"
	"github.com/sukamoto/app/internal/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx := context.Background()
	if len(args) == 4 && args[0] == "create-admin" {
		return createAdmin(ctx, args[1], args[2], args[3])
	}
	if len(args) != 3 {
		return fmt.Errorf("usage: sukamoto-db <backup|restore> <source-or-database> <destination> | create-admin <database> <username> <email>")
	}
	switch args[0] {
	case "backup":
		db, err := storage.Open(ctx, args[1])
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.Backup(ctx, args[2]); err != nil {
			return err
		}
		return nil
	case "restore":
		if err := storage.Restore(ctx, args[1], args[2]); err != nil {
			return err
		}
		db, err := storage.Open(ctx, args[2])
		if err != nil {
			return err
		}
		return db.Close()
	default:
		return fmt.Errorf("unknown command %q: use backup or restore", args[0])
	}
}

func createAdmin(ctx context.Context, databasePath, username, email string) error {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	parsedEmail, err := mail.ParseAddress(email)
	if username == "" || err != nil || parsedEmail.Address != email {
		return fmt.Errorf("create-admin: a username and valid email address are required")
	}
	password := os.Getenv("SUKAMOTO_ADMIN_PASSWORD")
	if password == "" {
		return fmt.Errorf("create-admin: set SUKAMOTO_ADMIN_PASSWORD to the new admin password")
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	db, err := storage.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.CreateUser(ctx, username, email, "", passwordHash, auth.RoleAdmin); err != nil {
		return err
	}
	fmt.Printf("Admin account %q created. Sign in with %s.\n", username, email)
	return nil
}
