package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ladvoy/brain/internal/model"
)

const factCols = `
  f.id, f.project_id, f.card_id, c.slug, f.fact_ref, f.text, f.author, f.status, f.source,
  f.time_from, f.time_to, f.revalidate, f.groups, f.visibility, f.core, f.keys,
  f.interpretation, f.disclosure, f.extensions, f.created_at, f.updated_at`

const factSelect = `SELECT ` + factCols + ` FROM facts f JOIN cards c ON c.id=f.card_id`

type scannable interface {
	Scan(dest ...any) error
}

func scanCard(row scannable) (*model.Card, error) {
	var c model.Card
	var def, ext []byte
	err := row.Scan(&c.ID, &c.ProjectID, &c.Slug, &c.Type, &c.Name, &c.Aliases, &def, &ext, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(def, &c.Defaults)
	_ = json.Unmarshal(ext, &c.Extensions)
	if c.Aliases == nil {
		c.Aliases = []string{}
	}
	if c.Defaults == nil {
		c.Defaults = map[string]any{}
	}
	if c.Extensions == nil {
		c.Extensions = map[string]any{}
	}
	return &c, nil
}

func scanCards(rows pgx.Rows) ([]model.Card, error) {
	var out []model.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func scanFact(row scannable) (*model.Fact, error) {
	var f model.Fact
	var src, vis, interp, disc, ext []byte
	err := row.Scan(
		&f.ID, &f.ProjectID, &f.CardID, &f.CardSlug, &f.FactRef, &f.Text, &f.Author, &f.Status, &src,
		&f.TimeFrom, &f.TimeTo, &f.Revalidate, &f.Groups, &vis, &f.Core, &f.Keys,
		&interp, &disc, &ext, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(src, &f.Source)
	if isJSONObject(vis) {
		var v model.Visibility
		if json.Unmarshal(vis, &v) == nil && v.Mode != "" {
			f.Visibility = &v
		}
	}
	_ = json.Unmarshal(interp, &f.Interpretation)
	if isJSONObject(disc) {
		var d model.DisclosureRule
		if json.Unmarshal(disc, &d) == nil && d.Until != "" {
			f.Disclosure = &d
		}
	}
	_ = json.Unmarshal(ext, &f.Extensions)
	normalizeFactSlices(&f)
	return &f, nil
}

func scanFacts(rows pgx.Rows) ([]model.Fact, error) {
	var out []model.Fact
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func scanFactsScored(rows pgx.Rows) ([]model.Fact, error) {
	var out []model.Fact
	for rows.Next() {
		var f model.Fact
		var src, vis, interp, disc, ext []byte
		var score float64
		err := rows.Scan(
			&f.ID, &f.ProjectID, &f.CardID, &f.CardSlug, &f.FactRef, &f.Text, &f.Author, &f.Status, &src,
			&f.TimeFrom, &f.TimeTo, &f.Revalidate, &f.Groups, &vis, &f.Core, &f.Keys,
			&interp, &disc, &ext, &f.CreatedAt, &f.UpdatedAt, &score,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(src, &f.Source)
		if isJSONObject(vis) {
			var v model.Visibility
			if json.Unmarshal(vis, &v) == nil && v.Mode != "" {
				f.Visibility = &v
			}
		}
		_ = json.Unmarshal(interp, &f.Interpretation)
		if isJSONObject(disc) {
			var d model.DisclosureRule
			if json.Unmarshal(disc, &d) == nil && d.Until != "" {
				f.Disclosure = &d
			}
		}
		_ = json.Unmarshal(ext, &f.Extensions)
		normalizeFactSlices(&f)
		f.BM25Score = score
		out = append(out, f)
	}
	return out, rows.Err()
}

func portraitFactToModel(projectID, cardID uuid.UUID, pf model.PortraitFact) (*model.Fact, error) {
	f := &model.Fact{
		ProjectID:      projectID,
		CardID:         cardID,
		FactRef:        pf.ID,
		Text:           pf.Text,
		Status:         pf.Status,
		Source:         pf.Source,
		Groups:         pf.Groups,
		Visibility:     pf.Visibility,
		Core:           pf.Core,
		Keys:           pf.Keys,
		Interpretation: pf.Interpretation,
		Disclosure:     pf.Disclosure,
		Extensions:     pf.Extensions,
	}
	if pf.Author != "" {
		a := pf.Author
		f.Author = &a
	}
	if pf.Revalidate != "" {
		r := pf.Revalidate
		f.Revalidate = &r
	}
	if pf.Time != nil {
		if pf.Time.From != "" {
			t, err := time.Parse("2006-01-02", pf.Time.From)
			if err != nil {
				return nil, err
			}
			f.TimeFrom = &t
		}
		if pf.Time.To != "" {
			t, err := time.Parse("2006-01-02", pf.Time.To)
			if err != nil {
				return nil, err
			}
			f.TimeTo = &t
		}
	}
	normalizeFactSlices(f)
	if f.Status == "" {
		f.Status = "confirmed"
	}
	return f, nil
}

func insertFactTx(ctx context.Context, tx pgx.Tx, f *model.Fact) error {
	normalizeFactSlices(f)
	src, _ := json.Marshal(f.Source)
	vis, _ := marshalNullable(f.Visibility)
	interp, _ := json.Marshal(f.Interpretation)
	disc, _ := marshalNullable(f.Disclosure)
	ext, _ := json.Marshal(f.Extensions)
	_, err := tx.Exec(ctx, `
		INSERT INTO facts (
		  project_id, card_id, fact_ref, text, author, status, source,
		  time_from, time_to, revalidate, groups, visibility, core, keys,
		  interpretation, disclosure, extensions
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (project_id, fact_ref) DO UPDATE SET
		  text=EXCLUDED.text, author=EXCLUDED.author, status=EXCLUDED.status, source=EXCLUDED.source,
		  time_from=EXCLUDED.time_from, time_to=EXCLUDED.time_to, revalidate=EXCLUDED.revalidate,
		  groups=EXCLUDED.groups, visibility=EXCLUDED.visibility, core=EXCLUDED.core, keys=EXCLUDED.keys,
		  interpretation=EXCLUDED.interpretation, disclosure=EXCLUDED.disclosure, extensions=EXCLUDED.extensions,
		  updated_at=now()`,
		f.ProjectID, f.CardID, f.FactRef, f.Text, f.Author, f.Status, src,
		f.TimeFrom, f.TimeTo, f.Revalidate, f.Groups, vis, f.Core, f.Keys,
		interp, disc, ext)
	return err
}

func isJSONObject(b []byte) bool {
	s := strings.TrimSpace(string(b))
	return len(s) > 0 && s[0] == '{' && s != "null"
}

func modelFactToPortrait(f model.Fact) model.PortraitFact {
	pf := model.PortraitFact{
		ID: f.FactRef, Text: f.Text, Status: f.Status, Source: f.Source,
		Groups: f.Groups, Visibility: f.Visibility, Core: f.Core, Keys: f.Keys,
		Interpretation: f.Interpretation, Disclosure: f.Disclosure,
	}
	if len(f.Extensions) > 0 {
		pf.Extensions = f.Extensions
	}
	if f.Author != nil {
		pf.Author = *f.Author
	}
	if f.Revalidate != nil {
		pf.Revalidate = *f.Revalidate
	}
	if f.TimeFrom != nil || f.TimeTo != nil {
		pf.Time = &model.PortraitTime{}
		if f.TimeFrom != nil {
			pf.Time.From = f.TimeFrom.Format("2006-01-02")
		}
		if f.TimeTo != nil {
			pf.Time.To = f.TimeTo.Format("2006-01-02")
		}
	}
	return pf
}
