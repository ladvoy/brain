# Quickstart — run the brain locally (001-brain-core)

Status: instructions for the implementation phase; each step is exercised by `quickstart` checks in tasks.md.

## Prerequisites

- Docker (for the ParadeDB Postgres image)
- Go (stable; `go.mod` pins the version)
- `goose` for migrations (`go install github.com/pressly/goose/v3/cmd/goose@latest`)
- curl (or httpie) for the manual walkthrough

## 1. Start Postgres with pg_search

```bash
docker run -d --name ladvoy-brain-db \
  -e POSTGRES_PASSWORD=brain \
  -e POSTGRES_USER=brain \
  -e POSTGRES_DB=brain \
  -p 5432:5432 paradedb/paradedb:latest
```

## 2. Configure

```bash
export DATABASE_URL="postgres://brain:brain@localhost:5432/brain?sslmode=disable"
export BIND_ADDR=":8080"
```

## 3. Migrate

```bash
goose -dir migrations postgres "$DATABASE_URL" up
```

## 4. Run

```bash
go run ./cmd/brain
```

OpenAPI is served at `http://localhost:8080/openapi.json` (FR-022) — any Swagger UI against it works as an interactive admin surface during development.

## 5. Create a project

```bash
curl -s -X POST localhost:8080/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{"slug": "carter", "name": "The Carter family"}'
# → {"slug": "carter", "admin_token": "…", "context_token": "…"}  (shown once)
```

## 6. Import the Carter fixture

```bash
curl -s -X POST "localhost:8080/v1/projects/carter/import" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @testdata/carter.json
```

## 7. Ask in two roles (isolation, live)

```bash
# Rebecca — child interpretation, no original health fact:
curl -s -X POST "localhost:8080/v1/projects/carter/context" \
  -H "Authorization: Bearer $CONTEXT_TOKEN" \
  -H 'X-Brain-Principal: rebecca' \
  -H 'Content-Type: application/json' \
  -d '{"query": "What should I get grandpa?", "budget_tokens": 800}'

# Rose — adults: full health fact, and Rebecca's private note is absent:
curl -s -X POST "localhost:8080/v1/projects/carter/context" \
  -H "Authorization: Bearer $CONTEXT_TOKEN" \
  -H 'X-Brain-Principal: rose' \
  -H 'Content-Type: application/json' \
  -d '{"query": "How is grandpa doing?", "budget_tokens": 800}'
```

Expected: rebecca's block carries the child interpretation texts and `audit.substitutions`; rose's block carries the original diagnosis and never Rebecca's private facts; both blocks end with `audit` explaining every include/exclude.

## 8. Tests

```bash
go test ./...
```

Integration/golden suites start a real Postgres+pg_search container (testcontainers), load the fixture through the admin import path, and assert the isolation matrix.
