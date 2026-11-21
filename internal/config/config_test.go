package config

import "testing"

func TestLoadMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("BIND_ADDR", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("BIND_ADDR", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BindAddr != ":8080" {
		t.Fatalf("default bind addr: got %q, want :8080", c.BindAddr)
	}
	if c.DatabaseURL != "postgres://x/y" {
		t.Fatalf("database url: got %q", c.DatabaseURL)
	}
}
