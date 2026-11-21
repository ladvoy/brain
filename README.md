# brain

The Ladvoy core: a headless family-memory API. **The brain never calls an LLM** — it assembles context deterministically; the client-side model does the thinking.

Two API planes, one service, one database (Postgres + ParadeDB pg_search):

| Plane | Auth | Path prefix | Consumers |
|---|---|---|---|
| **Privileged** | project (admin) token | `/v1/projects/{id}/…` | web-ui admin, internal bots (`system`) |
| **Query** | context token + `X-Brain-Principal` | `/v1/projects/{id}/context` | butler-bot, brain-mcp, user views |

Assembly pipeline: match → shared groups → visibility → interpretation → disclosure → ended/history → rank (core → BM25 → recency) → budget → structured block + audit.

## Status

**Fitness gate green** (T001–T027) and **closeout done** (T028–T030): OpenAPI + Swagger UI, quickstart verified live (Carter two-role), Dockerfile + compose draft.

## Run locally

Prerequisites: Docker, Go (see `go.mod`), [`goose`](https://github.com/pressly/goose).

```bash
docker run -d --name ladvoy-brain-db \
  -e POSTGRES_PASSWORD=brain -e POSTGRES_USER=brain -e POSTGRES_DB=brain \
  -p 5432:5432 paradedb/paradedb:latest

export DATABASE_URL="postgres://brain:brain@localhost:5432/brain?sslmode=disable"
export BIND_ADDR=":8080"
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/brain
```

- Health: `GET /healthz`
- OpenAPI: `GET /openapi.json` · YAML: `/openapi.yaml`
- Swagger UI: `http://localhost:8080/docs`
- Full walkthrough (create → import Carter → two-role `/context`): [`specs/001-brain-core/quickstart.md`](specs/001-brain-core/quickstart.md)

Or: `docker compose up --build` (see [`compose.yaml`](compose.yaml) — draft; production compose lives in `self-host` later).

## Tests

```bash
go test ./...
```

Integration/golden suites start a real ParadeDB container (testcontainers), load the Carter fixture via admin import, and assert isolation + audit + export schema + latency.

## Layout

```
cmd/brain/           # binary
internal/api/        # OpenAPI handlers (+ gen stubs)
internal/assembly/   # deterministic /context pipeline
internal/store/      # pgx + BM25/tsvector
migrations/          # goose SQL (ADR-0004)
specs/001-brain-core/
testdata/            # vendored Carter + schema (source: portrait-spec@c1aea9f)
```

## Vendored L1 / format

- `source: project-specs@5076b81` — constitution → `.specify/memory/constitution.md`, glossary → `docs/glossary.md`
- `source: portrait-spec@c1aea9f` — `testdata/carter.json`, `testdata/project.schema.json`

Re-vendor when L1 or portrait-spec change (checklist in project-specs README).

## See also

- L1: <https://github.com/ladvoy/project-specs>
- Format: <https://github.com/ladvoy/portrait-spec>
- Session rules: [`AGENTS.md`](AGENTS.md)

## License

AGPL-3.0 — see [LICENSE](LICENSE). CLA applies from the first commit.
