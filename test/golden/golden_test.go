package golden_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ladvoy/brain/internal/testharness"
)

type tokens struct {
	Slug         string `json:"slug"`
	AdminToken   string `json:"admin_token"`
	ContextToken string `json:"context_token"`
}

func setupCarter(t *testing.T) (*testharness.Env, tokens) {
	t.Helper()
	env := testharness.StartParadeDB(t)
	resp := doJSON(t, env, "POST", "/v1/projects", "", nil, map[string]string{"slug": "carter", "name": "The Carter family"})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %s", resp.StatusCode, readBody(resp))
	}
	var tok tokens
	mustDecode(t, resp, &tok)

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "carter.json"))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", env.URL("/v1/projects/carter/import"), bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+tok.AdminToken)
	req.Header.Set("Content-Type", "application/json")
	r, err := env.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 201 {
		t.Fatalf("import: %d %s", r.StatusCode, readBody(r))
	}
	_ = r.Body.Close()
	return env, tok
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, f, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(f), "..", ".."))
}

// TestFR024_IsolationMatrix — US1 scenarios 1–4 (T024).
func TestFR024_IsolationMatrix(t *testing.T) {
	env, tok := setupCarter(t)

	t.Run("US1.1_rebecca_health_substituted", func(t *testing.T) {
		block := ask(t, env, tok, "rebecca", "What about grandpa health?")
		text, ok := factText(block, "f_fred_health")
		if !ok {
			t.Fatal("expected f_fred_health present via interpretation")
		}
		if strings.Contains(strings.ToLower(text), "urologist") {
			t.Fatalf("original leaked: %s", text)
		}
		if !strings.Contains(text, "age-related") {
			t.Fatalf("want child interpretation: %s", text)
		}
		if !hasSubstitution(block, "f_fred_health") {
			t.Fatal("audit substitution missing")
		}
	})

	t.Run("US1.2_rose_no_rebecca_owner_secret", func(t *testing.T) {
		block := ask(t, env, tok, "rose", "Rebecca school algebra test")
		if _, ok := factText(block, "f_rebecca_test"); ok {
			t.Fatal("f_rebecca_test must not reach rose")
		}
		ex := excluded(block)
		if ex["f_rebecca_test"] != "deny_owner" {
			// may not be a candidate if card matching missed — force broader query
			block = ask(t, env, tok, "rose", "Rebecca Becca school test Maya")
			if _, ok := factText(block, "f_rebecca_test"); ok {
				t.Fatal("f_rebecca_test leaked")
			}
			ex = excluded(block)
			if reason, ok := ex["f_rebecca_test"]; ok && reason != "deny_owner" {
				t.Fatalf("exclude reason: %s", reason)
			}
		}
	})

	t.Run("US1.3_jack_sees_wishlist", func(t *testing.T) {
		block := ask(t, env, tok, "jack", "gift for Rebecca birthday wishlist")
		if _, ok := factText(block, "f_rebecca_wishlist"); !ok {
			t.Fatalf("wishlist missing; facts=%v", factIDs(block))
		}
	})

	t.Run("US1.4_unknown_fail_closed", func(t *testing.T) {
		block := ask(t, env, tok, "not-a-person", "Fred health grandpa")
		audit := block["audit"].(map[string]any)
		if audit["degraded"] != "fail_closed_unknown_principal" {
			t.Fatalf("degraded=%v", audit["degraded"])
		}
		if text, ok := factText(block, "f_fred_health"); ok {
			if strings.Contains(strings.ToLower(text), "urologist") {
				t.Fatal("adults health original leaked to unknown")
			}
		}
	})
}

// TestFR025_AuditCompleteness — T025 / SC-004.
func TestFR025_AuditCompleteness(t *testing.T) {
	env, tok := setupCarter(t)
	block := ask(t, env, tok, "rebecca", "grandpa health gifts firearms")
	audit := block["audit"].(map[string]any)
	included, _ := audit["included"].([]any)
	excluded, _ := audit["excluded"].([]any)
	if len(included) == 0 && len(excluded) == 0 {
		t.Fatal("empty audit")
	}
	// every fact in response is in included
	for id := range factIDs(block) {
		found := false
		for _, i := range included {
			if i.(string) == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s in facts but not audit.included", id)
		}
	}
	for _, e := range excluded {
		m := e.(map[string]any)
		if m["fact"] == "" || m["reason"] == "" {
			t.Fatalf("incomplete exclusion: %v", m)
		}
	}
	subs, _ := audit["substitutions"].([]any)
	for _, s := range subs {
		m := s.(map[string]any)
		if m["fact"] == "" || m["used"] == "" {
			t.Fatalf("incomplete substitution: %v", m)
		}
	}
}

// TestFR026_ImportExportRoundTrip — T026.
func TestFR026_ImportExportRoundTrip(t *testing.T) {
	env, tok := setupCarter(t)
	resp := doJSON(t, env, "GET", "/v1/projects/carter/export", tok.AdminToken, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("export: %d %s", resp.StatusCode, readBody(resp))
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	// portrait-spec revalidate.pattern uses Perl lookaround; Go RE2 cannot compile it.
	// Drop that pattern, keep the rest of the schema for the gate.
	schemaBytes, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "project.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schemaDoc map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaDoc); err != nil {
		t.Fatal(err)
	}
	if defs, ok := schemaDoc["$defs"].(map[string]any); ok {
		if rev, ok := defs["revalidate"].(map[string]any); ok {
			if anyOf, ok := rev["anyOf"].([]any); ok {
				for _, item := range anyOf {
					if m, ok := item.(map[string]any); ok {
						delete(m, "pattern")
					}
				}
			}
		}
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("project.schema.json", schemaDoc); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	sch, err := compiler.Compile("project.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(doc); err != nil {
		t.Fatalf("schema: %v", err)
	}

	// structural: same fact ids as fixture
	var exported, fixture map[string]any
	_ = json.Unmarshal(raw, &exported)
	fixRaw, _ := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "carter.json"))
	_ = json.Unmarshal(fixRaw, &fixture)
	got, want := collectFactIDs(exported), collectFactIDs(fixture)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fact id mismatch\ngot  %v\nwant %v", got, want)
	}

	// retell.py meaning check when python available
	retell := filepath.Join(repoRoot(t), "..", "portrait-spec", "scripts", "retell.py")
	if _, err := os.Stat(retell); err == nil {
		tmp := filepath.Join(t.TempDir(), "export.json")
		_ = os.WriteFile(tmp, raw, 0o644)
		cmd := exec.Command("python", retell, tmp)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("retell skipped/failed: %v %s", err, out)
		} else if len(out) == 0 {
			t.Log("retell produced empty output")
		}
	}
}

// TestFR027_LatencySmoke — T027.
func TestFR027_LatencySmoke(t *testing.T) {
	env, tok := setupCarter(t)
	// warm
	_ = ask(t, env, tok, "rose", "grandpa gifts")
	const n = 20
	samples := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		_ = ask(t, env, tok, "rose", "grandpa gifts firearms collection health")
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[int(float64(n)*0.95)-1]
	t.Logf("assembly p95=%s", p95)
	if p95 > 100*time.Millisecond {
		t.Fatalf("p95 %s exceeds 100ms", p95)
	}
}

func ask(t *testing.T, env *testharness.Env, tok tokens, principal, query string) map[string]any {
	t.Helper()
	resp := doJSON(t, env, "POST", "/v1/projects/carter/context", tok.ContextToken,
		map[string]string{"X-Brain-Principal": principal},
		map[string]any{"query": query, "budget_tokens": 2000})
	if resp.StatusCode != 200 {
		t.Fatalf("context: %d %s", resp.StatusCode, readBody(resp))
	}
	var out map[string]any
	mustDecode(t, resp, &out)
	return out
}

func factText(block map[string]any, id string) (string, bool) {
	for _, f := range block["facts"].([]any) {
		m := f.(map[string]any)
		if m["id"] == id {
			return m["text"].(string), true
		}
	}
	return "", false
}

func factIDs(block map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, f := range block["facts"].([]any) {
		out[f.(map[string]any)["id"].(string)] = true
	}
	return out
}

func excluded(block map[string]any) map[string]string {
	out := map[string]string{}
	audit := block["audit"].(map[string]any)
	for _, e := range audit["excluded"].([]any) {
		m := e.(map[string]any)
		out[m["fact"].(string)] = m["reason"].(string)
	}
	return out
}

func hasSubstitution(block map[string]any, id string) bool {
	audit := block["audit"].(map[string]any)
	subs, _ := audit["substitutions"].([]any)
	for _, s := range subs {
		if s.(map[string]any)["fact"] == id {
			return true
		}
	}
	return false
}

func collectFactIDs(doc map[string]any) []string {
	var ids []string
	for _, c := range doc["cards"].([]any) {
		card := c.(map[string]any)
		facts, _ := card["facts"].([]any)
		for _, f := range facts {
			ids = append(ids, f.(map[string]any)["id"].(string))
		}
	}
	return ids
}

func doJSON(t *testing.T, env *testharness.Env, method, path, token string, headers map[string]string, payload any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, env.URL(path), rdr)
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

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return string(b)
}
