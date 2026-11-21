// Package testharness starts a ParadeDB (or vanilla) container and a brain server for tests.
package testharness

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ladvoy/brain/internal/server"
)

// Env is a live brain HTTP server backed by a real Postgres.
type Env struct {
	Server   *httptest.Server
	Pool     *pgxpool.Pool
	DSN      string
	HasBM25  bool
	Cleanup  func()
}

func DockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/testharness → brain root
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// StartParadeDB boots paradedb/paradedb:latest, migrates, and returns an httptest server.
func StartParadeDB(t *testing.T) *Env {
	t.Helper()
	return start(t, "paradedb/paradedb:latest", true)
}

func StartVanilla(t *testing.T) *Env {
	t.Helper()
	return start(t, "postgres:16-alpine", false)
}

func start(t *testing.T, image string, expectBM25 bool) *Env {
	t.Helper()
	if !DockerAvailable() {
		t.Skip("docker unavailable")
	}
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        image,
		Env:          map[string]string{"POSTGRES_USER": "brain", "POSTGRES_PASSWORD": "brain", "POSTGRES_DB": "brain"},
		ExposedPorts: []string{"5432/tcp"},
		Cmd:          []string{"postgres", "-c", "fsync=off"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(120 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req, Started: true,
	})
	if err != nil {
		t.Skipf("container unavailable: %v", err)
	}

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	dsn := fmt.Sprintf("postgres://brain:brain@%s:%s/brain?sslmode=disable", host, port.Port())

	deadline := time.Now().Add(90 * time.Second)
	var sqlDB *sql.DB
	for {
		sqlDB, err = sql.Open("pgx", dsn)
		if err == nil && sqlDB.PingContext(ctx) == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = c.Terminate(ctx)
			t.Fatalf("db not ready: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	root := repoRoot(t)
	migPath := filepath.Join(root, "migrations", "0001_init.sql")
	raw, err := os.ReadFile(migPath)
	if err != nil {
		t.Fatalf("migration: %v", err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	if _, err := sqlDB.Exec(up); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}

	// workdir for openapi file resolution
	wd, _ := os.Getwd()
	_ = os.Chdir(root)
	t.Cleanup(func() { _ = os.Chdir(wd) })

	srv := httptest.NewServer(server.New(pool))
	env := &Env{
		Server:  srv,
		Pool:    pool,
		DSN:     dsn,
		HasBM25: expectBM25,
		Cleanup: func() {
			srv.Close()
			pool.Close()
			_ = c.Terminate(ctx)
		},
	}
	t.Cleanup(env.Cleanup)
	return env
}

// Client returns the httptest client.
func (e *Env) Client() *http.Client { return e.Server.Client() }

func (e *Env) URL(path string) string { return e.Server.URL + path }
