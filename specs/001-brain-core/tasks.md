# Tasks: 001-brain-core

**Ordering principle (per Boris, 2026-11-21)**: design first, then implementation; golden suites are the **fitness gate built after the component is designed** — they assert isolation *and* audit completeness, budget, disclosure, time (not only isolation).

**Rule**: every task is TDD — the test for its FR is written first and must fail before the implementation makes it pass. EARS requirement ↔ test name mapping is mechanical (`FR-012` ↔ `TestFR012_*`).

## Phase 0 — Setup

- [x] **T001** Init `go.mod` (module `github.com/ladvoy/brain`, Go pinned); deps: pgx/v5, chi, goose, oapi-codegen, testcontainers-go, gojsonschema
- [x] **T002** `cmd/brain/main.go`: env config (`DATABASE_URL`, `BIND_ADDR`), chi router, graceful shutdown; health endpoint
- [x] **T003** `migrations/0001_init.sql` per data-model.md (8 tables; paradedb_bm25 index guarded by extension presence; tsvector generated column + GIN as fallback; audit INSERT/SELECT-only grants)
- [x] **T004** testcontainers harness: Postgres+pg_search container, both search modes asserted (pg_search present / vanilla fallback)

## Phase 1 — Contract & skeleton

- [ ] **T005** Vendor the Carter fixture: copy `examples/carter-family.json` → `testdata/carter.json` (record source commit; re-vendor checklist on portrait-spec change)
- [ ] **T006** Generate server stubs from `contracts/openapi.yaml` via oapi-codegen (also validates the contract parses)
- [ ] **T007** Contract tests: all 17 paths respond; requests without tokens → 401 + `failed_auth` audit entry (FR-001)

## Phase 2 — Privileged plane (TDD: test first per FR)

- [ ] **T008** Project create → hashed tokens returned once (FR-001); failed auth audited
- [ ] **T009** Principals / roles / groups CRUD with referential 422s (FR-009)
- [ ] **T010** Cards CRUD (FR-005: slug/type/name required)
- [ ] **T011** Facts lifecycle (FR-006 source→pending; FR-007 confirm/reject+keep; FR-008 soft delete `deleted`; FR-010 duplicate-rejection report)
- [ ] **T012** Audit writing for every mutation (FR-003: actor, before/after, ts) + audit read endpoint (filters actor/since/kind)
- [ ] **T013** Import (FR-023: single transaction, all-or-nothing, FR-009 inline) and export (FR-021: full portrait-card JSON validating against `schema/project.schema.json`)
- [ ] **T014** Project deletion (D7: cascade removes rows; `audit` rows retained — no FK by design)

## Phase 3 — Query plane (TDD, order = assembly pipeline)

- [ ] **T015** Principal resolution; unknown/missing → fail-closed everyone-only assembly + audit (FR-002)
- [ ] **T016** Card matching: exact alias, pg_trgm fuzzy, BM25 over fact texts/keys (FR-011)
- [ ] **T017** Visibility filter with audited reasons (FR-012: everyone/role/list/owner)
- [ ] **T018** Interpretation substitution; original only when visible; substitutions audited (FR-013)
- [ ] **T019** Touched groups = groups(matched facts) ∪ explicit refs; core-marked facts included (FR-014)
- [ ] **T020** Ended facts excluded; `include_history: true` includes flagged `ended` (FR-015)
- [ ] **T021** Disclosure rules: non-owner excluded, `meanwhile` note, inquiry audited (FR-016)
- [ ] **T022** Ranking core → BM25 → recency; budget cuts audited `cut_budget` (FR-017/FR-018)
- [ ] **T023** Response = structured context block only (FR-019/FR-025)

## Phase 4 — Golden suites (fitness gate)

- [ ] **T024** Isolation matrix on the Carter fixture: every principal × every fact × the story's query set; expected include/exclude/substitute sets asserted (spec US1 scenarios 1–4)
- [ ] **T025** Audit completeness: every response's audit reconstructs why each fact was included/excluded/substituted (SC-004)
- [ ] **T026** Import → export round-trip: validates against `schema/project.schema.json`; `retell.py` meaning-unchanged (SC-002)
- [ ] **T027** Latency smoke: assembly p95 < 100 ms on the fixture (SC-003)

## Phase 5 — Closeout

- [ ] **T028** OpenAPI served at `/openapi.json` + Swagger UI mounted (FR-022)
- [ ] **T029** `quickstart.md` executed end-to-end manually; two-role check done live
- [ ] **T030** Brain README updated; re-vendor checklist run (L1 changed: architecture skeleton, ADR-0004); Dockerfile (distroless) + compose draft for `self-host`

## Notes

- Tasks reference spec.md FRs and success criteria; a PR may bundle several tasks, but **the golden suites (Phase 4) must be green before any merge to `main`** — they are the fitness gate.
- OpenSpec note: this tasks.md is Spec Kit's birth artifact. After the component exists, changes flow through `openspec/changes/<name>/` (proposal → deltas → tasks → validate → archive).
