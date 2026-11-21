-- +goose Up
-- 0001_init.sql — per specs/001-brain-core/data-model.md (ADR-0004)

-- fuzzy alias resolution (pg_trgm is standard contrib; present in postgres and paradedb images)
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE projects (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug               text NOT NULL UNIQUE,
  name               text NOT NULL,
  admin_token_hash   bytea NOT NULL,          -- sha256(project token); plaintext returned once
  context_token_hash bytea NOT NULL,          -- sha256(context token)
  created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE principals (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug       text NOT NULL,
  attributes jsonb NOT NULL DEFAULT '{}',    -- client-facing configuration only
  card_slug  text,                            -- own card; existence checked app-side when set
  UNIQUE (project_id, slug)
);

CREATE TABLE roles (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       text NOT NULL,                   -- e.g. adults, children, managers
  members    text[] NOT NULL DEFAULT '{}',    -- principal slugs
  UNIQUE (project_id, name)
);

CREATE TABLE cards (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug       text NOT NULL,
  type       text NOT NULL,                   -- open vocabulary
  name       text NOT NULL,
  aliases    text[] NOT NULL DEFAULT '{}',
  defaults   jsonb NOT NULL DEFAULT '{}',     -- card-level visibility/keys/time/revalidate/interpretation/disclosure/core
  extensions jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, slug)
);
-- trigram fuzzy over array elements: array_to_string is STABLE (not indexable),
-- so a deterministic IMMUTABLE wrapper is used as the index expression and in queries.
CREATE OR REPLACE FUNCTION aliases_trgm(text[]) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$ SELECT coalesce(array_to_string($1, ' '), '') $$;
CREATE INDEX cards_aliases_trgm ON cards USING gin ((aliases_trgm(aliases)) gin_trgm_ops);

CREATE TABLE facts (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  card_id        uuid NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  fact_ref       text NOT NULL,               -- "f_..." unique per project (FR ids)
  text           text NOT NULL,               -- one thought, self-contained
  author         text,                        -- principal slug; NULL = default per spec §2.3
  status         text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','confirmed','rejected','deleted')),
  source         jsonb NOT NULL,              -- {type, ref, date?, note?} — required (D1)
  time_from      date,
  time_to        date,                        -- set = fact ended (history)
  revalidate     text,                        -- ISO-8601 duration or 'never'
  groups         text[] NOT NULL DEFAULT '{}',
  visibility     jsonb,                       -- NULL = card default (defaults → owner, D2)
  core           text[] NOT NULL DEFAULT '{}',
  keys           text[] NOT NULL DEFAULT '{}',
  interpretation jsonb NOT NULL DEFAULT '[]', -- [{when, text|directive}]
  disclosure     jsonb,                       -- {until, then, meanwhile?, review?}
  extensions     jsonb NOT NULL DEFAULT '{}',
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, fact_ref)
);
CREATE INDEX facts_card_idx   ON facts (project_id, card_id);
CREATE INDEX facts_groups_idx ON facts USING gin (groups);
CREATE INDEX facts_keys_idx   ON facts USING gin (keys);
CREATE INDEX facts_core_idx   ON facts USING gin (core);
CREATE INDEX facts_time_idx   ON facts (project_id, time_to);

-- Fallback full-text path (works on vanilla Postgres)
ALTER TABLE facts ADD COLUMN tsv tsvector
  GENERATED ALWAYS AS (to_tsvector('simple', coalesce(text, ''))) STORED;
CREATE INDEX facts_tsv_idx ON facts USING gin (tsv);

CREATE TABLE groups (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       text NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE audit (
  id         bigserial PRIMARY KEY,
  project_id uuid,                            -- no FK on purpose: audit survives project deletion (D7 note)
  ts         timestamptz NOT NULL DEFAULT now(),
  actor      text,                            -- principal slug; NULL for failed auth
  kind       text NOT NULL,                   -- assembly | mutation | delete | inquiry | failed_auth
  object     text,                            -- fact_ref / card_slug / NULL
  details    jsonb NOT NULL DEFAULT '{}',     -- included/excluded/substitutions | before/after | reason
  UNIQUE (id)
);
CREATE INDEX audit_project_ts_idx ON audit (project_id, ts DESC);
-- Append-only is enforced at the application role level in production
-- (INSERT + SELECT grants only); in dev the single role owns the tables.

-- BM25 path (requires the ParadeDB image). Guarded: on vanilla Postgres the
-- exception is caught and the tsvector fallback above stays active.
-- +goose StatementBegin
DO $$
BEGIN
  EXECUTE 'CREATE EXTENSION IF NOT EXISTS pg_search';
  EXECUTE 'CREATE INDEX facts_bm25 ON facts USING bm25 (id, text, status, time_from, time_to) WITH (key_field=''id'')';
  RAISE NOTICE 'pg_search active: bm25 index created';
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'pg_search unavailable: BM25 index skipped (tsvector fallback active)';
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS audit;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS facts;
DROP TABLE IF EXISTS cards;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS principals;
DROP TABLE IF EXISTS projects;
ALTER TABLE facts DROP COLUMN IF EXISTS tsv;
DROP INDEX IF EXISTS facts_bm25;
DROP EXTENSION IF EXISTS pg_search;
