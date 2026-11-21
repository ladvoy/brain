package model

import (
	"time"

	"github.com/google/uuid"
)

// IngestJob tracks a document → pending facts run (002-ingestion).
type IngestJob struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	CardSlug   string
	Filename   string
	ContentSHA []byte
	Status     string
	Summary    map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
