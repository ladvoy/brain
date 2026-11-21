package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ladvoy/brain/internal/testharness"
)

func TestFR101_110_IngestTextPendingIdempotent(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "ing")
	_ = doJSON(t, env, "POST", "/v1/projects/ing/groups", tok.AdminToken, nil, map[string]any{"name": "hobbies"}).Body.Close()
	_ = doJSON(t, env, "POST", "/v1/projects/ing/cards", tok.AdminToken, nil, map[string]any{
		"slug": "fred", "type": "person", "name": "Fred",
	}).Body.Close()

	sample := readIngestSample(t)
	job1 := postIngest(t, env, tok.AdminToken, "ing", "fred", "sample.txt", sample)
	if job1["status"] != "completed" {
		t.Fatalf("job1 status=%v summary=%v", job1["status"], job1["summary"])
	}
	sum := job1["summary"].(map[string]any)
	created := int(sum["created"].(float64))
	if created < 1 {
		t.Fatalf("expected created facts, got %v", sum)
	}

	// proposals list
	resp := doJSON(t, env, "GET", "/v1/projects/ing/proposals", tok.AdminToken, nil, nil)
	var proposals []map[string]any
	mustDecode(t, resp, &proposals)
	if len(proposals) < created {
		t.Fatalf("proposals=%d want >= %d", len(proposals), created)
	}
	for _, p := range proposals {
		if p["status"] != "pending" {
			t.Fatalf("status %v", p["status"])
		}
	}

	// idempotent re-ingest
	job2 := postIngest(t, env, tok.AdminToken, "ing", "fred", "sample.txt", sample)
	if job2["id"] != job1["id"] {
		t.Fatalf("idempotency: got new job %v want %v", job2["id"], job1["id"])
	}

	// GET job
	resp = doJSON(t, env, "GET", "/v1/projects/ing/ingest/"+job1["id"].(string), tok.AdminToken, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("get job: %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()

	// audit has system
	resp = doJSON(t, env, "GET", "/v1/projects/ing/audit?actor=system", tok.AdminToken, nil, nil)
	var audit []map[string]any
	mustDecode(t, resp, &audit)
	if len(audit) < 1 {
		t.Fatal("expected system audit entries")
	}
}

func postIngest(t *testing.T, env *testharness.Env, token, project, card, filename string, data []byte) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("card", card)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	req, _ := http.NewRequest("POST", env.URL("/v1/projects/"+project+"/ingest"), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := env.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 202 {
		t.Fatalf("ingest: %d %s", resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func readIngestSample(t *testing.T) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "ingest", "sample.txt"))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
