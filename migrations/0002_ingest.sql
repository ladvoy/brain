-- +goose Up
CREATE TABLE ingest_jobs (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id   uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  card_slug    text NOT NULL,
  filename     text,
  content_sha  bytea NOT NULL,
  status       text NOT NULL CHECK (status IN ('queued','running','completed','failed')),
  summary      jsonb NOT NULL DEFAULT '{}',
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, card_slug, content_sha)
);
CREATE INDEX ingest_jobs_project_idx ON ingest_jobs (project_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS ingest_jobs;
