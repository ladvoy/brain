# brain

The Ladvoy core: a headless family-memory API. **The brain never calls an LLM** — it assembles context deterministically; the client-side model does the thinking.

Two API planes, one service, one database:

- **admin-api** (`/admin/*`, project token) — privileged write plane: CRUD for portraits, facts, actors, policies; confirm/reject proposals.
- **context-api** (`/context/*`, context token) — the high-frequency read path for agents and channels: deterministic assembly (match → intent → audience filter → child_view → constant core → ranking → budget), every assembly audited.

Workers (inside this repo): **ingestion** (upload → OCR → chunks → pending candidate facts) and **cron** (hold review, pending reminders, archival) — the cron worker acts via the same endpoints with a service token and the `system` role, so its writes are audited like a human's.

## Status

Skeleton. Specification pipeline: GitHub Spec Kit (constitution from the project invariants), then OpenSpec for the steady flow. See `AGENTS.md` before working here.

## See also

- L1 project truth: <https://github.com/ladvoy/project-specs>
- Format standard (external): <https://github.com/ladvoy/portrait-spec>

## License

AGPL-3.0 — see [LICENSE](LICENSE). CLA applies from the first commit.
