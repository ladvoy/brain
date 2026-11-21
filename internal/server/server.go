// Package server wires the brain's HTTP surface.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New returns the brain's HTTP handler. A nil pool means "no database configured"
// (used in smoke tests): health then reports db as "not configured".
func New(pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		db := "not configured"
		if pool != nil {
			db = "ok"
			if err := pool.Ping(req.Context()); err != nil {
				db = "err"
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": db})
	})

	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
