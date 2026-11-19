package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sukamoto/app/internal/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: sukamoto-db <backup|restore> <source-or-database> <destination>")
	}
	ctx := context.Background()
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
