# Implementation Plan: Ingestion v0

**Branch**: `002-ingestion` | **Date**: 2026-11-22 | **Spec**: [spec.md](./spec.md)

## Summary

Add an ingest job API and in-process worker to the brain service: accept document bytes → extract text (plain / PDF text layer) → chunk → create pending facts through existing store paths with audit actor `system`. Phase B (OCR) is specified but not in the first implementation tasks.

## Technical Context

**Language**: Go (same module as brain core)

**Dependencies (planned)**:
- `ledongthuc/pdf` or `pdfcpu` / `gen2brain/go-fitz` — choose at implement time for text-layer PDF; prefer pure-Go if quality suffices
- Existing `internal/store` fact create + FR-010 duplicate reject
- No new LLM clients

**Storage**: new table `ingest_jobs` (see data-model.md); facts remain in `facts`

**Testing**: `go test` + testcontainers (reuse harness); fixtures under `testdata/ingest/`

## Constitution Check

- I10 / B1: no LLM in brain — extraction only. ✓
- B6: writes as `system`, audited. ✓
- I5: every mutation audited. ✓
- D1: every fact has `source`. ✓

**No violations.**

## Project Structure (additions)

```text
cmd/brain/                 # unchanged entry; worker goroutine started with server
internal/ingest/
  extract.go               # text/plain + PDF text layer
  chunk.go                 # deterministic chunker
  worker.go                # job runner
internal/api/              # handlers: create job, get job
migrations/0002_ingest.sql
specs/002-ingestion/
testdata/ingest/
```

## API sketch

- `POST /v1/projects/{id}/ingest` (admin) — multipart `file` + form `card`; returns `{job_id, status}`
- `GET /v1/projects/{id}/ingest/{job_id}` (admin) — job summary

OpenAPI delta: [contracts/openapi-delta.yaml](./contracts/openapi-delta.yaml). Merge into L1 `contracts/brain.openapi.yaml` on release.

## Phases

1. Design artifacts (this folder) — **current gate**
2. Implementation per [tasks.md](./tasks.md) after owner approval
3. Phase B OCR — separate task batch after Phase A green

## Deployment

Same binary; env: `INGEST_CHUNK_CHARS`, `INGEST_MAX_BYTES`, `INGEST_OCR=off|on`.
