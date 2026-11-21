// Package assembly implements deterministic context assembly (FR-011…019).
package assembly

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ladvoy/brain/internal/model"
	"github.com/ladvoy/brain/internal/store"
)

const defaultBudget = 800

// Assembler builds structured context blocks.
type Assembler struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Assembler {
	return &Assembler{store: s, now: time.Now}
}

func (a *Assembler) SetNow(fn func() time.Time) {
	if fn != nil {
		a.now = fn
	}
}

type principalView struct {
	slug      string
	known     bool
	cardSlug  string
	roles     []string
	failClosed bool
}

// Assemble runs the full pipeline for a project + principal + request.
func (a *Assembler) Assemble(ctx context.Context, projectID uuid.UUID, principalSlug string, req model.ContextRequest) (*model.ContextBlock, error) {
	pv, err := a.resolvePrincipal(ctx, projectID, principalSlug)
	if err != nil {
		return nil, err
	}

	budget := defaultBudget
	if req.BudgetTokens != nil {
		budget = *req.BudgetTokens
	}

	matchedCards, err := a.store.MatchCards(ctx, projectID, req.Query)
	if err != nil {
		return nil, err
	}
	// also match via BM25 fact hits → their cards
	bm25Facts, err := a.store.SearchFactsBM25(ctx, projectID, req.Query)
	if err != nil {
		return nil, err
	}
	cardBySlug := map[string]model.Card{}
	for _, c := range matchedCards {
		cardBySlug[c.Slug] = c
	}
	scoreByRef := map[string]float64{}
	for _, f := range bm25Facts {
		scoreByRef[f.FactRef] = f.BM25Score
		if _, ok := cardBySlug[f.CardSlug]; !ok {
			c, err := a.store.GetCard(ctx, projectID, f.CardSlug)
			if err == nil {
				cardBySlug[c.Slug] = *c
			}
		}
	}

	matchedSlugs := make([]string, 0, len(cardBySlug))
	cardIDs := make([]uuid.UUID, 0, len(cardBySlug))
	for slug, c := range cardBySlug {
		matchedSlugs = append(matchedSlugs, slug)
		cardIDs = append(cardIDs, c.ID)
	}
	sort.Strings(matchedSlugs)

	// candidate facts from matched cards
	cardFacts, err := a.store.FactsByCardIDs(ctx, projectID, cardIDs)
	if err != nil {
		return nil, err
	}

	// touched groups = groups of matched facts ∪ intent_hint ∪ named groups in query
	touched := map[string]bool{}
	if req.IntentHint != "" {
		touched[req.IntentHint] = true
	}
	for _, f := range append(cardFacts, bm25Facts...) {
		for _, g := range f.Groups {
			touched[g] = true
		}
	}
	// query tokens that equal group names
	allGroups, _ := a.store.ListGroups(ctx, projectID)
	qlower := strings.ToLower(req.Query)
	for _, g := range allGroups {
		if strings.Contains(qlower, strings.ToLower(g)) {
			touched[g] = true
		}
	}
	touchedList := keys(touched)

	// FR-024: related facts via shared groups
	related, err := a.store.FactsByGroups(ctx, projectID, touchedList)
	if err != nil {
		return nil, err
	}

	candidates := dedupeFacts(append(append(cardFacts, bm25Facts...), related...))
	for i := range candidates {
		if sc, ok := scoreByRef[candidates[i].FactRef]; ok {
			candidates[i].BM25Score = sc
		}
	}

	block := &model.ContextBlock{
		Facts: []model.ContextFact{},
		Notes: []string{},
		Audit: model.ContextAudit{
			MatchedCards:  matchedSlugs,
			Included:      []string{},
			Excluded:      []model.ExcludedFact{},
			Substitutions: []model.Substitution{},
		},
	}
	if pv.failClosed {
		block.Audit.Degraded = "fail_closed_unknown_principal"
	}

	type cand struct {
		fact     model.Fact
		text     string
		flags    []string
		coreHit  bool
		visible  bool
		subUsed  string
	}
	var pool []cand
	today := a.now().UTC().Truncate(24 * time.Hour)

	for _, f := range candidates {
		c := cand{fact: f, text: f.Text}

		// ended (FR-015)
		if f.TimeTo != nil && f.TimeTo.Before(today) {
			if !req.IncludeHistory {
				block.Audit.Excluded = append(block.Audit.Excluded, model.ExcludedFact{Fact: f.FactRef, Reason: "ended"})
				continue
			}
			c.flags = append(c.flags, "ended")
		}

		// disclosure (FR-016)
		if f.Disclosure != nil && disclosureActive(f.Disclosure, today) {
			owner := factOwner(f)
			if pv.slug != owner {
				block.Audit.Excluded = append(block.Audit.Excluded, model.ExcludedFact{Fact: f.FactRef, Reason: "disclosure"})
				if f.Disclosure.Meanwhile != "" {
					block.Notes = append(block.Notes, f.Disclosure.Meanwhile)
				}
				_ = a.store.WriteAudit(ctx, &projectID, strPtr(pv.slug), "inquiry", &f.FactRef, map[string]any{
					"reason": "disclosure_inquiry", "card": f.CardSlug,
				})
				continue
			}
		}

		vis := resolveVisibility(f, cardBySlug[f.CardSlug])
		visible := visibilityAllows(vis, pv, f)
		c.visible = visible

		// interpretation (FR-013)
		if subst, ok := matchInterpretation(f, pv); ok {
			c.subUsed = subst
			c.text = subst
			c.flags = append(c.flags, "substituted")
			block.Audit.Substitutions = append(block.Audit.Substitutions, model.Substitution{Fact: f.FactRef, Used: subst})
			// original only when visible — we always present substituted text when rule matches
		} else if !visible {
			reason := denyReason(vis)
			block.Audit.Excluded = append(block.Audit.Excluded, model.ExcludedFact{Fact: f.FactRef, Reason: reason})
			continue
		}

		// fail-closed unknown principal: only everyone (+ interpretation already handled)
		if pv.failClosed && !isEveryone(vis) && c.subUsed == "" {
			block.Audit.Excluded = append(block.Audit.Excluded, model.ExcludedFact{Fact: f.FactRef, Reason: "fail_closed"})
			continue
		}

		// core for touched groups (FR-014)
		for _, g := range f.Core {
			if touched[g] {
				c.coreHit = true
				c.flags = append(c.flags, "core")
				break
			}
		}

		// stale revalidate flag
		if f.Revalidate != nil && *f.Revalidate != "" && *f.Revalidate != "never" && f.TimeFrom != nil {
			if stale(*f.Revalidate, *f.TimeFrom, today) {
				c.flags = append(c.flags, "stale")
			}
		}

		pool = append(pool, c)
	}

	// ranking FR-018: core → BM25 → recency
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].coreHit != pool[j].coreHit {
			return pool[i].coreHit
		}
		if pool[i].fact.BM25Score != pool[j].fact.BM25Score {
			return pool[i].fact.BM25Score > pool[j].fact.BM25Score
		}
		ti, tj := pool[i].fact.TimeFrom, pool[j].fact.TimeFrom
		if ti == nil && tj == nil {
			return pool[i].fact.FactRef < pool[j].fact.FactRef
		}
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		return ti.After(*tj)
	})

	used := 0
	for _, c := range pool {
		cost := estimateTokens(c.text)
		if used+cost > budget && budget >= 0 {
			block.Audit.Excluded = append(block.Audit.Excluded, model.ExcludedFact{Fact: c.fact.FactRef, Reason: "cut_budget"})
			continue
		}
		used += cost
		cf := model.ContextFact{
			ID:        c.fact.FactRef,
			Text:      c.text,
			Groups:    c.fact.Groups,
			SourceRef: c.fact.Source.Ref,
			Flags:     uniqueStrings(c.flags),
		}
		block.Facts = append(block.Facts, cf)
		block.Audit.Included = append(block.Audit.Included, c.fact.FactRef)
	}

	details := map[string]any{
		"included":      block.Audit.Included,
		"excluded":      block.Audit.Excluded,
		"substitutions": block.Audit.Substitutions,
		"matched_cards": block.Audit.MatchedCards,
	}
	if block.Audit.Degraded != "" {
		details["degraded"] = block.Audit.Degraded
	}
	actor := principalSlug
	_ = a.store.WriteAudit(ctx, &projectID, &actor, "assembly", nil, details)

	return block, nil
}

func (a *Assembler) resolvePrincipal(ctx context.Context, projectID uuid.UUID, slug string) (principalView, error) {
	pv := principalView{slug: slug}
	if slug == "" {
		pv.failClosed = true
		return pv, nil
	}
	p, err := a.store.GetPrincipal(ctx, projectID, slug)
	if err != nil {
		if err == store.ErrNotFound {
			pv.failClosed = true
			return pv, nil
		}
		return pv, err
	}
	pv.known = true
	if p.CardSlug != nil {
		pv.cardSlug = *p.CardSlug
	}
	roles, err := a.store.ListRoles(ctx, projectID)
	if err != nil {
		return pv, err
	}
	for _, r := range roles {
		for _, m := range r.Members {
			if m == slug {
				pv.roles = append(pv.roles, r.Name)
				break
			}
		}
	}
	return pv, nil
}

func resolveVisibility(f model.Fact, card model.Card) model.Visibility {
	if f.Visibility != nil {
		return *f.Visibility
	}
	if card.Defaults != nil {
		if v, ok := card.Defaults["visibility"]; ok {
			b, _ := jsonMarshal(v)
			var vis model.Visibility
			if jsonUnmarshal(b, &vis) == nil && vis.Mode != "" {
				return vis
			}
		}
	}
	return model.Visibility{Mode: "owner"}
}

func visibilityAllows(vis model.Visibility, pv principalView, f model.Fact) bool {
	switch vis.Mode {
	case "everyone", "":
		return true
	case "owner":
		return pv.slug == factOwner(f)
	case "role":
		if vis.Role == "" {
			return false
		}
		for _, r := range pv.roles {
			if r == vis.Role {
				return true
			}
		}
		return false
	case "list":
		for _, p := range vis.Principals {
			if p == pv.slug {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func isEveryone(vis model.Visibility) bool {
	return vis.Mode == "everyone" || vis.Mode == ""
}

func denyReason(vis model.Visibility) string {
	switch vis.Mode {
	case "owner":
		return "deny_owner"
	case "role":
		return "deny_role"
	case "list":
		return "deny_list"
	default:
		return "deny_unknown"
	}
}

func factOwner(f model.Fact) string {
	if f.Author != nil && *f.Author != "" {
		return *f.Author
	}
	return f.CardSlug
}

func matchInterpretation(f model.Fact, pv principalView) (string, bool) {
	for _, rule := range f.Interpretation {
		if interpretationMatches(rule.When, pv) {
			if rule.Text != "" {
				return rule.Text, true
			}
			if rule.Directive != "" {
				return rule.Directive, true
			}
		}
	}
	return "", false
}

func interpretationMatches(when map[string]any, pv principalView) bool {
	if when == nil {
		return false
	}
	if role, ok := when["role"].(string); ok {
		for _, r := range pv.roles {
			if r == role {
				return true
			}
		}
		return false
	}
	if slug, ok := when["principal"].(string); ok {
		return slug == pv.slug
	}
	return false
}

func disclosureActive(d *model.DisclosureRule, today time.Time) bool {
	if d == nil || d.Until == "" {
		return false
	}
	until, err := time.Parse("2006-01-02", d.Until)
	if err != nil {
		return true
	}
	return !today.After(until)
}

func stale(revalidate string, from, today time.Time) bool {
	// ISO-8601 duration starting with P — rough support for PnM / PnD / PnY
	revalidate = strings.TrimSpace(revalidate)
	if !strings.HasPrefix(revalidate, "P") {
		return false
	}
	body := revalidate[1:]
	var months, days, years int
	n := 0
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
			continue
		}
		switch c {
		case 'Y':
			years = n
		case 'M':
			months = n
		case 'D':
			days = n
		}
		n = 0
	}
	due := from.AddDate(years, months, days)
	return today.After(due)
}

func estimateTokens(text string) int {
	// rough: ~4 chars per token
	n := utf8.RuneCountInString(text)
	t := n / 4
	if t < 1 {
		t = 1
	}
	return t
}

func dedupeFacts(in []model.Fact) []model.Fact {
	seen := map[string]int{}
	var out []model.Fact
	for _, f := range in {
		if i, ok := seen[f.FactRef]; ok {
			if f.BM25Score > out[i].BM25Score {
				out[i] = f
			}
			continue
		}
		seen[f.FactRef] = len(out)
		out = append(out, f)
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func strPtr(s string) *string { return &s }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
