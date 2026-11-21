// Package store is the pgx persistence layer for brain.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ladvoy/brain/internal/model"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrValidation    = errors.New("validation")
	ErrDuplicateReject = errors.New("duplicate of rejected fact")
)

// Store wraps a pgx pool and search-mode flag.
type Store struct {
	pool     *pgxpool.Pool
	bm25     bool
	now      func() time.Time
}

func New(pool *pgxpool.Pool) *Store {
	s := &Store{pool: pool, now: time.Now}
	s.bm25 = s.detectBM25(context.Background())
	return s
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }
func (s *Store) HasBM25() bool       { return s.bm25 }

func (s *Store) SetNow(fn func() time.Time) {
	if fn != nil {
		s.now = fn
	}
}

func (s *Store) detectBM25(ctx context.Context) bool {
	var n int
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname='facts_bm25'`).Scan(&n)
	return n > 0
}

func (s *Store) WriteAudit(ctx context.Context, projectID *uuid.UUID, actor *string, kind string, object *string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO audit (project_id, actor, kind, object, details) VALUES ($1,$2,$3,$4,$5)`,
		projectID, actor, kind, object, b)
	return err
}

// --- projects ---

func (s *Store) CreateProject(ctx context.Context, slug, name string, adminHash, contextHash []byte) (*model.Project, error) {
	var p model.Project
	err := s.pool.QueryRow(ctx,
		`INSERT INTO projects (slug, name, admin_token_hash, context_token_hash)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, slug, name, admin_token_hash, context_token_hash, created_at`,
		slug, name, adminHash, contextHash,
	).Scan(&p.ID, &p.Slug, &p.Name, &p.AdminTokenHash, &p.ContextTokenHash, &p.CreatedAt)
	if err != nil {
		if isUnique(err) {
			return nil, fmt.Errorf("%w: slug taken", ErrConflict)
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) GetProject(ctx context.Context, idOrSlug string) (*model.Project, error) {
	var p model.Project
	q := `SELECT id, slug, name, admin_token_hash, context_token_hash, created_at FROM projects WHERE slug=$1`
	args := []any{idOrSlug}
	if u, err := uuid.Parse(idOrSlug); err == nil {
		q = `SELECT id, slug, name, admin_token_hash, context_token_hash, created_at FROM projects WHERE id=$1 OR slug=$2`
		args = []any{u, idOrSlug}
	}
	err := s.pool.QueryRow(ctx, q, args...).Scan(&p.ID, &p.Slug, &p.Name, &p.AdminTokenHash, &p.ContextTokenHash, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) UpdateProjectName(ctx context.Context, id uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx, `UPDATE projects SET name=$2 WHERE id=$1`, id, name)
	return err
}

func (s *Store) DeleteProject(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountProjectRows(ctx context.Context, id uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM principals WHERE project_id=$1) +
		  (SELECT count(*) FROM roles WHERE project_id=$1) +
		  (SELECT count(*) FROM cards WHERE project_id=$1) +
		  (SELECT count(*) FROM facts WHERE project_id=$1) +
		  (SELECT count(*) FROM groups WHERE project_id=$1)`, id).Scan(&n)
	return n, err
}

// --- principals / roles / groups ---

func (s *Store) ListPrincipals(ctx context.Context, projectID uuid.UUID) ([]model.Principal, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, project_id, slug, attributes, card_slug FROM principals WHERE project_id=$1 ORDER BY slug`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Principal
	for rows.Next() {
		var p model.Principal
		var attrs []byte
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Slug, &attrs, &p.CardSlug); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(attrs, &p.Attributes)
		if p.Attributes == nil {
			p.Attributes = map[string]any{}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPrincipal(ctx context.Context, projectID uuid.UUID, slug string) (*model.Principal, error) {
	var p model.Principal
	var attrs []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, slug, attributes, card_slug FROM principals WHERE project_id=$1 AND slug=$2`,
		projectID, slug,
	).Scan(&p.ID, &p.ProjectID, &p.Slug, &attrs, &p.CardSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(attrs, &p.Attributes)
	return &p, nil
}

func (s *Store) CreatePrincipal(ctx context.Context, projectID uuid.UUID, slug string, attrs map[string]any, card *string) error {
	if card != nil && *card != "" {
		if _, err := s.GetCard(ctx, projectID, *card); err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: unknown card %q", ErrValidation, *card)
			}
			return err
		}
	}
	if attrs == nil {
		attrs = map[string]any{}
	}
	b, _ := json.Marshal(attrs)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO principals (project_id, slug, attributes, card_slug) VALUES ($1,$2,$3,$4)`,
		projectID, slug, b, card)
	if isUnique(err) {
		return fmt.Errorf("%w: principal exists", ErrConflict)
	}
	return err
}

func (s *Store) ListRoles(ctx context.Context, projectID uuid.UUID) ([]model.Role, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, project_id, name, members FROM roles WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Role
	for rows.Next() {
		var r model.Role
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Members); err != nil {
			return nil, err
		}
		if r.Members == nil {
			r.Members = []string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PutRole(ctx context.Context, projectID uuid.UUID, name string, members []string) error {
	for _, m := range members {
		if _, err := s.GetPrincipal(ctx, projectID, m); err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: unknown principal %q", ErrValidation, m)
			}
			return err
		}
	}
	if members == nil {
		members = []string{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO roles (project_id, name, members) VALUES ($1,$2,$3)
		ON CONFLICT (project_id, name) DO UPDATE SET members=EXCLUDED.members`,
		projectID, name, members)
	return err
}

func (s *Store) ListGroups(ctx context.Context, projectID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM groups WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, projectID uuid.UUID, name string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO groups (project_id, name) VALUES ($1,$2)`, projectID, name)
	if isUnique(err) {
		return fmt.Errorf("%w: group exists", ErrConflict)
	}
	return err
}

func (s *Store) GroupExists(ctx context.Context, projectID uuid.UUID, name string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE project_id=$1 AND name=$2`, projectID, name).Scan(&n)
	return n > 0, err
}

func (s *Store) RoleExists(ctx context.Context, projectID uuid.UUID, name string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM roles WHERE project_id=$1 AND name=$2`, projectID, name).Scan(&n)
	return n > 0, err
}

// --- cards ---

func (s *Store) ListCards(ctx context.Context, projectID uuid.UUID, q string) ([]model.Card, error) {
	var rows pgx.Rows
	var err error
	if q == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, project_id, slug, type, name, aliases, defaults, extensions, created_at, updated_at
			 FROM cards WHERE project_id=$1 ORDER BY slug`, projectID)
	} else {
		like := "%" + q + "%"
		rows, err = s.pool.Query(ctx,
			`SELECT id, project_id, slug, type, name, aliases, defaults, extensions, created_at, updated_at
			 FROM cards WHERE project_id=$1 AND (
			   name ILIKE $2 OR slug ILIKE $2 OR aliases_trgm(aliases) ILIKE $2
			 ) ORDER BY slug`, projectID, like)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCards(rows)
}

func (s *Store) GetCard(ctx context.Context, projectID uuid.UUID, slug string) (*model.Card, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, project_id, slug, type, name, aliases, defaults, extensions, created_at, updated_at
		 FROM cards WHERE project_id=$1 AND slug=$2`, projectID, slug)
	c, err := scanCard(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateCard(ctx context.Context, c *model.Card) error {
	if c.Slug == "" || c.Type == "" || c.Name == "" {
		return fmt.Errorf("%w: slug, type, name required", ErrValidation)
	}
	if c.Aliases == nil {
		c.Aliases = []string{}
	}
	if c.Defaults == nil {
		c.Defaults = map[string]any{}
	}
	if c.Extensions == nil {
		c.Extensions = map[string]any{}
	}
	def, _ := json.Marshal(c.Defaults)
	ext, _ := json.Marshal(c.Extensions)
	err := s.pool.QueryRow(ctx, `
		INSERT INTO cards (project_id, slug, type, name, aliases, defaults, extensions)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at, updated_at`,
		c.ProjectID, c.Slug, c.Type, c.Name, c.Aliases, def, ext,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if isUnique(err) {
		return fmt.Errorf("%w: card exists", ErrConflict)
	}
	return err
}

func (s *Store) UpdateCard(ctx context.Context, c *model.Card) error {
	if c.Defaults == nil {
		c.Defaults = map[string]any{}
	}
	if c.Extensions == nil {
		c.Extensions = map[string]any{}
	}
	if c.Aliases == nil {
		c.Aliases = []string{}
	}
	def, _ := json.Marshal(c.Defaults)
	ext, _ := json.Marshal(c.Extensions)
	ct, err := s.pool.Exec(ctx, `
		UPDATE cards SET name=$3, type=$4, aliases=$5, defaults=$6, extensions=$7, updated_at=now()
		WHERE project_id=$1 AND slug=$2`,
		c.ProjectID, c.Slug, c.Name, c.Type, c.Aliases, def, ext)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCard(ctx context.Context, projectID uuid.UUID, slug string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM cards WHERE project_id=$1 AND slug=$2`, projectID, slug)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- facts ---

func (s *Store) validateFactRefs(ctx context.Context, projectID uuid.UUID, f *model.Fact) error {
	for _, g := range f.Groups {
		ok, err := s.GroupExists(ctx, projectID, g)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: unknown group %q", ErrValidation, g)
		}
	}
	for _, g := range f.Core {
		ok, err := s.GroupExists(ctx, projectID, g)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: unknown core group %q", ErrValidation, g)
		}
	}
	if f.Author != nil && *f.Author != "" {
		if _, err := s.GetPrincipal(ctx, projectID, *f.Author); err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: unknown principal %q", ErrValidation, *f.Author)
			}
			return err
		}
	}
	if f.Visibility != nil && f.Visibility.Mode == "role" && f.Visibility.Role != "" {
		ok, err := s.RoleExists(ctx, projectID, f.Visibility.Role)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: unknown role %q", ErrValidation, f.Visibility.Role)
		}
	}
	if f.Visibility != nil && f.Visibility.Mode == "list" {
		for _, p := range f.Visibility.Principals {
			if _, err := s.GetPrincipal(ctx, projectID, p); err != nil {
				if errors.Is(err, ErrNotFound) {
					return fmt.Errorf("%w: unknown principal %q", ErrValidation, p)
				}
				return err
			}
		}
	}
	return nil
}

func (s *Store) CreateFact(ctx context.Context, f *model.Fact) error {
	if err := s.validateFactRefs(ctx, f.ProjectID, f); err != nil {
		return err
	}
	// FR-010: duplicate of rejected
	var rejectedRef string
	err := s.pool.QueryRow(ctx, `
		SELECT fact_ref FROM facts
		WHERE project_id=$1 AND card_id=$2 AND status='rejected' AND text=$3
		LIMIT 1`, f.ProjectID, f.CardID, f.Text).Scan(&rejectedRef)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrDuplicateReject, rejectedRef)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	if f.Source.Type == "" || f.Source.Ref == "" {
		f.Status = "pending"
		if f.Source.Type == "" {
			f.Source.Type = "unknown"
		}
		if f.Source.Ref == "" {
			f.Source.Ref = "pending"
		}
	}
	if f.Status == "" {
		f.Status = "confirmed"
	}
	normalizeFactSlices(f)
	src, _ := json.Marshal(f.Source)
	vis, _ := marshalNullable(f.Visibility)
	interp, _ := json.Marshal(f.Interpretation)
	disc, _ := marshalNullable(f.Disclosure)
	ext, _ := json.Marshal(f.Extensions)

	err = s.pool.QueryRow(ctx, `
		INSERT INTO facts (
		  project_id, card_id, fact_ref, text, author, status, source,
		  time_from, time_to, revalidate, groups, visibility, core, keys,
		  interpretation, disclosure, extensions
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, created_at, updated_at`,
		f.ProjectID, f.CardID, f.FactRef, f.Text, f.Author, f.Status, src,
		f.TimeFrom, f.TimeTo, f.Revalidate, f.Groups, vis, f.Core, f.Keys,
		interp, disc, ext,
	).Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
	if isUnique(err) {
		return fmt.Errorf("%w: fact_ref exists", ErrConflict)
	}
	return err
}

func (s *Store) GetFact(ctx context.Context, projectID uuid.UUID, factRef string) (*model.Fact, error) {
	row := s.pool.QueryRow(ctx, factSelect+` WHERE f.project_id=$1 AND f.fact_ref=$2`, projectID, factRef)
	f, err := scanFact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *Store) ListFacts(ctx context.Context, projectID uuid.UUID, cardSlug string, status string) ([]model.Fact, error) {
	q := factSelect + ` WHERE f.project_id=$1 AND c.slug=$2`
	args := []any{projectID, cardSlug}
	if status != "" {
		q += ` AND f.status=$3`
		args = append(args, status)
	}
	q += ` ORDER BY f.fact_ref`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

func (s *Store) ListProposals(ctx context.Context, projectID uuid.UUID) ([]model.Fact, error) {
	rows, err := s.pool.Query(ctx, factSelect+` WHERE f.project_id=$1 AND f.status='pending' ORDER BY f.created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

func (s *Store) SetFactStatus(ctx context.Context, projectID uuid.UUID, factRef, status string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE facts SET status=$3, updated_at=now() WHERE project_id=$1 AND fact_ref=$2`,
		projectID, factRef, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateFact(ctx context.Context, f *model.Fact) error {
	if err := s.validateFactRefs(ctx, f.ProjectID, f); err != nil {
		return err
	}
	normalizeFactSlices(f)
	src, _ := json.Marshal(f.Source)
	vis, _ := marshalNullable(f.Visibility)
	interp, _ := json.Marshal(f.Interpretation)
	disc, _ := marshalNullable(f.Disclosure)
	ext, _ := json.Marshal(f.Extensions)
	ct, err := s.pool.Exec(ctx, `
		UPDATE facts SET text=$3, author=$4, status=$5, source=$6, time_from=$7, time_to=$8,
		  revalidate=$9, groups=$10, visibility=$11, core=$12, keys=$13,
		  interpretation=$14, disclosure=$15, extensions=$16, updated_at=now()
		WHERE project_id=$1 AND fact_ref=$2`,
		f.ProjectID, f.FactRef, f.Text, f.Author, f.Status, src, f.TimeFrom, f.TimeTo,
		f.Revalidate, f.Groups, vis, f.Core, f.Keys, interp, disc, ext)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListConfirmedFacts(ctx context.Context, projectID uuid.UUID) ([]model.Fact, error) {
	rows, err := s.pool.Query(ctx, factSelect+` WHERE f.project_id=$1 AND f.status='confirmed'`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

func (s *Store) MatchCards(ctx context.Context, projectID uuid.UUID, query string) ([]model.Card, error) {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	seen := map[string]model.Card{}
	for _, term := range terms {
		like := "%" + term + "%"
		rows, err := s.pool.Query(ctx, `
			SELECT id, project_id, slug, type, name, aliases, defaults, extensions, created_at, updated_at
			FROM cards WHERE project_id=$1 AND (
			  lower(name)=lower($2) OR lower(slug)=lower($2)
			  OR EXISTS (SELECT 1 FROM unnest(aliases) a WHERE lower(a)=lower($2))
			  OR aliases_trgm(aliases) % $2
			  OR name ILIKE $3 OR aliases_trgm(aliases) ILIKE $3
			)`, projectID, term, like)
		if err != nil {
			return nil, err
		}
		cards, err := scanCards(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, c := range cards {
			seen[c.Slug] = c
		}
	}
	out := make([]model.Card, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) SearchFactsBM25(ctx context.Context, projectID uuid.UUID, query string) ([]model.Fact, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if s.bm25 {
		rows, err := s.pool.Query(ctx, `
			SELECT `+factCols+`, coalesce(paradedb.score(f.id), 0) AS score
			FROM facts f JOIN cards c ON c.id=f.card_id
			WHERE f.project_id=$1 AND f.status='confirmed' AND f.text @@@ $2
			ORDER BY score DESC`, projectID, query)
		if err != nil {
			// fallback if operator unsupported
			return s.searchFactsTS(ctx, projectID, query)
		}
		defer rows.Close()
		return scanFactsScored(rows)
	}
	return s.searchFactsTS(ctx, projectID, query)
}

func (s *Store) searchFactsTS(ctx context.Context, projectID uuid.UUID, query string) ([]model.Fact, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+factCols+`, ts_rank(f.tsv, plainto_tsquery('simple', $2)) AS score
		FROM facts f JOIN cards c ON c.id=f.card_id
		WHERE f.project_id=$1 AND f.status='confirmed' AND f.tsv @@ plainto_tsquery('simple', $2)
		ORDER BY score DESC`, projectID, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFactsScored(rows)
}

func (s *Store) FactsByGroups(ctx context.Context, projectID uuid.UUID, groups []string) ([]model.Fact, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, factSelect+`
		WHERE f.project_id=$1 AND f.status='confirmed' AND f.groups && $2::text[]`,
		projectID, groups)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

func (s *Store) FactsByCardIDs(ctx context.Context, projectID uuid.UUID, cardIDs []uuid.UUID) ([]model.Fact, error) {
	if len(cardIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, factSelect+`
		WHERE f.project_id=$1 AND f.status='confirmed' AND f.card_id = ANY($2)`,
		projectID, cardIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

func (s *Store) ReadAudit(ctx context.Context, projectID uuid.UUID, actor, kind string, since *time.Time) ([]model.AuditEntry, error) {
	q := `SELECT id, project_id, ts, actor, kind, object, details FROM audit WHERE project_id=$1`
	args := []any{projectID}
	n := 2
	if actor != "" {
		q += fmt.Sprintf(` AND actor=$%d`, n)
		args = append(args, actor)
		n++
	}
	if kind != "" {
		q += fmt.Sprintf(` AND kind=$%d`, n)
		args = append(args, kind)
		n++
	}
	if since != nil {
		q += fmt.Sprintf(` AND ts>=$%d`, n)
		args = append(args, *since)
	}
	q += ` ORDER BY ts DESC LIMIT 500`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var details []byte
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Ts, &e.Actor, &e.Kind, &e.Object, &details); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(details, &e.Details)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ImportPortrait loads a full portrait file in one transaction (FR-023).
func (s *Store) ImportPortrait(ctx context.Context, projectID uuid.UUID, doc *model.PortraitFile) (cards, facts int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// groups
	for _, g := range doc.Groups {
		if _, err := tx.Exec(ctx, `INSERT INTO groups (project_id, name) VALUES ($1,$2) ON CONFLICT DO NOTHING`, projectID, g); err != nil {
			return 0, 0, err
		}
	}
	// roles from project.roles
	for name, members := range doc.Project.Roles {
		if members == nil {
			members = []string{}
		}
		// principals may not exist yet — insert roles after principals
		_ = name
		_ = members
	}
	// cards first (principals may reference cards)
	for _, pc := range doc.Cards {
		aliases := pc.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		def, _ := json.Marshal(pc.Defaults)
		if pc.Defaults == nil {
			def = []byte("{}")
		}
		ext, _ := json.Marshal(pc.Extensions)
		if pc.Extensions == nil {
			ext = []byte("{}")
		}
		var cardID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO cards (project_id, slug, type, name, aliases, defaults, extensions)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (project_id, slug) DO UPDATE SET name=EXCLUDED.name, type=EXCLUDED.type,
			  aliases=EXCLUDED.aliases, defaults=EXCLUDED.defaults, extensions=EXCLUDED.extensions, updated_at=now()
			RETURNING id`,
			projectID, pc.Slug, pc.Type, pc.Name, aliases, def, ext,
		).Scan(&cardID)
		if err != nil {
			return 0, 0, fmt.Errorf("card %s: %w", pc.Slug, err)
		}
		cards++
		for _, pf := range pc.Facts {
			f, err := portraitFactToModel(projectID, cardID, pf)
			if err != nil {
				return 0, 0, err
			}
			if err := insertFactTx(ctx, tx, f); err != nil {
				return 0, 0, fmt.Errorf("fact %s: %w", pf.ID, err)
			}
			facts++
		}
	}
	// principals
	for _, pp := range doc.Principals {
		attrs, _ := json.Marshal(pp.Attributes)
		if pp.Attributes == nil {
			attrs = []byte("{}")
		}
		var card *string
		if pp.Card != "" {
			card = &pp.Card
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO principals (project_id, slug, attributes, card_slug)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (project_id, slug) DO UPDATE SET attributes=EXCLUDED.attributes, card_slug=EXCLUDED.card_slug`,
			projectID, pp.Slug, attrs, card); err != nil {
			return 0, 0, fmt.Errorf("principal %s: %w", pp.Slug, err)
		}
	}
	// roles (after principals)
	for name, members := range doc.Project.Roles {
		if members == nil {
			members = []string{}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO roles (project_id, name, members) VALUES ($1,$2,$3)
			ON CONFLICT (project_id, name) DO UPDATE SET members=EXCLUDED.members`,
			projectID, name, members); err != nil {
			return 0, 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return cards, facts, nil
}

// ExportPortrait rebuilds a portrait-card JSON document (FR-021).
func (s *Store) ExportPortrait(ctx context.Context, p *model.Project) (*model.PortraitFile, error) {
	doc := &model.PortraitFile{
		Spec:        "portrait-card",
		SpecVersion: "0.1.0-draft",
		Project: model.PortraitProject{
			Slug: p.Slug,
			Name: p.Name,
			Roles: map[string][]string{},
		},
		Extensions: map[string]any{},
	}
	groups, err := s.ListGroups(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	doc.Groups = groups
	roles, err := s.ListRoles(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		doc.Project.Roles[r.Name] = r.Members
	}
	princs, err := s.ListPrincipals(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, pr := range princs {
		pp := model.PortraitPrinc{Slug: pr.Slug, Attributes: pr.Attributes, Extensions: map[string]any{}}
		if pr.CardSlug != nil {
			pp.Card = *pr.CardSlug
		}
		doc.Principals = append(doc.Principals, pp)
	}
	cards, err := s.ListCards(ctx, p.ID, "")
	if err != nil {
		return nil, err
	}
	for _, c := range cards {
		pc := model.PortraitCard{
			Slug: c.Slug, Type: c.Type, Name: c.Name, Aliases: c.Aliases,
			Defaults: c.Defaults, Extensions: c.Extensions,
		}
		facts, err := s.ListFacts(ctx, p.ID, c.Slug, "")
		if err != nil {
			return nil, err
		}
		for _, f := range facts {
			if f.Status == "deleted" {
				continue
			}
			pc.Facts = append(pc.Facts, modelFactToPortrait(f))
		}
		doc.Cards = append(doc.Cards, pc)
	}
	return doc, nil
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")
}

func tokenize(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '\'' || r == '-')
	})
	var out []string
	stop := map[string]bool{"what": true, "should": true, "i": true, "get": true, "for": true, "the": true, "a": true, "an": true, "is": true, "how": true, "doing": true, "about": true, "to": true, "of": true, "and": true, "or": true, "my": true}
	for _, f := range fields {
		if len(f) < 2 || stop[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func normalizeFactSlices(f *model.Fact) {
	if f.Groups == nil {
		f.Groups = []string{}
	}
	if f.Core == nil {
		f.Core = []string{}
	}
	if f.Keys == nil {
		f.Keys = []string{}
	}
	if f.Interpretation == nil {
		f.Interpretation = []model.InterpretationRule{}
	}
	if f.Extensions == nil {
		f.Extensions = map[string]any{}
	}
}

func marshalNullable(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
