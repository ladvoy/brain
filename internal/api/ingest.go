package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ladvoy/brain/internal/api/gen"
	"github.com/ladvoy/brain/internal/store"
)

func (a *API) CreateIngestJob(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	if a.Ingest == nil {
		writeErr(w, http.StatusServiceUnavailable, "ingest not configured")
		return
	}
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(int64(a.Ingest.Config.MaxBytes) + (1 << 20)); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "multipart required: "+err.Error())
		return
	}
	card := r.FormValue("card")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "file required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(a.Ingest.Config.MaxBytes)+1))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	filename := ""
	if hdr != nil {
		filename = hdr.Filename
	}
	job, err := a.Ingest.Enqueue(r.Context(), p.ID, card, filename, data)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrValidation) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": job.ID.String(), "card": job.CardSlug, "filename": job.Filename,
		"status": job.Status, "summary": job.Summary,
	})
}

func (a *API) GetIngestJob(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, jobId openapi_types.UUID) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	id := uuid.UUID(jobId)
	job, err := a.Store.GetIngestJob(r.Context(), p.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "job not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": job.ID.String(), "card": job.CardSlug, "filename": job.Filename,
		"status": job.Status, "summary": job.Summary,
	})
}
