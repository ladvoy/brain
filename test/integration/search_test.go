// Package integration exercises the real storage: migrations + both search paths
// (pg_search on the ParadeDB image; tsvector fallback on vanilla Postgres).
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type dbVariant struct {
	name     string
	image    string
	pgSearch bool
}

func variants() []dbVariant {
	return []dbVariant{
		{name: "paradedb", image: "paradedb/paradedb:latest", pgSearch: true},
		{name: "vanilla", image: "postgres:16-alpine", pgSearch: false},
	}
}

func dockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func startDB(t *testing.T, v dbVariant) *sql.DB {
	t.Helper()
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        v.image,
		Env:          map[string]string{"POSTGRES_USER": "brain", "POSTGRES_PASSWORD": "brain", "POSTGRES_DB": "brain"},
		ExposedPorts: []string{"5432/tcp"},
		Cmd:          []string{"postgres", "-c", "fsync=off"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(120 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("container %s unavailable: %v", v.name, err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	dsn := fmt.Sprintf("postgres://brain:brain@%s:%s/brain?sslmode=disable", host, port.Port())

	deadline := time.Now().Add(90 * time.Second)
	var db *sql.DB
	for {
		db, err = sql.Open("pgx", dsn)
		if err == nil {
			if err = db.PingContext(ctx); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("db not ready: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Cleanup(func() { db.Close() })

	applyMigrations(t, db)
	return db
}

// applyMigrations executes the Up part of 0001_init.sql (the Down part is split off).
func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, err := os.ReadFile("../../migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	if _, err := db.Exec(up); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}

func searchByMode(t *testing.T, db *sql.DB, pgSearch bool) string {
	t.Helper()
	if pgSearch {
		// pg_search path (try parse() first, then the bare @@ literal form)
		for _, q := range []string{
			`SELECT fact_ref FROM facts WHERE text @@ paradedb.parse('firearms')`,
			`SELECT fact_ref FROM facts WHERE text @@ 'firearms'`,
		} {
			var ref string
			if err := db.QueryRow(q).Scan(&ref); err == nil {
				return ref
			}
		}
		t.Fatal("pg_search query found nothing")
	}
	var ref string
	err := db.QueryRow(`SELECT fact_ref FROM facts WHERE tsv @@ plainto_tsquery('simple', 'firearms')`).Scan(&ref)
	if err != nil {
		t.Fatalf("tsvector query: %v", err)
	}
	return ref
}

func TestSearchModes(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker unavailable")
	}
	for _, v := range variants() {
		t.Run(v.name, func(t *testing.T) {
			db := startDB(t, v)
			ctx := context.Background()

			// pg_search extension presence matches the variant
			var extCount int
			if err := db.QueryRowContext(ctx,
				`SELECT count(*) FROM pg_extension WHERE extname='pg_search'`).Scan(&extCount); err != nil {
				t.Fatalf("pg_extension: %v", err)
			}
			if v.pgSearch && extCount != 1 {
				t.Fatalf("paradedb variant: expected pg_search extension installed")
			}
			if !v.pgSearch && extCount != 0 {
				t.Fatalf("vanilla variant: unexpected pg_search")
			}

			// bm25 index presence matches the variant
			var idxCount int
			if err := db.QueryRowContext(ctx,
				`SELECT count(*) FROM pg_indexes WHERE indexname='facts_bm25'`).Scan(&idxCount); err != nil {
				t.Fatalf("pg_indexes: %v", err)
			}
			if v.pgSearch && idxCount != 1 {
				t.Fatalf("paradedb variant: facts_bm25 index missing — check the bm25 DDL against pg_search docs")
			}
			if !v.pgSearch && idxCount != 0 {
				t.Fatalf("vanilla variant: unexpected facts_bm25 index")
			}

			// seed: project → card → fact
			var projectID, cardID string
			if err := db.QueryRowContext(ctx,
				`INSERT INTO projects (slug, name, admin_token_hash, context_token_hash)
				 VALUES ('t', 'T', '\x00'::bytea, '\x00'::bytea) RETURNING id`,
			).Scan(&projectID); err != nil {
				t.Fatalf("insert project: %v", err)
			}
			if err := db.QueryRowContext(ctx,
				`INSERT INTO cards (project_id, slug, type, name) VALUES ($1, 'fred', 'person', 'Fred') RETURNING id`,
				projectID).Scan(&cardID); err != nil {
				t.Fatalf("insert card: %v", err)
			}
			_, err := db.ExecContext(ctx,
				`INSERT INTO facts (project_id, card_id, fact_ref, text, status, source, groups)
				 VALUES ($1, $2, 'f_test_1',
				         'Fred collects historical firearms and dreams of the Napoleonic era.',
				         'confirmed', '{"type":"note","ref":"t"}'::jsonb, '{hobbies}')`,
				projectID, cardID)
			if err != nil {
				t.Fatalf("insert fact: %v", err)
			}

			// the searched term must find the fact in BOTH search modes
			if got := searchByMode(t, db, v.pgSearch); got != "f_test_1" {
				t.Fatalf("search result: got %q, want f_test_1", got)
			}
		})
	}
}
