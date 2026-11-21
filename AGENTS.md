# AGENTS.md — brain (L2 layer)

Shared project truth (L1): `git@github.com:ladvoy/project-specs.git` (branch `main`).
When in doubt — read L1: `product/vision.md`, `invariants.md`, `glossary.md`, `contracts/`, `adr/`, `architecture.md`, `roadmap.md`.

## Session rules

1. Read `.specify/memory/constitution.md` **before any design or implementation decision**.
2. The vendored copies below are **read-only**: edit L1, then re-vendor (copy + bump the `source:` commit) in all repos (checklist in L1 README).
3. Component behavior specs live in **this repo** (Spec Kit artifacts under `.specify/`, later OpenSpec deltas under `openspec/`) — never in project-specs.
4. Requirements are written in EARS notation (`WHEN <actor> requests <X> (role R) THE SYSTEM SHALL <Y>`) — each requirement maps 1:1 to a golden test.
5. Workflows: Spec Kit pipeline (this repo's `.specify/`) for component birth; OpenSpec deltas for steady-flow changes after birth. Current progress: `001-brain-core` shipped (T001–T030); `002-ingestion` design in `specs/002-ingestion/` awaiting owner approve (T101) before implementation.
6. License: AGPL-3.0; CLA applies from the first commit (dual-licensing door).
7. Never commit family data: local `*.sqlite3` / `data/` are git-ignored; tests use the synthetic Carter reference from L1.
8. **Workflow order**: design first (spec → plan → tasks, approved by the owner), then implementation; **pushing to the remote happens as a separate step only after the owner's explicit approval** — local commits are fine at any time.

## Vendored from L1

- `source: project-specs@5076b81` — `invariants.md` → `.specify/memory/constitution.md` (plus brain-specific component articles)
- `source: project-specs@5076b81` — `glossary.md` → `docs/glossary.md`
