# Data Model — 002-ingestion

## Table `ingest_jobs`

```sql
CREATE TABLE ingest_jobs (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id   uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  card_slug    text NOT NULL,
  filename     text,
  content_sha  bytea NOT NULL,          -- sha256(raw bytes)
  status       text NOT NULL CHECK (status IN ('queued','running','completed','failed')),
  summary      jsonb NOT NULL DEFAULT '{}',  -- {chunks, created, skipped, error, reason}
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, card_slug, content_sha)
);
CREATE INDEX ingest_jobs_project_idx ON ingest_jobs (project_id, created_at DESC);
```

Idempotency (FR-106): unique on `(project_id, card_slug, content_sha)` — second submit returns the existing row.

## Fact mapping

| Ingest field | Fact column |
|---|---|
| chunk text | `text` |
| always | `status=pending` |
| document | `source={type:document, ref:"ingest:{job_id}#{i}"}` |
| target card | `card_id` |
| optional | `groups` empty unless caller supplies defaults later |

No new fact columns. Visibility/interpretation left unset → card defaults / fail-closed owner on confirm-time edit.
