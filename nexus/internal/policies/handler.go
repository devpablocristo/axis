package policies

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	policydto "github.com/devpablocristo/nexus/internal/policies/handler/dto"
	policydomain "github.com/devpablocristo/nexus/internal/policies/usecases/domain"
	requesteval "github.com/devpablocristo/nexus/internal/requests"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

// Port mínimo: solo lo que el handler necesita
const maxExpressionLen = 5000

type policyUsecase interface {
	Create(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error)
	GetByID(ctx context.Context, id uuid.UUID) (policydomain.Policy, error)
	List(ctx context.Context, filters ListFilters) ([]policydomain.Policy, error)
	Update(ctx context.Context, p policydomain.Policy) (policydomain.Policy, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
	ArchiveByID(ctx context.Context, id uuid.UUID) error
	RestoreByID(ctx context.Context, id uuid.UUID) error
}

type Handler struct {
	uc policyUsecase
}

func NewHandler(uc policyUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/policies", h.create)
	mux.HandleFunc("GET /v1/policies", h.list)
	mux.HandleFunc("GET /v1/policies/{id}", h.getByID)
	mux.HandleFunc("PATCH /v1/policies/{id}", h.update)
	mux.HandleFunc("DELETE /v1/policies/{id}", h.deleteByID)
	mux.HandleFunc("POST /v1/policies/{id}/archive", h.archiveByID)
	mux.HandleFunc("POST /v1/policies/{id}/restore", h.restoreByID)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	var body policydto.CreatePolicyRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Name == "" || body.Expression == "" || body.Effect == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "name, expression and effect are required")
		return
	}
	if err := validatePolicyFields(body.Expression, body.Effect, body.RiskOverride, body.Priority, body.Mode); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	mode := policydomain.PolicyModeEnforced
	if body.Mode == "shadow" {
		mode = policydomain.PolicyModeShadow
	}
	p := policydomain.Policy{
		Name:         body.Name,
		Description:  body.Description,
		Expression:   body.Expression,
		Effect:       body.Effect,
		RiskOverride: body.RiskOverride,
		Priority:     body.Priority,
		Mode:         mode,
		Enabled:      body.Enabled,
		ActionType:   body.ActionType,
		TargetSystem: body.TargetSystem,
		Origin:       "manual",
	}
	if orgID := principalOrgID(r); orgID != nil {
		p.OrgID = orgID
	} else if !identityhttp.HasNoAuthContext(r) && !identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	created, err := h.uc.Create(r.Context(), p)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create policy failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, toPolicyResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	archived := r.URL.Query().Get("archived") == "true"
	filters := ListFilters{IncludeArchived: archived}
	if identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		// Cross-org callers (ej: Pymes service que sirve N tenants) pueden
		// solicitar policies de un tenant específico via X-Org-ID. Sin el
		// header, listan todas las policies del cluster.
		if orgID := principalOrgID(r); orgID != nil {
			filters.OrgID = orgID
		}
	} else if orgID := principalOrgID(r); orgID != nil {
		filters.OrgID = orgID
	} else if !identityhttp.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	list, err := h.uc.List(r.Context(), filters)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list policies failed")
		return
	}
	out := make([]policydto.PolicyResponse, 0, len(list))
	for _, p := range list {
		out = append(out, toPolicyResponse(p))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	p, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	if !canWritePolicyOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "policy org is not writable for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toPolicyResponse(p))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	p, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	if !canWritePolicyOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "policy org is not writable for this principal")
		return
	}
	var body policydto.UpdatePolicyRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	// Aplicar solo los campos presentes (patch parcial)
	if body.Name != nil {
		p.Name = *body.Name
	}
	if body.Description != nil {
		p.Description = *body.Description
	}
	if body.Expression != nil {
		p.Expression = *body.Expression
	}
	if body.Effect != nil {
		p.Effect = *body.Effect
	}
	if body.RiskOverride != nil {
		p.RiskOverride = body.RiskOverride
	}
	if body.Priority != nil {
		p.Priority = *body.Priority
	}
	if body.Enabled != nil {
		p.Enabled = *body.Enabled
	}
	if body.ActionType != nil {
		p.ActionType = body.ActionType
	}
	if body.TargetSystem != nil {
		p.TargetSystem = body.TargetSystem
	}
	if body.Mode != nil {
		p.Mode = policydomain.PolicyMode(*body.Mode)
	}
	if err := validatePolicyFields(p.Expression, p.Effect, p.RiskOverride, p.Priority, string(p.Mode)); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	updated, err := h.uc.Update(r.Context(), p)
	if err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toPolicyResponse(updated))
}

func (h *Handler) deleteByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	p, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	if !canWritePolicyOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "policy org is not writable for this principal")
		return
	}
	if err := h.uc.DeleteByID(r.Context(), id); err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archiveByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	p, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	if !canWritePolicyOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "policy org is not writable for this principal")
		return
	}
	if err := h.uc.ArchiveByID(r.Context(), id); err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) restoreByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusPoliciesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	p, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	if !canAccessPolicyOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "policy org is not allowed for this principal")
		return
	}
	if err := h.uc.RestoreByID(r.Context(), id); err != nil {
		writePolicyUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Helpers ---

func toPolicyResponse(p policydomain.Policy) policydto.PolicyResponse {
	modeStr := string(p.Mode)
	if modeStr == "" {
		modeStr = "enforced"
	}
	resp := policydto.PolicyResponse{
		ID:           p.ID.String(),
		OrgID:        stringPtrValue(p.OrgID),
		Name:         p.Name,
		Description:  p.Description,
		Expression:   p.Expression,
		Effect:       p.Effect,
		RiskOverride: p.RiskOverride,
		Priority:     p.Priority,
		Origin:       p.Origin,
		Mode:         modeStr,
		Enabled:      p.Enabled,
		ShadowHits:   p.ShadowHits,
		ActionType:   p.ActionType,
		TargetSystem: p.TargetSystem,
		CreatedAt:    p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    p.UpdatedAt.Format(time.RFC3339),
	}
	if p.ArchivedAt != nil {
		s := p.ArchivedAt.Format(time.RFC3339)
		resp.ArchivedAt = &s
	}
	return resp
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func writePolicyUsecaseError(w http.ResponseWriter, err error) {
	if domainerr.IsNotFound(err) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "policy not found")
		return
	}
	if errors.Is(err, ErrArchived) {
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "policy is archived")
		return
	}
	httpjson.WriteFlatInternalError(w, err, "policy operation failed")
}

func validatePolicyFields(expression, effect string, riskOverride *string, priority int, mode string) error {
	if len(expression) > maxExpressionLen {
		return errors.New("expression too long")
	}
	if err := requesteval.NewPolicyEvaluator().Validate(expression); err != nil {
		return errors.New("invalid CEL expression: " + err.Error())
	}
	switch effect {
	case "allow", "deny", "require_approval":
	default:
		return errors.New("effect must be allow, deny or require_approval")
	}
	if riskOverride != nil {
		switch strings.TrimSpace(*riskOverride) {
		case "", "low", "medium", "high":
		default:
			return errors.New("risk_override must be low, medium or high")
		}
	}
	switch mode {
	case "", string(policydomain.PolicyModeEnforced), string(policydomain.PolicyModeShadow):
	default:
		return errors.New("mode must be enforced or shadow")
	}
	if priority < 0 {
		return errors.New("priority must be greater than or equal to 0")
	}
	return nil
}
