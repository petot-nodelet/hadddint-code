// Package config loads environment-driven configuration for SUKAMOTO.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime settings sourced from environment variables.
type Config struct {
	Address         string
	DatabasePath    string
	Env             string
	SessionName     string
	SessionTTLHours int
}

// Load reads configuration from the environment with safe defaults for dev.
func Load() (*Config, error) {
	c := &Config{
		Address:         getenv("SUKAMOTO_ADDR", ":8080"),
		DatabasePath:    getenv("DATABASE_PATH", "data/sukamoto.db"),
		Env:             getenv("APP_ENV", "development"),
		SessionName:     "sukamoto_session",
		SessionTTLHours: 72,
	}
	if c.DatabasePath == "" {
		return nil, fmt.Errorf("config: DATABASE_PATH cannot be empty")
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
