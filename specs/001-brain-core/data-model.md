# Data Model — 001-brain-core (Phase 1)

**Date**: 2026-11-21 | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Storage decision**: ADR-0004

Status: draft — validated against the real ParadeDB container during `tasks.md` implementation (testcontainers).

## ERD (text sketch)

```
projects 1─┬─* principals ──────────┐ (card_slug → cards.slug, app-checked)
           ├─* roles                │
           ├─* cards 1─* facts ────┤ groups[] ⇄ groups (referential, app-checked)
           ├─* groups              │
           └─* audit               │
```

## DDL — migration `0001_init.sql`

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;      -- fuzzy alias resolution (pg_trgm ops)

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
CREATE INDEX cards_aliases_trgm ON cards USING gin ((aliases_trgm(aliases)) gin_trgm_ops);
-- `aliases_trgm(text[]) -> text` is an IMMUTABLE sql wrapper around array_to_string
-- (defined in migration 0001; array_to_string itself is STABLE and not indexable).
-- Fuzzy alias queries use the same expression: aliases_trgm(aliases) % 'grndpa' or
-- aliases_trgm(aliases) ILIKE '%…%'. Exact element match: plain GIN on the array when needed.

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

-- BM25 (requires ParadeDB image; access method is `bm25`, key field first)
CREATE INDEX facts_bm25 ON facts
USING bm25 (id, text, status, time_from, time_to)
WITH (key_field='id');

-- Fallback for vanilla Postgres (no pg_search): generated tsvector + GIN
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
  project_id uuid,                            -- no FK: survives project deletion? see note
  ts         timestamptz NOT NULL DEFAULT now(),
  actor      text,                            -- principal slug; NULL for failed auth
  kind       text NOT NULL,                   -- assembly | mutation | delete | inquiry | failed_auth
  object     text,                            -- fact_ref / card_slug / NULL
  details    jsonb NOT NULL DEFAULT '{}',     -- included/excluded/substitutions | before/after | reason
  UNIQUE (id)
);
CREATE INDEX audit_project_ts_idx ON audit (project_id, ts DESC);
-- append-only: the application role gets INSERT + SELECT only (no UPDATE/DELETE grants)
```

## Design notes and decisions

- **`fact_groups` table dropped** (deviation from the ADR-0004 first-cut): `groups text[]` + GIN preserves the many-to-many semantics (D6) with no join; unknown-group writes are rejected app-side (FR-009). Revisit only if group-level joins become a real query pattern.
- **Audit survives project deletion**: `audit.project_id` has **no FK** — D7 removes the family's memory, not the trail that proves what was done and when (audit is our accountability record, 152-FZ-adjacent). The app role has INSERT/SELECT only on `audit`.
- **Tokens are stored hashed** (sha256 in `bytea`); plaintext is returned once at project creation and never again (FR-001).
- **pg_search vs fallback**: at startup the store checks whether the `paradedb_bm25` index exists; if yes, text matching uses pg_search SQL (`@@@`, `paradedb.score()`), otherwise the `tsvector` path. Both are exercised in tests (integration suite runs both modes).
- **Statuses**: `pending → confirmed | rejected` (FR-006/007) and `deleted` (soft delete, FR-008). Physical removal happens only at whole-project deletion (`ON DELETE CASCADE`), D7.
- **Card defaults** live in `cards.defaults` (jsonb blob keyed by property: visibility, keys, time, revalidate, interpretation, disclosure, core); resolution order: fact value → card default → fail-closed/`owner`.
- **Multilingual BM25**: analyzer choice is a deployment-level setting (English default; Russian when a Russian project arrives) — recorded in `projects` at creation (column added in migration `0002_lang` if needed).
- **Import/export mapping** (US3/FR-021/FR-023): import runs in a single transaction (project → principals → roles → groups → cards → facts; all-or-nothing, FR-009 checks inline); export assembles the same shape back — the fixture round-trips through `retell.py` unchanged in meaning (SC-002).

## Format field → column mapping

| Format (fact) | Column(s) |
|---|---|
| `id` | `fact_ref` (unique per project) |
| `text` | `text` (+ `tsv` generated, bm25 index) |
| `author` | `author` (principal slug, nullable) |
| `status` | `status` (CHECK enum, incl. `deleted`) |
| `source` | `source jsonb` |
| `time.from/to` | `time_from` / `time_to` (date) |
| `revalidate` | `revalidate` (text) |
| `groups` | `groups text[]` (GIN) |
| `visibility` | `visibility jsonb` (NULL = card default) |
| `core` | `core text[]` (GIN) |
| `keys` | `keys text[]` (GIN) |
| `interpretation` | `interpretation jsonb` |
| `disclosure` | `disclosure jsonb` |
| `extensions` | `extensions jsonb` |

Card-level defaults are not columns — they live in `cards.defaults` and are merged in the assembly layer (resolution: fact → card default → fail-closed).
