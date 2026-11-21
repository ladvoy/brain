// Package server wires the brain's HTTP surface.
package server

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ladvoy/brain/internal/api"
	"github.com/ladvoy/brain/internal/api/gen"
	"github.com/ladvoy/brain/internal/assembly"
	"github.com/ladvoy/brain/internal/store"
)

//go:embed openapi.yaml
var openapiFS embed.FS

// New returns the brain's HTTP handler. A nil pool means "no database configured"
// (used in smoke tests): health then reports db as "not configured" and API routes
// are omitted.
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

	// FR-022: OpenAPI + Swagger UI
	r.Get("/openapi.json", serveOpenAPIJSON)
	r.Get("/openapi.yaml", serveOpenAPIYAML)
	r.Get("/docs", serveSwaggerUI)
	r.Get("/docs/", serveSwaggerUI)

	if pool != nil {
		st := store.New(pool)
		asm := assembly.New(st)
		h := &api.API{Store: st, Asm: asm}
		gen.HandlerFromMuxWithBaseURL(h, r, "/v1")
	}

	return r
}

func serveOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	spec, err := gen.GetSpec()
	if err != nil {
		http.Error(w, "openapi unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(spec)
}

func serveOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	data, err := openapiFS.ReadFile("openapi.yaml")
	if err != nil {
		http.Error(w, "openapi.yaml missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerHTML))
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>Brain API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css"/>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script>
  window.ui = SwaggerUIBundle({
    url: "/openapi.json",
    dom_id: "#swagger-ui",
    presets: [SwaggerUIBundle.presets.apis],
    layout: "BaseLayout"
  });
</script>
</body>
</html>
`

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
