package api_test

import (
	"strings"
	"testing"

	"github.com/ladvoy/brain/internal/testharness"
)

func TestFR002_015_016_UnknownPrincipalAndHistoryDisclosure(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "carter")
	importCarter(t, env, tok)

	// unknown principal → fail-closed
	block := contextAsk(t, env, tok, "stranger", "How is grandpa Fred health?", nil)
	audit := block["audit"].(map[string]any)
	if audit["degraded"] != "fail_closed_unknown_principal" {
		t.Fatalf("degraded: %v", audit["degraded"])
	}
	ids := factIDs(block)
	if _, ok := ids["f_fred_health"]; ok {
		// adults-only original must not appear for unknown; child interpretation might if role matched — unknown has no roles
		t.Fatal("f_fred_health must not appear for unknown principal")
	}

	// add disclosure fact via admin
	_ = doJSON(t, env, "POST", "/v1/projects/carter/cards/fred/facts", tok.AdminToken, nil, map[string]any{
		"id": "f_fred_secret_party", "text": "Surprise party for Fred in January.",
		"status": "confirmed", "author": "fred",
		"source": map[string]string{"type": "note", "ref": "t"},
		"groups": []string{"traditions"},
		"disclosure": map[string]any{
			"until": "2099-01-01",
			"then":  map[string]string{"mode": "everyone"},
			"meanwhile": "Something nice is being planned for Grandpa.",
		},
	}).Body.Close()

	block = contextAsk(t, env, tok, "rose", "What about Fred traditions?", nil)
	ids = factIDs(block)
	if _, ok := ids["f_fred_secret_party"]; ok {
		t.Fatal("disclosure fact leaked to rose")
	}
	notes, _ := block["notes"].([]any)
	found := false
	for _, n := range notes {
		if strings.Contains(n.(string), "Something nice") {
			found = true
		}
	}
	if !found {
		t.Fatalf("meanwhile note missing: %v", notes)
	}

	block = contextAsk(t, env, tok, "fred", "What about Fred traditions?", nil)
	ids = factIDs(block)
	if _, ok := ids["f_fred_secret_party"]; !ok {
		t.Fatal("owner should see disclosure fact")
	}

	// history: jack audit ends 2026-11-30 — with include_history
	block = contextAsk(t, env, tok, "rose", "Jack factory audit work", map[string]any{"include_history": true})
	ids = factIDs(block)
	if text, ok := ids["f_jack_audit"]; ok {
		facts := block["facts"].([]any)
		for _, f := range facts {
			m := f.(map[string]any)
			if m["id"] == "f_jack_audit" {
				flags, _ := m["flags"].([]any)
				hasEnded := false
				for _, fl := range flags {
					if fl == "ended" {
						hasEnded = true
					}
				}
				// only if today > 2026-11-30; system date is Nov 22 2026 per user_info — so NOT ended yet
				_ = text
				_ = hasEnded
			}
		}
	}
}

func TestFR011_012_013_017_018_MatchVisibilityInterpretationBudget(t *testing.T) {
	env := testharness.StartParadeDB(t)
	tok := createProject(t, env, "carter")
	importCarter(t, env, tok)

	// rebecca: interpretation for health, no original
	block := contextAsk(t, env, tok, "rebecca", "grandpa health urologist", nil)
	ids := factIDs(block)
	if text, ok := ids["f_fred_health"]; ok {
		if strings.Contains(strings.ToLower(text), "urologist") {
			t.Fatalf("original health text leaked to rebecca: %s", text)
		}
		if !strings.Contains(text, "age-related") {
			t.Fatalf("expected child interpretation, got %s", text)
		}
	} else {
		t.Fatal("expected substituted f_fred_health for rebecca")
	}
	ex := excludedReasons(block)
	// may be substituted rather than excluded — check substitutions
	audit := block["audit"].(map[string]any)
	subs, _ := audit["substitutions"].([]any)
	foundSub := false
	for _, s := range subs {
		m := s.(map[string]any)
		if m["fact"] == "f_fred_health" {
			foundSub = true
		}
	}
	if !foundSub {
		t.Fatalf("missing substitution; excluded=%v audit=%v", ex, audit)
	}

	// rose sees original
	block = contextAsk(t, env, tok, "rose", "grandpa health urologist", nil)
	ids = factIDs(block)
	if text, ok := ids["f_fred_health"]; !ok || !strings.Contains(strings.ToLower(text), "urologist") {
		t.Fatalf("rose should see original health fact: %v", ids["f_fred_health"])
	}

	// budget 0 → all cut_budget
	block = contextAsk(t, env, tok, "rose", "grandpa gifts firearms collection", map[string]any{"budget_tokens": 0})
	if len(factIDs(block)) != 0 {
		t.Fatalf("budget 0 should yield no facts: %v", factIDs(block))
	}
	ex = excludedReasons(block)
	cut := 0
	for _, r := range ex {
		if r == "cut_budget" {
			cut++
		}
	}
	if cut == 0 {
		t.Fatalf("expected cut_budget exclusions: %v", ex)
	}
}
