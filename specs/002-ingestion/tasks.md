# Tasks: 002-ingestion

**Gate**: design artifacts (this folder) reviewed by owner **before** any implementation task is started.

**Rule**: TDD — `FR-10x` ↔ `TestFR10x_*`.

## Phase 0 — Design

- [x] **T100** Spec, plan, data-model, OpenAPI delta, quickstart draft
- [x] **T101** Owner approve design (clarify closed) — approved 2026-11-22

## Phase 1 — Skeleton

- [x] **T102** Migration `0002_ingest.sql` per data-model.md
- [x] **T103** `internal/ingest` package stub + env config (`INGEST_*`)
- [x] **T104** Admin routes: `POST/GET …/ingest` wired in OpenAPI gen + handlers
- [x] **T105** Re-vendor OpenAPI delta into `project-specs/contracts/brain.openapi.yaml`

## Phase 2 — Extract & chunk (Phase A)

- [x] **T106** Text/plain extraction (FR-102) + max bytes (NFR-3)
- [x] **T107** PDF text-layer extraction; empty → `empty_text` / `needs_ocr` (FR-107)
- [x] **T108** Deterministic chunker (FR-103) + golden fixtures

## Phase 3 — Persist

- [x] **T109** Job row create + sha256 idempotency (FR-106)
- [x] **T110** Worker: chunks → pending facts + audit actor `system` (FR-104)
- [x] **T111** Skips for rejected duplicates (FR-105 / FR-010)
- [x] **T112** Job status API (FR-108)

## Phase 4 — Fitness

- [x] **T113** SC-001/002/003/004 integration tests on testcontainers
- [ ] **T114** Quickstart executed live — optional; covered by TestFR101_110

## Phase 5 — Phase B (deferred)

- [ ] **T115** OCR path when `INGEST_OCR=on` (FR-111)

## Notes

- Implementation PRs must keep ingestion LLM-free (FR-110).
- After birth, further changes use OpenSpec deltas under `openspec/changes/`.
