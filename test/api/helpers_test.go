package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ladvoy/brain/internal/testharness"
)

type tokens struct {
	Slug         string `json:"slug"`
	AdminToken   string `json:"admin_token"`
	ContextToken string `json:"context_token"`
}

func createProject(t *testing.T, env *testharness.Env, slug string) tokens {
	t.Helper()
	resp := doJSON(t, env, "POST", "/v1/projects", "", nil, map[string]string{"slug": slug, "name": slug})
	if resp.StatusCode != 201 {
		t.Fatalf("create project: %d %s", resp.StatusCode, body(resp))
	}
	var tok tokens
	mustDecode(t, resp, &tok)
	return tok
}

func importCarter(t *testing.T, env *testharness.Env, tok tokens) {
	t.Helper()
	raw, err := os.ReadFile(carterPath(t))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", env.URL("/v1/projects/"+tok.Slug+"/import"), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok.AdminToken)
	resp, err := env.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("import: %d %s", resp.StatusCode, body(resp))
	}
}

func carterPath(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "carter.json"))
}

func schemaPath(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "project.schema.json"))
}

func doJSON(t *testing.T, env *testharness.Env, method, path, token string, headers map[string]string, payload any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, env.URL(path), rdr)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := env.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustDecode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func body(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return string(b)
}

func contextAsk(t *testing.T, env *testharness.Env, tok tokens, principal, query string, extra map[string]any) map[string]any {
	t.Helper()
	payload := map[string]any{"query": query}
	if _, ok := extra["budget_tokens"]; !ok {
		payload["budget_tokens"] = 2000
	}
	for k, v := range extra {
		payload[k] = v
	}
	resp := doJSON(t, env, "POST", "/v1/projects/"+tok.Slug+"/context", tok.ContextToken,
		map[string]string{"X-Brain-Principal": principal}, payload)
	if resp.StatusCode != 200 {
		t.Fatalf("context: %d %s", resp.StatusCode, body(resp))
	}
	var out map[string]any
	mustDecode(t, resp, &out)
	return out
}

func factIDs(block map[string]any) map[string]string {
	out := map[string]string{}
	facts, _ := block["facts"].([]any)
	for _, f := range facts {
		m := f.(map[string]any)
		out[m["id"].(string)] = m["text"].(string)
	}
	return out
}

func excludedReasons(block map[string]any) map[string]string {
	out := map[string]string{}
	audit := block["audit"].(map[string]any)
	ex, _ := audit["excluded"].([]any)
	for _, e := range ex {
		m := e.(map[string]any)
		out[m["fact"].(string)] = m["reason"].(string)
	}
	return out
}
