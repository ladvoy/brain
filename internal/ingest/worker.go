package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/ladvoy/brain/internal/model"
	"github.com/ladvoy/brain/internal/store"
)

// Service runs ingest jobs against the store.
type Service struct {
	Store  *store.Store
	Config Config
}

type Result struct {
	Job *model.IngestJob
}

// Enqueue creates or returns an idempotent job and processes it synchronously (v0).
func (s *Service) Enqueue(ctx context.Context, projectID uuid.UUID, cardSlug, filename string, data []byte) (*model.IngestJob, error) {
	if cardSlug == "" {
		return nil, fmt.Errorf("%w: card required", store.ErrValidation)
	}
	if _, err := s.Store.GetCard(ctx, projectID, cardSlug); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: unknown card %q", store.ErrValidation, cardSlug)
		}
		return nil, err
	}
	sum := sha256.Sum256(data)
	job, created, err := s.Store.CreateIngestJob(ctx, projectID, cardSlug, filename, sum[:])
	if err != nil {
		return nil, err
	}
	if !created {
		return job, nil // idempotent hit (FR-106)
	}
	return s.run(ctx, job, data)
}

func (s *Service) run(ctx context.Context, job *model.IngestJob, data []byte) (*model.IngestJob, error) {
	_ = s.Store.UpdateIngestJob(ctx, job.ID, "running", map[string]any{})
	text, err := ExtractText(data, job.Filename, s.Config.MaxBytes, s.Config.OCR)
	if err != nil {
		reason := err.Error()
		switch {
		case errors.Is(err, ErrEmptyText):
			reason = "empty_text"
		case errors.Is(err, ErrNeedsOCR):
			reason = "needs_ocr"
		case errors.Is(err, ErrTooLarge):
			reason = "too_large"
		case errors.Is(err, ErrUnsupported):
			reason = "unsupported_type"
		}
		sum := map[string]any{"error": err.Error(), "reason": reason}
		_ = s.Store.UpdateIngestJob(ctx, job.ID, "failed", sum)
		job.Status = "failed"
		job.Summary = sum
		return job, nil
	}

	chunks := Chunk(text, s.Config.ChunkChars)
	card, err := s.Store.GetCard(ctx, job.ProjectID, job.CardSlug)
	if err != nil {
		return nil, err
	}

	createdN, skipped := 0, 0
	system := "system"
	for i, ch := range chunks {
		ref := fmt.Sprintf("ingest:%s#%d", job.ID.String(), i)
		f := &model.Fact{
			ProjectID: job.ProjectID,
			CardID:    card.ID,
			CardSlug:  card.Slug,
			FactRef:   fmt.Sprintf("f_ingest_%s_%d", strings.ReplaceAll(job.ID.String()[:8], "-", ""), i),
			Text:      ch,
			Status:    "pending",
			Source:    model.Source{Type: "document", Ref: ref},
		}
		err := s.Store.CreateFact(ctx, f)
		if err != nil {
			if errors.Is(err, store.ErrDuplicateReject) {
				skipped++
				continue
			}
			sum := map[string]any{"chunks": len(chunks), "created": createdN, "skipped": skipped, "error": err.Error()}
			_ = s.Store.UpdateIngestJob(ctx, job.ID, "failed", sum)
			job.Status = "failed"
			job.Summary = sum
			return job, nil
		}
		createdN++
		_ = s.Store.WriteAudit(ctx, &job.ProjectID, &system, "mutation", &f.FactRef, map[string]any{
			"op": "ingest_fact", "job_id": job.ID.String(), "after": f.Text,
		})
	}

	sum := map[string]any{"chunks": len(chunks), "created": createdN, "skipped": skipped}
	_ = s.Store.UpdateIngestJob(ctx, job.ID, "completed", sum)
	job.Status = "completed"
	job.Summary = sum
	return job, nil
}
