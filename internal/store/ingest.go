package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ladvoy/brain/internal/model"
)

// CreateIngestJob inserts a queued job. created=false means idempotent replay (FR-106).
func (s *Store) CreateIngestJob(ctx context.Context, projectID uuid.UUID, cardSlug, filename string, sha []byte) (*model.IngestJob, bool, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ingest_jobs (project_id, card_slug, filename, content_sha, status, summary)
		VALUES ($1,$2,$3,$4,'queued','{}')
		RETURNING id, project_id, card_slug, coalesce(filename,''), content_sha, status, summary, created_at, updated_at`,
		projectID, cardSlug, filename, sha)
	j, err := scanIngestJob(row)
	if err == nil {
		return j, true, nil
	}
	if !isUnique(err) {
		return nil, false, err
	}
	// fetch existing
	existing, err := s.GetIngestJobBySHA(ctx, projectID, cardSlug, sha)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (s *Store) GetIngestJobBySHA(ctx context.Context, projectID uuid.UUID, cardSlug string, sha []byte) (*model.IngestJob, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, card_slug, coalesce(filename,''), content_sha, status, summary, created_at, updated_at
		FROM ingest_jobs WHERE project_id=$1 AND card_slug=$2 AND content_sha=$3`,
		projectID, cardSlug, sha)
	return scanIngestJob(row)
}

func (s *Store) GetIngestJob(ctx context.Context, projectID, jobID uuid.UUID) (*model.IngestJob, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, project_id, card_slug, coalesce(filename,''), content_sha, status, summary, created_at, updated_at
		FROM ingest_jobs WHERE project_id=$1 AND id=$2`, projectID, jobID)
	j, err := scanIngestJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

func (s *Store) UpdateIngestJob(ctx context.Context, id uuid.UUID, status string, summary map[string]any) error {
	if summary == nil {
		summary = map[string]any{}
	}
	b, _ := json.Marshal(summary)
	_, err := s.pool.Exec(ctx, `
		UPDATE ingest_jobs SET status=$2, summary=$3, updated_at=now() WHERE id=$1`, id, status, b)
	return err
}

func scanIngestJob(row scannable) (*model.IngestJob, error) {
	var j model.IngestJob
	var summary []byte
	err := row.Scan(&j.ID, &j.ProjectID, &j.CardSlug, &j.Filename, &j.ContentSHA, &j.Status, &summary, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(summary, &j.Summary)
	if j.Summary == nil {
		j.Summary = map[string]any{}
	}
	return &j, nil
}
