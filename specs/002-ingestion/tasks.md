# Tasks: 002-ingestion

**Gate**: design artifacts (this folder) reviewed by owner **before** any implementation task is started.

**Rule**: TDD — `FR-10x` ↔ `TestFR10x_*`.

## Phase 0 — Design (current)

- [x] **T100** Spec, plan, data-model, OpenAPI delta, quickstart draft
- [ ] **T101** Owner approve design (clarify closed) — **blocking**

## Phase 1 — Skeleton (after T101)

- [ ] **T102** Migration `0002_ingest.sql` per data-model.md
- [ ] **T103** `internal/ingest` package stub + env config (`INGEST_*`)
- [ ] **T104** Admin routes: `POST/GET …/ingest` wired in OpenAPI gen + handlers
- [ ] **T105** Re-vendor OpenAPI delta into `project-specs/contracts/brain.openapi.yaml`

## Phase 2 — Extract & chunk (Phase A)

- [ ] **T106** Text/plain extraction (FR-102) + max bytes (NFR-3)
- [ ] **T107** PDF text-layer extraction; empty → `empty_text` / `needs_ocr` (FR-107)
- [ ] **T108** Deterministic chunker (FR-103) + golden fixtures

## Phase 3 — Persist

- [ ] **T109** Job row create + sha256 idempotency (FR-106)
- [ ] **T110** Worker: chunks → pending facts + audit actor `system` (FR-104)
- [ ] **T111** Skips for rejected duplicates (FR-105 / FR-010)
- [ ] **T112** Job status API (FR-108)

## Phase 4 — Fitness

- [ ] **T113** SC-001/002/003/004 integration tests on testcontainers
- [ ] **T114** Quickstart executed live

## Phase 5 — Phase B (deferred)

- [ ] **T115** OCR path when `INGEST_OCR=on` (FR-111)

## Notes

- Implementation PRs must keep ingestion LLM-free (FR-110).
- After birth, further changes use OpenSpec deltas under `openspec/changes/`.
