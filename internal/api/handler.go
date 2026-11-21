// Package api implements the OpenAPI ServerInterface (privileged + query planes).
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ladvoy/brain/internal/api/gen"
	"github.com/ladvoy/brain/internal/assembly"
	"github.com/ladvoy/brain/internal/auth"
	"github.com/ladvoy/brain/internal/model"
	"github.com/ladvoy/brain/internal/store"
)

// API wires store + assembly to the generated contract.
type API struct {
	Store *store.Store
	Asm   *assembly.Assembler
}

var _ gen.ServerInterface = (*API)(nil)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, gen.Error{Message: msg})
}

func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) (*model.Project, bool) {
	p, err := a.Store.GetProject(r.Context(), string(projectId))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project not found")
			return nil, false
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	tok := auth.Bearer(r)
	if !auth.EqualHash(tok, p.AdminTokenHash) {
		pid := p.ID
		_ = a.Store.WriteAudit(r.Context(), &pid, nil, "failed_auth", nil, map[string]any{
			"plane": "admin", "path": r.URL.Path,
		})
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return p, true
}

func (a *API) requireContext(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) (*model.Project, bool) {
	p, err := a.Store.GetProject(r.Context(), string(projectId))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project not found")
			return nil, false
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	tok := auth.Bearer(r)
	if !auth.EqualHash(tok, p.ContextTokenHash) {
		pid := p.ID
		_ = a.Store.WriteAudit(r.Context(), &pid, nil, "failed_auth", nil, map[string]any{
			"plane": "context", "path": r.URL.Path,
		})
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return p, true
}

func actorFrom(r *http.Request) *string {
	s := r.Header.Get("X-Brain-Actor")
	if s == "" {
		s = r.Header.Get("X-Brain-Principal")
	}
	if s == "" {
		return nil
	}
	return &s
}

// CreateProject — no auth (bootstrap).
func (a *API) CreateProject(w http.ResponseWriter, r *http.Request) {
	var body gen.ProjectCreate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	if body.Slug == "" || body.Name == "" {
		writeErr(w, http.StatusUnprocessableEntity, "slug and name required")
		return
	}
	adminTok, err := auth.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctxTok, err := auth.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, err := a.Store.CreateProject(r.Context(), body.Slug, body.Name, auth.Hash(adminTok), auth.Hash(ctxTok))
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, nil, "mutation", strPtr(p.Slug), map[string]any{
		"op": "create_project", "after": map[string]any{"slug": p.Slug, "name": p.Name},
	})
	writeJSON(w, http.StatusCreated, gen.ProjectTokens{
		Slug: p.Slug, AdminToken: adminTok, ContextToken: ctxTok,
	})
}

func (a *API) GetProject(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, gen.ProjectInfo{Slug: p.Slug, Name: p.Name, CreatedAt: &p.CreatedAt})
}

func (a *API) PatchProject(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	var body gen.ProjectPatch
	_ = json.NewDecoder(r.Body).Decode(&body)
	before := p.Name
	if body.Name != nil {
		if err := a.Store.UpdateProjectName(r.Context(), p.ID, *body.Name); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		p.Name = *body.Name
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &p.Slug, map[string]any{
		"op": "patch_project", "before": before, "after": p.Name,
	})
	w.WriteHeader(http.StatusOK)
}

func (a *API) DeleteProject(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	pid := p.ID
	if err := a.Store.DeleteProject(r.Context(), p.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &pid, actorFrom(r), "delete", &p.Slug, map[string]any{
		"op": "delete_project",
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) ImportProject(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	var doc model.PortraitFile
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid portrait JSON: "+err.Error())
		return
	}
	cards, facts, err := a.Store.ImportPortrait(r.Context(), p.ID, &doc)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &p.Slug, map[string]any{
		"op": "import", "cards": cards, "facts": facts,
	})
	writeJSON(w, http.StatusCreated, map[string]int{"cards": cards, "facts": facts})
}

func (a *API) ExportProject(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	doc, err := a.Store.ExportPortrait(r.Context(), p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (a *API) ListPrincipals(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	list, err := a.Store.ListPrincipals(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gen.Principal, 0, len(list))
	for _, pr := range list {
		gp := gen.Principal{Slug: pr.Slug, Attributes: &pr.Attributes, Card: pr.CardSlug}
		out = append(out, gp)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) CreatePrincipal(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	var body gen.Principal
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Slug == "" {
		writeErr(w, http.StatusUnprocessableEntity, "slug required")
		return
	}
	attrs := map[string]any{}
	if body.Attributes != nil {
		attrs = *body.Attributes
	}
	if err := a.Store.CreatePrincipal(r.Context(), p.ID, body.Slug, attrs, body.Card); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrValidation) || errors.Is(err, store.ErrConflict) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &body.Slug, map[string]any{
		"op": "create_principal", "after": body,
	})
	w.WriteHeader(http.StatusCreated)
}

func (a *API) ListRoles(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	list, err := a.Store.ListRoles(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gen.Role, 0, len(list))
	for _, role := range list {
		out = append(out, gen.Role{Name: role.Name, Members: role.Members})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) PutRole(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	var body gen.Role
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, http.StatusUnprocessableEntity, "name required")
		return
	}
	if err := a.Store.PutRole(r.Context(), p.ID, body.Name, body.Members); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrValidation) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &body.Name, map[string]any{
		"op": "put_role", "after": body,
	})
	w.WriteHeader(http.StatusCreated)
}

func (a *API) ListGroups(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	list, err := a.Store.ListGroups(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []string{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) CreateGroup(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, http.StatusUnprocessableEntity, "name required")
		return
	}
	if err := a.Store.CreateGroup(r.Context(), p.ID, body.Name); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrConflict) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &body.Name, map[string]any{
		"op": "create_group",
	})
	w.WriteHeader(http.StatusCreated)
}

func (a *API) ListCards(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, params gen.ListCardsParams) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	q := ""
	if params.Q != nil {
		q = *params.Q
	}
	list, err := a.Store.ListCards(r.Context(), p.ID, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gen.Card, 0, len(list))
	for _, c := range list {
		out = append(out, toGenCard(c, nil))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) CreateCard(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	var body gen.Card
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	c := model.Card{ProjectID: p.ID, Slug: body.Slug, Type: body.Type, Name: body.Name}
	if body.Aliases != nil {
		c.Aliases = *body.Aliases
	}
	if body.Defaults != nil {
		c.Defaults = *body.Defaults
	}
	if body.Extensions != nil {
		c.Extensions = *body.Extensions
	}
	if err := a.Store.CreateCard(r.Context(), &c); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrValidation) || errors.Is(err, store.ErrConflict) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &c.Slug, map[string]any{
		"op": "create_card", "after": body,
	})
	w.WriteHeader(http.StatusCreated)
}

func (a *API) GetCard(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, slug string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	c, err := a.Store.GetCard(r.Context(), p.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "card not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	facts, err := a.Store.ListFacts(r.Context(), p.ID, slug, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toGenCard(*c, facts))
}

func (a *API) PatchCard(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, slug string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	c, err := a.Store.GetCard(r.Context(), p.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "card not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var body gen.Card
	_ = json.NewDecoder(r.Body).Decode(&body)
	before := *c
	if body.Name != "" {
		c.Name = body.Name
	}
	if body.Type != "" {
		c.Type = body.Type
	}
	if body.Aliases != nil {
		c.Aliases = *body.Aliases
	}
	if body.Defaults != nil {
		c.Defaults = *body.Defaults
	}
	if err := a.Store.UpdateCard(r.Context(), c); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &slug, map[string]any{
		"op": "patch_card", "before": before.Name, "after": c.Name,
	})
	w.WriteHeader(http.StatusOK)
}

func (a *API) DeleteCard(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, slug string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	if err := a.Store.DeleteCard(r.Context(), p.ID, slug); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "card not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "delete", &slug, map[string]any{"op": "delete_card"})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) ListFacts(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, slug string, params gen.ListFactsParams) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	st := ""
	if params.Status != nil {
		st = string(*params.Status)
	}
	list, err := a.Store.ListFacts(r.Context(), p.ID, slug, st)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gen.Fact, 0, len(list))
	for _, f := range list {
		out = append(out, toGenFact(f))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) CreateFact(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, slug string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	card, err := a.Store.GetCard(r.Context(), p.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "card not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	f, err := factFromRaw(p.ID, card.ID, slug, raw)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.Store.CreateFact(r.Context(), f); err != nil {
		if errors.Is(err, store.ErrDuplicateReject) {
			writeJSON(w, http.StatusUnprocessableEntity, gen.Error{
				Message: "previously rejected",
				Details: &map[string]any{"prior": strings.TrimPrefix(err.Error(), store.ErrDuplicateReject.Error()+": ")},
			})
			return
		}
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrValidation) || errors.Is(err, store.ErrConflict) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &f.FactRef, map[string]any{
		"op": "create_fact", "after": f,
	})
	writeJSON(w, http.StatusCreated, toGenFact(*f))
}

func (a *API) GetFact(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, factRef string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	f, err := a.Store.GetFact(r.Context(), p.ID, factRef)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "fact not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toGenFact(*f))
}

func (a *API) PatchFact(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, factRef string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	f, err := a.Store.GetFact(r.Context(), p.ID, factRef)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "fact not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	before := f.Text
	var raw map[string]any
	_ = json.NewDecoder(r.Body).Decode(&raw)
	applyFactPatch(f, raw)
	if err := a.Store.UpdateFact(r.Context(), f); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrValidation) {
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &factRef, map[string]any{
		"op": "patch_fact", "before": before, "after": f.Text,
	})
	writeJSON(w, http.StatusOK, toGenFact(*f))
}

func (a *API) DeleteFact(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, factRef string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	if err := a.Store.SetFactStatus(r.Context(), p.ID, factRef, "deleted"); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "fact not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "delete", &factRef, map[string]any{
		"op": "soft_delete_fact",
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) ListProposals(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	list, err := a.Store.ListProposals(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gen.Fact, 0, len(list))
	for _, f := range list {
		out = append(out, toGenFact(f))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) ConfirmProposal(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, factRef string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	if err := a.Store.SetFactStatus(r.Context(), p.ID, factRef, "confirmed"); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "fact not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &factRef, map[string]any{
		"op": "confirm", "after": "confirmed",
	})
	w.WriteHeader(http.StatusOK)
}

func (a *API) RejectProposal(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, factRef string) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	if err := a.Store.SetFactStatus(r.Context(), p.ID, factRef, "rejected"); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "fact not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.Store.WriteAudit(r.Context(), &p.ID, actorFrom(r), "mutation", &factRef, map[string]any{
		"op": "reject", "after": "rejected",
	})
	w.WriteHeader(http.StatusOK)
}

func (a *API) ReadAudit(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, params gen.ReadAuditParams) {
	p, ok := a.requireAdmin(w, r, projectId)
	if !ok {
		return
	}
	actor, kind := "", ""
	var since *time.Time
	if params.Actor != nil {
		actor = *params.Actor
	}
	if params.Kind != nil {
		kind = *params.Kind
	}
	if params.Since != nil {
		since = params.Since
	}
	list, err := a.Store.ReadAudit(r.Context(), p.ID, actor, kind, since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gen.AuditEntry, 0, len(list))
	for _, e := range list {
		d := e.Details
		out = append(out, gen.AuditEntry{
			Ts: &e.Ts, Actor: e.Actor, Kind: &e.Kind, Object: e.Object, Details: &d,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) AssembleContext(w http.ResponseWriter, r *http.Request, projectId gen.ProjectId, params gen.AssembleContextParams) {
	p, ok := a.requireContext(w, r, projectId)
	if !ok {
		return
	}
	var req model.ContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid body")
		return
	}
	principal := params.XBrainPrincipal
	block, err := a.Asm.Assemble(r.Context(), p.ID, principal, req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, block)
}

func strPtr(s string) *string { return &s }

func toGenCard(c model.Card, facts []model.Fact) gen.Card {
	aliases, defaults, ext := c.Aliases, c.Defaults, c.Extensions
	gc := gen.Card{
		Slug: c.Slug, Type: c.Type, Name: c.Name,
		Aliases: &aliases, Defaults: &defaults, Extensions: &ext,
	}
	if facts != nil {
		gf := make([]gen.Fact, 0, len(facts))
		for _, f := range facts {
			gf = append(gf, toGenFact(f))
		}
		gc.Facts = &gf
	}
	return gc
}

func toGenFact(f model.Fact) gen.Fact {
	gf := gen.Fact{
		Id: f.FactRef, Text: f.Text, Status: gen.FactStatus(f.Status),
		Source: gen.Source{Type: f.Source.Type, Ref: f.Source.Ref},
		Author: f.Author, Revalidate: f.Revalidate,
	}
	g, core, keys := f.Groups, f.Core, f.Keys
	gf.Groups, gf.Core, gf.Keys = &g, &core, &keys
	if f.Source.Note != "" {
		n := f.Source.Note
		gf.Source.Note = &n
	}
	if len(f.Interpretation) > 0 {
		ir := make([]gen.InterpretationRule, 0, len(f.Interpretation))
		for _, rule := range f.Interpretation {
			gr := gen.InterpretationRule{When: rule.When}
			if rule.Text != "" {
				t := rule.Text
				gr.Text = &t
			}
			if rule.Directive != "" {
				d := rule.Directive
				gr.Directive = &d
			}
			ir = append(ir, gr)
		}
		gf.Interpretation = &ir
	}
	if f.Visibility != nil {
		b, _ := json.Marshal(f.Visibility)
		var v gen.Visibility
		_ = json.Unmarshal(b, &v)
		gf.Visibility = &v
	}
	ext := f.Extensions
	gf.Extensions = &ext
	return gf
}

func factFromRaw(projectID, cardID uuid.UUID, cardSlug string, raw map[string]any) (*model.Fact, error) {
	b, _ := json.Marshal(raw)
	var pf model.PortraitFact
	if err := json.Unmarshal(b, &pf); err != nil {
		return nil, err
	}
	if pf.ID == "" || pf.Text == "" {
		return nil, errors.New("id and text required")
	}
	// detect missing source
	srcRaw, hasSource := raw["source"]
	f, err := portraitBridge(projectID, cardID, pf)
	if err != nil {
		return nil, err
	}
	f.CardSlug = cardSlug
	if !hasSource || srcRaw == nil {
		f.Source = model.Source{}
		f.Status = "pending"
	}
	return f, nil
}

func portraitBridge(projectID, cardID uuid.UUID, pf model.PortraitFact) (*model.Fact, error) {
	// reuse store helper via re-export — inline copy
	f := &model.Fact{
		ProjectID: projectID, CardID: cardID, FactRef: pf.ID, Text: pf.Text,
		Status: pf.Status, Source: pf.Source, Groups: pf.Groups, Visibility: pf.Visibility,
		Core: pf.Core, Keys: pf.Keys, Interpretation: pf.Interpretation,
		Disclosure: pf.Disclosure, Extensions: pf.Extensions,
	}
	if pf.Author != "" {
		a := pf.Author
		f.Author = &a
	}
	if pf.Revalidate != "" {
		r := pf.Revalidate
		f.Revalidate = &r
	}
	if pf.Time != nil {
		if pf.Time.From != "" {
			t, err := time.Parse("2006-01-02", pf.Time.From)
			if err != nil {
				return nil, err
			}
			f.TimeFrom = &t
		}
		if pf.Time.To != "" {
			t, err := time.Parse("2006-01-02", pf.Time.To)
			if err != nil {
				return nil, err
			}
			f.TimeTo = &t
		}
	}
	if f.Status == "" {
		f.Status = "confirmed"
	}
	return f, nil
}

func applyFactPatch(f *model.Fact, raw map[string]any) {
	if v, ok := raw["text"].(string); ok {
		f.Text = v
	}
	if v, ok := raw["status"].(string); ok {
		f.Status = v
	}
	if v, ok := raw["groups"]; ok {
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &f.Groups)
	}
	if v, ok := raw["visibility"]; ok {
		b, _ := json.Marshal(v)
		var vis model.Visibility
		if json.Unmarshal(b, &vis) == nil {
			f.Visibility = &vis
		}
	}
	if v, ok := raw["disclosure"]; ok {
		b, _ := json.Marshal(v)
		var d model.DisclosureRule
		if json.Unmarshal(b, &d) == nil {
			f.Disclosure = &d
		}
	}
	if v, ok := raw["interpretation"]; ok {
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &f.Interpretation)
	}
}
