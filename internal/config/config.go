package config

import (
	"errors"
	"os"
)

// Config is the brain's runtime configuration, loaded from environment variables.
type Config struct {
	DatabaseURL string
	BindAddr    string
}

// Load reads the environment. DATABASE_URL is required (FR-020 storage is Postgres);
// BIND_ADDR defaults to :8080.
func Load() (Config, error) {
	db := os.Getenv("DATABASE_URL")
	if db == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	addr := os.Getenv("BIND_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return Config{DatabaseURL: db, BindAddr: addr}, nil
}
