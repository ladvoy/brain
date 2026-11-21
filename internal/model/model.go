// Package model holds domain types used by store and assembly.
package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID               uuid.UUID
	Slug             string
	Name             string
	AdminTokenHash   []byte
	ContextTokenHash []byte
	CreatedAt        time.Time
}

type Principal struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	Slug       string
	Attributes map[string]any
	CardSlug   *string
}

type Role struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Members   []string
}

type Card struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	Slug       string
	Type       string
	Name       string
	Aliases    []string
	Defaults   map[string]any
	Extensions map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Visibility struct {
	Mode       string   `json:"mode"`
	Role       string   `json:"role,omitempty"`
	Principals []string `json:"principals,omitempty"`
}

type Source struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
	Date string `json:"date,omitempty"`
	Note string `json:"note,omitempty"`
}

type InterpretationRule struct {
	When      map[string]any `json:"when"`
	Text      string         `json:"text,omitempty"`
	Directive string         `json:"directive,omitempty"`
}

type DisclosureRule struct {
	Until     string      `json:"until"`
	Then      Visibility  `json:"then"`
	Meanwhile string      `json:"meanwhile,omitempty"`
	Review    string      `json:"review,omitempty"`
}

type Fact struct {
	ID             uuid.UUID
	ProjectID      uuid.UUID
	CardID         uuid.UUID
	CardSlug       string
	FactRef        string
	Text           string
	Author         *string
	Status         string
	Source         Source
	TimeFrom       *time.Time
	TimeTo         *time.Time
	Revalidate     *string
	Groups         []string
	Visibility     *Visibility
	Core           []string
	Keys           []string
	Interpretation []InterpretationRule
	Disclosure     *DisclosureRule
	Extensions     map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// BM25Score is set by search queries when available.
	BM25Score float64
}

type AuditEntry struct {
	ID        int64
	ProjectID *uuid.UUID
	Ts        time.Time
	Actor     *string
	Kind      string
	Object    *string
	Details   map[string]any
}

type ContextRequest struct {
	Query          string `json:"query"`
	IntentHint     string `json:"intent_hint,omitempty"`
	BudgetTokens   *int   `json:"budget_tokens,omitempty"` // nil = deployment default; 0 = empty (cut_budget)
	IncludeHistory bool   `json:"include_history,omitempty"`
}

type ContextFact struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Groups    []string `json:"groups,omitempty"`
	SourceRef string   `json:"source_ref,omitempty"`
	Flags     []string `json:"flags,omitempty"`
}

type ExcludedFact struct {
	Fact   string `json:"fact"`
	Reason string `json:"reason"`
}

type Substitution struct {
	Fact string `json:"fact"`
	Used string `json:"used"`
}

type ContextAudit struct {
	MatchedCards  []string       `json:"matched_cards,omitempty"`
	Included      []string       `json:"included"`
	Excluded      []ExcludedFact `json:"excluded"`
	Substitutions []Substitution `json:"substitutions,omitempty"`
	Degraded      string         `json:"degraded,omitempty"`
}

type ContextBlock struct {
	Facts []ContextFact `json:"facts"`
	Notes []string      `json:"notes,omitempty"`
	Audit ContextAudit  `json:"audit"`
}

// PortraitFile is the on-wire portrait-card project document.
type PortraitFile struct {
	Spec        string           `json:"spec"`
	SpecVersion string           `json:"spec_version"`
	Project     PortraitProject  `json:"project"`
	Principals  []PortraitPrinc  `json:"principals"`
	Groups      []string         `json:"groups"`
	Cards       []PortraitCard   `json:"cards"`
	Extensions  map[string]any   `json:"extensions,omitempty"`
}

type PortraitProject struct {
	Slug           string              `json:"slug"`
	Name           string              `json:"name"`
	Roles          map[string][]string `json:"roles,omitempty"`
	Interpretation []InterpretationRule `json:"interpretation,omitempty"`
	Extensions     map[string]any      `json:"extensions,omitempty"`
}

type PortraitPrinc struct {
	Slug       string         `json:"slug"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Card       string         `json:"card,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type PortraitCard struct {
	Slug       string         `json:"slug"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Aliases    []string       `json:"aliases,omitempty"`
	Defaults   map[string]any `json:"defaults,omitempty"`
	Facts      []PortraitFact `json:"facts,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type PortraitFact struct {
	ID             string               `json:"id"`
	Text           string               `json:"text"`
	Author         string               `json:"author,omitempty"`
	Status         string               `json:"status"`
	Source         Source               `json:"source"`
	Time           *PortraitTime        `json:"time,omitempty"`
	Revalidate     string               `json:"revalidate,omitempty"`
	Groups         []string             `json:"groups,omitempty"`
	Visibility     *Visibility          `json:"visibility,omitempty"`
	Core           []string             `json:"core,omitempty"`
	Keys           []string             `json:"keys,omitempty"`
	Interpretation []InterpretationRule `json:"interpretation,omitempty"`
	Disclosure     *DisclosureRule      `json:"disclosure,omitempty"`
	Extensions     map[string]any       `json:"extensions,omitempty"`
}

// MarshalJSON omits empty optional slices/maps that the portrait schema rejects.
func (f PortraitFact) MarshalJSON() ([]byte, error) {
	type alias PortraitFact
	a := alias(f)
	if len(a.Interpretation) == 0 {
		a.Interpretation = nil
	}
	if len(a.Groups) == 0 {
		a.Groups = nil
	}
	if len(a.Core) == 0 {
		a.Core = nil
	}
	if len(a.Keys) == 0 {
		a.Keys = nil
	}
	if len(a.Extensions) == 0 {
		a.Extensions = nil
	}
	if a.Visibility != nil && a.Visibility.Mode == "" {
		a.Visibility = nil
	}
	if a.Disclosure != nil && a.Disclosure.Until == "" {
		a.Disclosure = nil
	}
	return json.Marshal(a)
}

type PortraitTime struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

func MustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
