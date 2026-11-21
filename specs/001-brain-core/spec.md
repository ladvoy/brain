# Feature Specification: Brain Core — two-plane memory service

**Feature Branch**: `001-brain-core`

**Created**: 2026-11-21

**Status**: Draft — awaiting review (Boris)

**Input**: The approved Portrait Card Spec (data semantics), ADR-0004 (PostgreSQL + ParadeDB pg_search), roadmap v0 ("the butler answers"), and the brain-api-v0 sketch (`saas/brain-api-v0.md`, terminology updated to the approved format).

## Position in the system

Per the L1 skeleton (`architecture.md`, approved): the brain is a **headless memory API**. It never answers users, never calls LLMs, never identifies humans — channels (butler-bot for messengers, brain-mcp for LLM-chat clients, web-ui for web) establish the principal and deliver the **structured context block** to the client's LLM, which composes the natural answer. This spec covers only the brain's boundary: the two planes (admin/context), assembly, and storage.

**Assembly quality contract** (deterministic, asserted later by golden suites): **completeness** — for the touched subjects, the context block carries everything related: matched cards, cards connected via **shared groups** (v0's cross-card link), and core-marked facts of touched groups; **precision** — nothing unrelated, every ranked cut recorded in audit.

## User Scenarios & Testing

### User Story 1 — Privacy-safe context assembly (P1)

A family member asks the assistant a question. The brain assembles context from the family's cards so that the reader receives exactly what they may know — original facts when visible, prepared interpretation texts when visibility fails but an interpretation rule matches — and nothing else. No restricted fact ever reaches the answering model.

**Why this priority**: this is the product's core promise; everything else is storage convenience around it.

**Independent Test**: load the Carter reference project, issue `POST /context` as each principal (rebecca, rose, jack), compare responses against the isolation matrix — zero leaks, substitutions and exclusions recorded in audit.

**Acceptance Scenarios**:

1. **Given** the Carter project (Fred's health fact: `visibility: role adults`, child interpretation present), **When** rebecca requests context about grandpa's health, **Then** the original fact text is absent from the response, the child interpretation text is present, and audit contains a `substitution` entry for the fact.
2. **Given** Rebecca's private test-fear fact (`visibility: owner`, author rebecca), **When** rose requests context mentioning Rebecca's school, **Then** the fact is absent from the response and audit records `excluded: deny_owner`.
3. **Given** Rebecca's wishlist (`visibility: list [jack, rose, hunter]`), **When** jack requests gift ideas for Rebecca, **Then** the wishlist fact is included with its source reference.
4. **Given** an unknown principal slug in `X-Brain-Principal`, **When** any context request arrives, **Then** the response contains only `everyone`-visible facts and core entries, and audit records the degraded assembly (`fail_closed_unknown_principal`).

### User Story 2 — Memory lifecycle (P2)

The owner maintains the family's memory through the admin API: create cards, add facts, confirm or reject pending candidates, correct mistakes. Every write is attributable and auditable. A fact without a source is a candidate, not knowledge.

**Why this priority**: without trustworthy writes the assembled context has nothing trustworthy to draw from.

**Independent Test**: create a project from the Carter example via the admin API, add a sourceless fact (expect `pending`), confirm it, reject another (expect it never re-proposed), delete a fact (expect it gone from assemblies, deletion present in audit).

**Acceptance Scenarios**:

1. **Given** an admin request adding a fact without `source`, **When** the fact is stored, **Then** its status is `pending`.
2. **Given** a pending fact, **When** the owner confirms it, **Then** status becomes `confirmed` and the fact participates in subsequent assemblies.
3. **Given** a rejected fact, **When** a later proposal arrives with the same text for the same card, **Then** the brain reports it as previously rejected instead of creating a duplicate.
4. **Given** any successful admin mutation, **When** audit is queried, **Then** the mutation appears with actor, before/after, and timestamp.

### User Story 3 — Project bootstrap and D7 (P2)

A new deployment is bootstrapped entirely through the admin API: project with roles, principals (each optionally linked to their own card), cards, groups — e.g., loading the Carter example file. The whole project can be exported back as a conforming portrait-card JSON and fully deleted.

**Why this priority**: proves D7 (nothing lives outside the project's rows) and gives tests their fixture-loading path.

**Independent Test**: `POST` the Carter example file as admin, `GET` the export, validate the export against `schema/project.schema.json`, then delete the project and verify zero remaining rows.

**Acceptance Scenarios**:

1. **Given** a conforming Carter example file, **When** imported via the admin API, **Then** all principals, roles, cards, and facts are queryable with their visibility and interpretations intact.
2. **Given** an existing project, **When** export is requested, **Then** the output validates against the portrait-card JSON Schema and round-trips through `retell.py` without loss.
3. **Given** an existing project, **When** deletion is requested, **Then** no rows with that `project_id` remain.

### User Story 4 — Time awareness (P3)

Facts carry their truth in the world: a factory audit held "until the end of November" leaves present-tense context when it ends, yet remains answerable about the past. Facts with a revalidation schedule surface as needing a re-check.

**Why this priority**: bi-temporality is in the approved format but not needed for the first usable slice.

**Independent Test**: with `time.to` in the past on the audit fact, default assembly excludes it; a request with `include_history: true` includes it flagged as ended; facts past their `revalidate` date are flagged `stale` in assembly output.

**Acceptance Scenarios**:

1. **Given** the audit fact (`time.to = 2026-11-30`), **When** context is assembled on 2026-12-01 without `include_history`, **Then** the fact is absent and audit shows `excluded: ended`.
2. **Given** the same state, **When** `include_history: true`, **Then** the fact is present and flagged `ended`.

### User Story 5 — Disclosure rules (P3)

A held fact (e.g., Becca's surprise plans) is invisible to others while the rule is active; if a `meanwhile` answer is defined, it appears in others' context as a note; the owner sees the fact and can end the rule early; other principals' touching inquiries are recorded so the owner can see them in audit.

**Why this priority**: the hold mechanic is distinctive for the trust story but not needed for the first slice.

**Independent Test**: with an embargoed fact on Fred's card, Fred's context includes it, Rose's excludes it and receives the `meanwhile` note, and the inquiry appears in audit.

**Acceptance Scenarios**:

1. **Given** an embargoed fact (until 2027-01-01), **When** rose's context touches the card, **Then** the fact is absent and the `meanwhile` note is present.
2. **Given** the same state, **When** fred's context is assembled, **Then** the fact is present.
3. **Given** the same state, **When** rose's query touches the card, **Then** audit records the inquiry (`disclosure_inquiry`).

### Edge Cases

- Unknown or malformed `X-Brain-Principal` → fail-closed assembly (US1.4), never an error leak of facts.
- Empty query or zero matches → response with core-marked facts of touched cards only; if nothing touched — empty facts + audit explaining why.
- `budget_tokens: 0` → empty facts, audit lists all candidates as `cut_budget`.
- Query mentions an alias shared by two cards → both cards matched; assembly includes both; audit lists matched cards.
- A fact references a group that was deleted → fact treated as groupless at write time; writes referencing unknown groups/principals/roles are rejected with 422.
- A role is deleted after facts reference it → those facts' role-visibility matches nobody (fail-closed), audit shows `excluded: role_unmatched`.
- A directive-type interpretation rule matches → the response carries the directive as `presentation_directive` for the client to apply (the brain does not run LLMs).
- Two core groups touched by one request → both groups' core facts included, deduplicated.

## Requirements

### Functional Requirements — access and audit

- **FR-001**: WHEN a request addresses a project without a valid token of that project, THE SYSTEM SHALL reject it with 401 and record the attempt in audit. The brain serves multiple projects per deployment; every request is scoped to exactly one project.
- **FR-002**: WHEN a context request lacks `X-Brain-Principal` or names an unregistered principal, THE SYSTEM SHALL assemble a fail-closed context (only `everyone` facts and `everyone` core entries) and record the degraded assembly in audit.
- **FR-003**: WHEN any admin mutation succeeds, THE SYSTEM SHALL record it in audit with actor, object, before/after, and timestamp.
- **FR-004**: WHEN any context assembly completes, THE SYSTEM SHALL record an audit entry listing included facts, excluded facts with reasons, and substitutions.

### Functional Requirements — cards and facts (admin plane)

- **FR-005**: WHEN a card is created without `slug`, `type`, or `name`, THE SYSTEM SHALL reject it with 422.
- **FR-006**: WHEN a fact is created without `source.type` and `source.ref`, THE SYSTEM SHALL store it with status `pending`.
- **FR-007**: WHEN the owner confirms a pending fact, THE SYSTEM SHALL set status `confirmed`; WHEN the owner rejects it, THE SYSTEM SHALL keep it with status `rejected` and never surface it as a proposal again.
- **FR-008**: WHEN a fact is deleted, THE SYSTEM SHALL soft-delete it: mark status `deleted` (excluded from all assemblies, row retained) and record the deletion in audit. A deletion is either a correction-with-replacement or an owner's refusal to encounter the fact — the brain does not adjudicate truth. Physical removal happens only at whole-project deletion (D7).
- **FR-009**: WHEN a fact or card references a group, role, or principal that does not exist in the project, THE SYSTEM SHALL reject the write with 422.
- **FR-010**: WHEN a proposal arrives whose text matches a rejected fact of the same card, THE SYSTEM SHALL report the prior rejection instead of creating a duplicate.

### Functional Requirements — context assembly (query plane)

- **FR-011**: WHEN a context request arrives from a registered principal, THE SYSTEM SHALL match cards by query terms against card names/aliases (exact and pg_trgm fuzzy) and fact texts/keys (BM25 via pg_search).
- **FR-012**: WHEN a candidate fact is visible to the principal (mode `everyone`, or `role` with membership, or `list` with the principal, or `owner` and the principal is the owner), THE SYSTEM SHALL include it in the candidate pool; otherwise THE SYSTEM SHALL exclude it and record the reason (`deny_role`, `deny_owner`, `deny_list`, `ended`).
- **FR-013**: WHEN an interpretation rule of a fact matches the principal, THE SYSTEM SHALL present the prepared text in place of the original (or carry the directive for the client), regardless of whether the original passed visibility; the original text SHALL appear only when visibility passed; every substitution SHALL be recorded in audit.
- **FR-014**: THE SYSTEM SHALL compute touched groups as the union of the groups of matched facts (each matched fact pulls its own groups in) and groups explicitly referenced by the client (`intent_hint`) or named in the query; WHEN a candidate fact is core-marked for a touched group, THE SYSTEM SHALL include it regardless of ranking position, subject to visibility.
- **FR-015**: WHEN a fact's `time.to` is in the past, THE SYSTEM SHALL exclude it from assembly unless `include_history: true`, in which case it is included flagged `ended`. (`include_history` concerns **fact history** — facts whose world-truth has ended per `time.to`; the brain stores no conversation history.)
- **FR-016**: WHEN a fact has an active disclosure rule and the requesting principal is not the owner, THE SYSTEM SHALL exclude the fact; WHEN a `meanwhile` answer is defined and the query touches the fact's card, THE SYSTEM SHALL include the `meanwhile` text as a note; WHEN a non-owner query touches the card, THE SYSTEM SHALL record the inquiry in audit.
- **FR-017**: WHEN the assembled set exceeds `budget_tokens`, THE SYSTEM SHALL cut lowest-ranked candidates and record them in audit with reason `cut_budget`.
- **FR-018**: WHEN ranking candidates, THE SYSTEM SHALL order core-marked facts for touched groups first, then BM25 relevance descending, then most recent `time.from` first.
- **FR-019**: WHEN assembly responds, THE SYSTEM SHALL return `{ "facts": [{id, text (possibly substituted), groups, source_ref, flags}], "notes": [meanwhile texts], "audit": {included, excluded, substitutions, matched_cards} }`.

### Functional Requirements — storage and interfaces

- **FR-020**: THE SYSTEM SHALL store all state in PostgreSQL with `project_id` scoping on every row, per ADR-0004; fact texts SHALL be BM25-indexed (pg_search); `keys`/`aliases` SHALL be GIN-indexed `text[]`.
- **FR-021**: WHEN a project export is requested, THE SYSTEM SHALL emit complete portrait-card JSON validating against `schema/project.schema.json`; WHEN deletion is requested, THE SYSTEM SHALL remove every row of the project.
- **FR-022**: THE SYSTEM SHALL serve OpenAPI generated from code at `/openapi.json`.
- **FR-023**: THE SYSTEM SHALL load the Carter reference project as a fixture for tests via the admin import path.
- **FR-024**: WHEN a matched card shares a group with other cards of the project, THE SYSTEM SHALL include those cards' facts from the shared groups as related candidates (subject to visibility and budget) — shared groups are v0's cross-card relatedness link.
- **FR-025**: THE SYSTEM SHALL return the structured context block only — never a natural-language answer; the client LLM composes the user-facing reply.

### Non-functional requirements

- **NFR-1**: Assembly and all access decisions are deterministic code; the brain never calls an LLM (constitution I10). Identical inputs and state produce identical responses.
- **NFR-2**: Every unknown/missing principal, role, or visibility element resolves to deny, never to allow (fail-closed, D2).
- **NFR-3**: Context assembly latency target: p95 < 100 ms on family-scale data (hundreds of facts) — no vectors required.

### Key Entities

- **Project** — the deployment unit; owns principals, roles, cards, groups, audit.
- **Principal** — a human or service reader/writer; slug, client-facing attributes, optional link to own card.
- **Card** — one concrete subject; slug, type, name, aliases, fact-level property defaults.
- **Fact** — one thought; text, source, status, time, visibility, groups, core, keys, interpretation, disclosure rule, revalidate, extensions.
- **Group** — project-wide named collection of facts (m2m).
- **Audit entry** — append-only record of assemblies and mutations.

## Success Criteria

- **SC-001**: The golden test suite on the Carter reference passes: zero leaks across the isolation matrix (every principal × every fact), substitutions and exclusions verified in audit.
- **SC-002**: The Carter example file imports, exports, and re-validates against `schema/project.schema.json` byte-structure intact (round-trip with `retell.py` unchanged in meaning).
- **SC-003**: Context assembly p95 < 100 ms on the Carter fixture.
- **SC-004**: Every response's audit entry is sufficient to reconstruct *why* each fact was included, excluded, or substituted.

## Assumptions

- One project per deployment in v0; the token is bound to that project; `project_id` columns are present from day one for the cloud stage.
- Single writer service in v0; PostgreSQL concurrency is relied upon, not application-level locking.
- Texts may be any language; BM25 analyzers are configured per deployment (English default; Russian when a Russian project arrives).

## Out of scope (v0)

Ingestion worker (OCR/PDF — task 1.2); cron worker (revalidation sweeps); notifications (channel concern); UI; multi-project API and multi-tenant orchestration; LLM calls (never); relations between cards as structured edges (facts carry them textually until the format grows relations); semantic embeddings (BM25-first per decision; pgvector deferred).

## Dependencies

- Portrait Card Spec (approved 2026-11-21) — data semantics of visibility, interpretation, disclosure, core marking, time.
- ADR-0004 — PostgreSQL + ParadeDB pg_search.
- Brain constitution (`.specify/memory/constitution.md`) — invariants I1–I10, articles B1–B6.
- Carter reference (`examples/carter-family.json` in portrait-spec, vendored for tests as fixture).

## Clarified decisions (clarify gate, 2026-11-21)

- **Q1 → multi-project per deployment.** A project is user-created via the admin API at the very start (before principals and data exist); the brain serves many projects at once, and every request is scoped to exactly one project by its token and path (FR-001). The earlier "one deployment = one project" reading was a leftover of the old sketch and is dropped.
- **Q2 → group-touch rule (FR-014).** Matched facts pull their own groups into the touched set; groups may also be touched explicitly (`intent_hint` or named in the query). Core-marked facts of touched groups are included necessarily.
- **Q3 → no importance concept.** Importance is subjective (potentially per-principal, per-subject) — interesting, but with no clear data representation or use for now; deferred indefinitely. Ranking stays core → BM25 → recency (FR-018).
- **Q4 → soft deletion (FR-008).** Deletion is a correction-with-replacement or an owner's refusal to encounter a fact; truth is not adjudicated. Physical removal only at whole-project deletion (D7).
- **Q5 → history = fact history, not conversations.** `include_history` (FR-015) concerns facts whose world-truth ended (`time.to`) — the bi-temporal "answerable about the past" property of the format. The brain stores no conversation history; query logging/statistics is an ops concern, outside the brain.
