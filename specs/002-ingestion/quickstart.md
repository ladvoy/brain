# Quickstart — ingestion v0 (draft; valid after implementation)

Status: instructions for the implementation phase (blocked on design approval T101).

## Prerequisites

Brain core running (see `specs/001-brain-core/quickstart.md`), admin token, a target card.

## 1. Ingest a text file

```bash
curl -s -X POST "localhost:8080/v1/projects/carter/ingest" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -F "card=fred" \
  -F "file=@testdata/ingest/sample.txt"
# → {"id":"…","status":"queued"|"completed",…}
```

## 2. Poll job

```bash
curl -s "localhost:8080/v1/projects/carter/ingest/$JOB_ID" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## 3. Review proposals

```bash
curl -s "localhost:8080/v1/projects/carter/proposals" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Confirm selected facts, then `/context` as a principal should include them.
