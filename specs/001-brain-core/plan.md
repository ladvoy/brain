# Implementation Plan: Brain Core — two-plane memory service

**Branch**: `001-brain-core` | **Date**: 2026-11-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-brain-core/spec.md` (clarify gate closed 2026-11-21).

## Summary

A headless two-plane memory service implementing the approved portrait-card semantics: the **privileged plane** (admin: CRUD, proposals ✓/✗, export/deletion, audit) consumed by the web-ui admin panel (family/company administrator) and internal system bots (ingestion, cron — role `system`); the **query plane** (`/context` assembly) consumed by everything else (butler-bot, brain-mcp, client LLM chats). Go service on PostgreSQL with ParadeDB pg_search; BM25-first deterministic assembly (no LLM, no answers — structured context blocks); golden suites on the Carter fixture are the fitness gate.

## Technical Context

**Language/Version**: Go (latest stable; version pinned in `go.mod` at implementation start)

**Primary Dependencies**:
- `pgx/v5` — PostgreSQL driver
- `chi` — HTTP router (stdlib-adjacent)
- `oapi-codegen` — contract-first: server generated from the OpenAPI contract (task 0.7 produces the contract; FR-022 serves it)
- `goose` — SQL migrations
- `gojsonschema` — validation on project import/export (D7)
- pg_search is used **via SQL** (`@@@` operator, `paradedb.score()`) — no Go-side dependency beyond SQL

**Storage**: PostgreSQL 17+ with ParadeDB pg_search extension (dev/self-host: ParadeDB docker image); schema per ADR-0004 first-cut; every row carries `project_id`

**Testing**: `go test` + testcontainers-go (real Postgres+pg_search container); golden suites on the Carter fixture

**Target Platform**: Linux, single binary, docker image; config via env vars

**Project Type**: web-service (API server)

**Performance Goals**: context assembly p95 < 100 ms on family-scale data (hundreds of facts); admin CRUD < 50 ms

**Constraints**: deterministic assembly — zero LLM/network calls in the request path; fail-closed on every unknown; single binary; no family data committed to the repo (fixture = synthetic Carters)

**Scale/Scope**: target 1000 client projects per instance; v0 scope per spec (no ingestion/cron workers, no UI, no embeddings)

## API planes — consumer mapping (per Boris's decision, 2026-11-21)

| Plane | Token | Consumers | Capabilities |
|---|---|---|---|
| **Privileged** (`/v1/projects/{pid}/…` admin) | project token (+ principal with role `system` for bots) | **internal bots** (ingestion, cron — tasks over users' memory), **web-ui admin panel** (family/company administrator) | CRUD cards/facts/groups/principals/roles, confirm/reject proposals, export/deletion, audit read |
| **Query** (`/context`, reads) | context token + `X-Brain-Principal` | everything else: butler-bot (messengers), brain-mcp (LLM chat clients), web-ui user views | assembly (FR-011…019), portrait reads |

This keeps fail-closed semantics intact: privileged operations stay behind the privileged token regardless of caller type; internal bots act with role `system` (constitution B6) and every write is audited like a human's. Final endpoint/token shapes are fixed in task 0.7 (OpenAPI contract) and this plan defers to the contract.

## Constitution Check

- I1 isolation at input — assembly filters before any model exists; the brain has no model. ✓
- I5 audit mandatory — FR-003/FR-004. ✓
- I10 deterministic access — NFR-1, no LLM in the request path. ✓
- B1 two planes, one DB — exactly the structure above. ✓
- B2 principal mandatory on query plane — FR-002 fail-closed. ✓
- B4 SQLite-first — superseded by ADR-0004 (Postgres). ✓ (no violation: ADR is the constitution's allowed amendment path)
- B6 cron/system role — workers deferred (task 1.2+, but the plane and role design accommodates them now). ✓

**No violations.**

## Project Structure

### Documentation (this feature)

```text
specs/001-brain-core/
├── spec.md              # approved requirements (clarify closed)
├── plan.md              # this file
├── data-model.md        # Phase 1: full DDL per ADR-0004 + pg_search index DDL
├── contracts/           # Phase 1: OpenAPI contract (task 0.7 input/output)
├── quickstart.md        # Phase 1: run locally (ParadeDB image + fixture load)
└── tasks.md             # Phase 2: ordered implementation checklist (golden tests included)
```

### Source Code (repository root)

```text
cmd/brain/
└── main.go              # single binary: config, router, graceful shutdown
internal/
├── api/
│   ├── admin/           # privileged plane handlers (generated stubs + logic)
│   └── contextx/        # query plane handlers
├── assembly/            # deterministic pipeline: match → expand (shared groups) →
│                        # visibility filter → interpretation → core → rank → budget → audit
├── store/               # pgx repositories; pg_search queries; fallback tsvector path
└── model/               # domain types (project, principal, card, fact, group, audit)
migrations/              # goose SQL: DDL per ADR-0004 + paradedb bm25 indexes
testdata/
└── carter.json          # vendored Carter fixture (from portrait-spec examples)
test/
├── golden/              # golden suites: isolation matrix, audit completeness, budget
├── contract/            # API ↔ OpenAPI conformance
└── integration/         # testcontainers: real Postgres + pg_search
```

**Structure Decision**: single Go module, packages layered by responsibility; contract-first API (0.7 contract → oapi-codegen → handlers implement it); golden fixtures loaded through the admin import path itself (FR-023) so the import/export code is exercised by every test run.

## Storage detail

- Tables per ADR-0004: `projects`, `principals`, `roles`, `cards`, `facts`, `groups`, `fact_groups`, `audit`; `extensions`/`interpretation`/`disclosure`/`source` as `jsonb`.
- Facts: `status ∈ {pending, confirmed, rejected, deleted}` (soft delete per FR-008); `time_from`/`time_to` date columns; `keys`, `groups`, `core` as `text[]` with GIN.
- BM25: `paradedb_bm25` index on fact text (analyzers per deployment language); fallback `tsvector` generated column + GIN — query path selects pg_search when the extension is present.
- Alias/keys exact match: GIN on `text[]`; fuzzy alias resolution: `pg_trgm`.
- Audit: append-only, `project_id`-scoped.

## Testing strategy

- TDD: every FR gets its test first; EARS requirement ↔ test name mapping is mechanical (`FR-012` ↔ `TestFR012_*`).
- **Golden suites** (the fitness gate, built after plan→tasks per Boris's ordering): the isolation matrix on the Carter fixture — every principal × every fact, expected include/exclude/substitute sets asserted (spec US1 scenarios + completeness/precision contract from `architecture.md`).
- Integration tests run against a real Postgres+pg_search container (testcontainers) — no mocked SQL semantics.
- Fixture is loaded through the admin import path itself (FR-023) — import/export code is exercised by every run.

## Deployment

- Single container for the brain (distroless base); ParadeDB image for Postgres; compose file lives in `self-host` (later task 1.6).
- Config via env: `DATABASE_URL`, `BIND_ADDR`, token secrets per project (v0: static tokens issued at project creation).
