package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sukamoto/app/internal/auth"
	"github.com/sukamoto/app/internal/config"
	"github.com/sukamoto/app/internal/storage"
	"github.com/sukamoto/app/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := storage.Open(context.Background(), cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	sessionManager, err := auth.NewManager(db.Queries, cfg.SessionName, time.Duration(cfg.SessionTTLHours)*time.Hour, cfg.Env == "production")
	if err != nil {
		return err
	}
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = ".."
	}
	handler, err := web.NewServer(db, sessionManager, staticDir)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("SUKAMOTO listening on %s", cfg.Address)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http server shutdown: %w", err)
		}
		err := <-serverErrors
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}
}
