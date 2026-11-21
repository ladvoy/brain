# Feature Specification: Ingestion v0 — files to pending facts

**Feature Branch**: `002-ingestion`

**Created**: 2026-11-22

**Status**: Draft — awaiting owner review (design only; no implementation until approved)

**Input**: Roadmap v0 ("ingestion: files → text → chunks → pending facts"), constitution B6 (`system` role), brain core admin plane (FR-006/007/010), portrait-card `source` rules (D1).

## Position

Ingestion is a **worker inside the brain repo**. It never calls an LLM (I10). It turns uploaded or path-referenced documents into **pending** fact proposals via the privileged API (same endpoints humans use), acting as principal/role `system` so every write is audited (B6). The owner confirms or rejects proposals; the worker does not auto-confirm.

OCR of scanned PDFs is **Phase B** of this feature (optional path); Phase A accepts text-extractable PDFs and plain text.

## User Scenarios & Testing

### User Story 1 — Upload becomes pending candidates (P1)

An owner (or admin bot) submits a document for a project and target card. The system extracts text, splits it into chunks, and creates one pending fact per chunk with `source.type=document` and a stable `source.ref`.

**Independent Test**: POST a small UTF-8 text fixture; expect N pending facts on the card; audit shows `system` mutations.

**Acceptance**:

1. **Given** a project with card `fred` and group vocabulary present, **When** a `.txt` file is ingested targeting `fred`, **Then** each non-empty chunk becomes a fact with `status=pending` and `source={type:document, ref:<job-id>#chunk-i}`.
2. **Given** the same ingest, **When** proposals are listed, **Then** those facts appear and are absent from `/context` assemblies until confirmed.

### User Story 2 — PDF text extraction (P1)

A digitally generated PDF (text layer present) is ingested the same way as plain text.

**Independent Test**: fixture PDF with known sentences → pending facts containing those sentences (normalized whitespace).

### User Story 3 — Idempotency and rejects (P2)

Re-ingesting the same document bytes does not duplicate pending facts; chunks matching a rejected fact of the same card are reported, not re-created (FR-010).

### User Story 4 — OCR path (P3 / Phase B)

A scan-only PDF triggers OCR when enabled; otherwise the job fails with a clear `needs_ocr` error. Out of Phase A implementation gate.

## Requirements

### Functional

- **FR-101**: WHEN an ingest job is created with a valid project admin token, THE SYSTEM SHALL accept `multipart` or raw bytes plus `card` slug and optional `filename`, and return a `job_id`.
- **FR-102**: WHEN the payload is `text/plain` or a PDF with an extractable text layer, THE SYSTEM SHALL extract UTF-8 text without calling an LLM.
- **FR-103**: WHEN extracted text is non-empty, THE SYSTEM SHALL split it into chunks of at most `INGEST_CHUNK_CHARS` (default 500) on paragraph boundaries when possible.
- **FR-104**: WHEN chunks are produced, THE SYSTEM SHALL create one pending fact per chunk on the target card via the privileged fact-create path, with `source.type=document` and `source.ref` including `job_id` and chunk index; actor in audit SHALL be `system` (or the configured ingest principal with role `system`).
- **FR-105**: WHEN a chunk's text matches a rejected fact of the same card, THE SYSTEM SHALL skip creation and record the skip on the job (FR-010 semantics).
- **FR-106**: WHEN the same content digest (`sha256` of raw bytes) is submitted again for the same card while a completed job with that digest exists, THE SYSTEM SHALL return the prior job result without creating duplicate facts.
- **FR-107**: WHEN extraction yields empty text and OCR is disabled, THE SYSTEM SHALL fail the job with reason `empty_text` (or `needs_ocr` for PDF without text layer).
- **FR-108**: WHEN a job completes or fails, THE SYSTEM SHALL persist job status (`queued|running|completed|failed`) and a summary (`chunks`, `created`, `skipped`, `error`) readable via admin API.
- **FR-109**: THE SYSTEM SHALL NOT confirm pending facts automatically; confirmation remains an owner action (FR-007).
- **FR-110**: THE SYSTEM SHALL NOT call an LLM at any stage of ingestion (I10 / NFR).

### Phase B (specified, deferred in tasks)

- **FR-111**: WHEN OCR is enabled and a PDF has no text layer, THE SYSTEM SHALL run local OCR and continue from FR-103.

### Non-functional

- **NFR-1**: Deterministic chunking for fixed input bytes and config.
- **NFR-2**: Jobs are project-scoped; no cross-project leakage.
- **NFR-3**: Max upload size configurable (`INGEST_MAX_BYTES`, default 5 MiB).

## Success criteria

- **SC-001**: Text fixture → pending facts visible in proposals; zero confirmed without owner action.
- **SC-002**: Confirmed ingest facts appear in subsequent `/context` for an authorized principal.
- **SC-003**: Duplicate digest ingest is a no-op for fact rows.
- **SC-004**: Audit entries attribute writes to `system`.

## Out of scope (v0)

- Semantic embedding / LLM summarization of chunks
- Auto-assignment of visibility/interpretation
- Multi-card routing from one document
- Cloud object storage (local bytes / multipart only)
