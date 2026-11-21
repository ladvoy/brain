package contract_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/ladvoy/brain/internal/testharness"
)

// paths that must respond (not 404) once a project exists — T007.
var protectedPaths = []struct {
	method string
	path   string
}{
	{"GET", "/v1/projects/{id}"},
	{"PATCH", "/v1/projects/{id}"},
	{"DELETE", "/v1/projects/{id}"},
	{"POST", "/v1/projects/{id}/import"},
	{"GET", "/v1/projects/{id}/export"},
	{"GET", "/v1/projects/{id}/principals"},
	{"POST", "/v1/projects/{id}/principals"},
	{"GET", "/v1/projects/{id}/roles"},
	{"POST", "/v1/projects/{id}/roles"},
	{"GET", "/v1/projects/{id}/groups"},
	{"POST", "/v1/projects/{id}/groups"},
	{"GET", "/v1/projects/{id}/cards"},
	{"POST", "/v1/projects/{id}/cards"},
	{"GET", "/v1/projects/{id}/cards/x"},
	{"PATCH", "/v1/projects/{id}/cards/x"},
	{"DELETE", "/v1/projects/{id}/cards/x"},
	{"GET", "/v1/projects/{id}/cards/x/facts"},
	{"POST", "/v1/projects/{id}/cards/x/facts"},
	{"GET", "/v1/projects/{id}/facts/f_x"},
	{"PATCH", "/v1/projects/{id}/facts/f_x"},
	{"DELETE", "/v1/projects/{id}/facts/f_x"},
	{"GET", "/v1/projects/{id}/proposals"},
	{"POST", "/v1/projects/{id}/proposals/f_x/confirm"},
	{"POST", "/v1/projects/{id}/proposals/f_x/reject"},
	{"GET", "/v1/projects/{id}/audit"},
	{"POST", "/v1/projects/{id}/context"},
}

func TestFR001_ContractPathsAndFailedAuth(t *testing.T) {
	env := testharness.StartParadeDB(t)

	// create project (no auth)
	resp := postJSON(t, env, "/v1/projects", "", map[string]string{"slug": "c1", "name": "C1"})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, readBody(resp))
	}
	var tokens map[string]string
	decode(t, resp, &tokens)

	// all protected paths without token → 401
	for _, p := range protectedPaths {
		path := replaceID(p.path, "c1")
		req, _ := http.NewRequest(p.method, env.URL(path), bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if p.path == "/v1/projects/{id}/context" {
			req.Header.Set("X-Brain-Principal", "anyone")
		}
		r, err := env.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", p.method, path, err)
		}
		body := readBody(r)
		if r.StatusCode != 401 {
			t.Fatalf("%s %s: want 401, got %d (%s)", p.method, path, r.StatusCode, body)
		}
		r.Body.Close()
	}

	// failed_auth audit entries present
	req, _ := http.NewRequest("GET", env.URL("/v1/projects/c1/audit?kind=failed_auth"), nil)
	req.Header.Set("Authorization", "Bearer "+tokens["admin_token"])
	r, err := env.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var audit []map[string]any
	decode(t, r, &audit)
	if len(audit) < len(protectedPaths) {
		t.Fatalf("failed_auth audit entries: got %d, want >= %d", len(audit), len(protectedPaths))
	}
}

func replaceID(path, id string) string {
	return string(bytes.ReplaceAll([]byte(path), []byte("{id}"), []byte(id)))
}

func postJSON(t *testing.T, env *testharness.Env, path, token string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", env.URL(path), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := env.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return string(b)
}
