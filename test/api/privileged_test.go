package api_test

import (
	"testing"

	"github.com/ladvoy/brain/internal/testharness"
)

func TestFR001_ProjectTokensHashedOnce(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "p1")
	if tok.AdminToken == "" || tok.ContextToken == "" {
		t.Fatal("tokens missing")
	}
	// get with admin works
	resp := doJSON(t, env, "GET", "/v1/projects/p1", tok.AdminToken, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("get: %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()
	// context token cannot access admin
	resp = doJSON(t, env, "GET", "/v1/projects/p1", tok.ContextToken, nil, nil)
	if resp.StatusCode != 401 {
		t.Fatalf("context on admin: want 401 got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestFR005_009_CardsPrincipalsGroups(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "p2")

	// card without name → 422
	resp := doJSON(t, env, "POST", "/v1/projects/p2/cards", tok.AdminToken, nil, map[string]any{
		"slug": "a", "type": "person",
	})
	if resp.StatusCode != 422 {
		t.Fatalf("missing name: want 422 got %d %s", resp.StatusCode, body(resp))
	}

	resp = doJSON(t, env, "POST", "/v1/projects/p2/cards", tok.AdminToken, nil, map[string]any{
		"slug": "alice", "type": "person", "name": "Alice",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("create card: %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()

	resp = doJSON(t, env, "POST", "/v1/projects/p2/groups", tok.AdminToken, nil, map[string]any{"name": "health"})
	if resp.StatusCode != 201 {
		t.Fatalf("group: %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()

	resp = doJSON(t, env, "POST", "/v1/projects/p2/principals", tok.AdminToken, nil, map[string]any{
		"slug": "alice", "card": "alice",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("principal: %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()

	// unknown group on fact → 422
	resp = doJSON(t, env, "POST", "/v1/projects/p2/cards/alice/facts", tok.AdminToken, nil, map[string]any{
		"id": "f1", "text": "hi", "status": "confirmed",
		"source": map[string]string{"type": "note", "ref": "t"},
		"groups": []string{"nope"},
	})
	if resp.StatusCode != 422 {
		t.Fatalf("unknown group: want 422 got %d %s", resp.StatusCode, body(resp))
	}
}

func TestFR006_007_008_010_FactLifecycle(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "p3")
	_ = doJSON(t, env, "POST", "/v1/projects/p3/cards", tok.AdminToken, nil, map[string]any{
		"slug": "c", "type": "person", "name": "C",
	}).Body.Close()
	_ = doJSON(t, env, "POST", "/v1/projects/p3/groups", tok.AdminToken, nil, map[string]any{"name": "g"}).Body.Close()

	// no source → pending
	resp := doJSON(t, env, "POST", "/v1/projects/p3/cards/c/facts", tok.AdminToken, nil, map[string]any{
		"id": "f_pend", "text": "candidate thought",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("create pending: %d %s", resp.StatusCode, body(resp))
	}
	var fact map[string]any
	mustDecode(t, resp, &fact)
	if fact["status"] != "pending" {
		t.Fatalf("status: %v", fact["status"])
	}

	resp = doJSON(t, env, "POST", "/v1/projects/p3/proposals/f_pend/confirm", tok.AdminToken, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("confirm: %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()

	// reject another
	resp = doJSON(t, env, "POST", "/v1/projects/p3/cards/c/facts", tok.AdminToken, nil, map[string]any{
		"id": "f_rej", "text": "bad idea",
	})
	mustDecode(t, resp, &fact)
	_ = doJSON(t, env, "POST", "/v1/projects/p3/proposals/f_rej/reject", tok.AdminToken, nil, nil).Body.Close()

	// duplicate of rejected → 422 report
	resp = doJSON(t, env, "POST", "/v1/projects/p3/cards/c/facts", tok.AdminToken, nil, map[string]any{
		"id": "f_dup", "text": "bad idea",
	})
	if resp.StatusCode != 422 {
		t.Fatalf("dup reject: want 422 got %d %s", resp.StatusCode, body(resp))
	}
	_ = resp.Body.Close()

	// soft delete
	resp = doJSON(t, env, "DELETE", "/v1/projects/p3/facts/f_pend", tok.AdminToken, nil, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete: %d %s", resp.StatusCode, body(resp))
	}
	resp = doJSON(t, env, "GET", "/v1/projects/p3/facts/f_pend", tok.AdminToken, nil, nil)
	mustDecode(t, resp, &fact)
	if fact["status"] != "deleted" {
		t.Fatalf("soft delete status: %v", fact["status"])
	}
}

func TestFR003_021_023_014_AuditImportExportDelete(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "carter")
	importCarter(t, env, tok)

	resp := doJSON(t, env, "GET", "/v1/projects/carter/export", tok.AdminToken, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("export: %d %s", resp.StatusCode, body(resp))
	}
	var doc map[string]any
	mustDecode(t, resp, &doc)
	if doc["spec"] != "portrait-card" {
		t.Fatalf("spec: %v", doc["spec"])
	}
	cards, _ := doc["cards"].([]any)
	if len(cards) < 5 {
		t.Fatalf("cards: %d", len(cards))
	}

	resp = doJSON(t, env, "GET", "/v1/projects/carter/audit", tok.AdminToken, nil, nil)
	var audit []map[string]any
	mustDecode(t, resp, &audit)
	if len(audit) == 0 {
		t.Fatal("expected audit entries after import")
	}

	resp = doJSON(t, env, "DELETE", "/v1/projects/carter", tok.AdminToken, nil, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete project: %d %s", resp.StatusCode, body(resp))
	}
	// project gone
	resp = doJSON(t, env, "GET", "/v1/projects/carter", tok.AdminToken, nil, nil)
	if resp.StatusCode != 401 && resp.StatusCode != 404 {
		t.Fatalf("after delete: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// audit retained (query by recreating? audit has no FK — check via SQL)
	var n int
	err := env.Pool.QueryRow(t.Context(), `SELECT count(*) FROM audit WHERE kind='delete'`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("audit delete row missing after project cascade")
	}
}
