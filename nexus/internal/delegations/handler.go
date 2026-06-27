package delegations

import (
	"context"
	"net/http"
	"time"

	dto "github.com/devpablocristo/nexus/internal/delegations/handler/dto"
	domain "github.com/devpablocristo/nexus/internal/delegations/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

type delegationUsecase interface {
	Create(ctx context.Context, d domain.Delegation) (domain.Delegation, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Delegation, error)
	List(ctx context.Context) ([]domain.Delegation, error)
	Update(ctx context.Context, d domain.Delegation) (domain.Delegation, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type Handler struct {
	uc delegationUsecase
}

func NewHandler(uc delegationUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/delegations", h.create)
	mux.HandleFunc("GET /v1/delegations", h.list)
	mux.HandleFunc("GET /v1/delegations/{id}", h.getByID)
	mux.HandleFunc("PATCH /v1/delegations/{id}", h.update)
	mux.HandleFunc("DELETE /v1/delegations/{id}", h.deleteByID)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusDelegationsAdmin) {
		return
	}
	var body dto.CreateDelegationRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.OwnerID == "" || body.AgentID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "owner_id and agent_id are required")
		return
	}

	d := domain.Delegation{
		OwnerID:            body.OwnerID,
		OwnerType:          body.OwnerType,
		AgentID:            body.AgentID,
		AgentType:          body.AgentType,
		AllowedActionTypes: body.AllowedActionTypes,
		AllowedResources:   body.AllowedResources,
		Purpose:            body.Purpose,
		MaxRiskClass:       body.MaxRiskClass,
	}
	if orgID := principalOrgID(r); orgID != nil {
		d.OrgID = orgID
	} else if !identityhttp.HasNoAuthContext(r) && !identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	if body.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err == nil {
			d.ExpiresAt = &t
		}
	}

	created, err := h.uc.Create(r.Context(), d)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create delegation")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusDelegationsAdmin) {
		return
	}
	list, err := h.uc.List(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list delegations")
		return
	}
	out := make([]dto.DelegationResponse, 0, len(list))
	for _, d := range list {
		if !canAccessDelegationOrg(r, d) {
			continue
		}
		out = append(out, toResponse(d))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusDelegationsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	d, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canWriteDelegationOrg(r, d) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "delegation org is not writable for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toResponse(d))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusDelegationsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	d, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canWriteDelegationOrg(r, d) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "delegation org is not writable for this principal")
		return
	}
	var body dto.UpdateDelegationRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.AllowedActionTypes != nil {
		d.AllowedActionTypes = *body.AllowedActionTypes
	}
	if body.AllowedResources != nil {
		d.AllowedResources = *body.AllowedResources
	}
	if body.Purpose != nil {
		d.Purpose = *body.Purpose
	}
	if body.MaxRiskClass != nil {
		d.MaxRiskClass = *body.MaxRiskClass
	}
	if body.Enabled != nil {
		d.Enabled = *body.Enabled
	}
	if body.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err == nil {
			d.ExpiresAt = &t
		}
	}

	updated, err := h.uc.Update(r.Context(), d)
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) deleteByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusDelegationsAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	d, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canAccessDelegationOrg(r, d) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "delegation org is not allowed for this principal")
		return
	}
	if err := h.uc.DeleteByID(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toResponse(d domain.Delegation) dto.DelegationResponse {
	resp := dto.DelegationResponse{
		ID:                 d.ID.String(),
		OrgID:              stringPtrValue(d.OrgID),
		OwnerID:            d.OwnerID,
		OwnerType:          d.OwnerType,
		AgentID:            d.AgentID,
		AgentType:          d.AgentType,
		AllowedActionTypes: d.AllowedActionTypes,
		AllowedResources:   d.AllowedResources,
		Purpose:            d.Purpose,
		MaxRiskClass:       d.MaxRiskClass,
		Enabled:            d.Enabled,
		CreatedAt:          d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          d.UpdatedAt.Format(time.RFC3339),
	}
	if d.ExpiresAt != nil {
		s := d.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	return resp
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeError(w http.ResponseWriter, err error) {
	if domainerr.IsNotFound(err) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "delegation not found")
		return
	}
	// Resto: clasificar con el helper de platform en vez de enmascarar como 500.
	httpjson.WriteFlatErrorFrom(w, err, "delegation operation failed")
}
