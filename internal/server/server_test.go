package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(New(nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field: got %q, want ok", body["status"])
	}
	if body["db"] != "not configured" {
		t.Fatalf("db field: got %q, want \"not configured\"", body["db"])
	}
}

func TestFR022_OpenAPIAndDocs(t *testing.T) {
	srv := httptest.NewServer(New(nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("openapi.json: %d", resp.StatusCode)
	}
	var spec map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		t.Fatal(err)
	}
	if spec["openapi"] == nil {
		t.Fatal("missing openapi field")
	}

	resp, err = srv.Client().Get(srv.URL + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("openapi.yaml: %d", resp.StatusCode)
	}

	resp, err = srv.Client().Get(srv.URL + "/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("docs: %d", resp.StatusCode)
	}
}
