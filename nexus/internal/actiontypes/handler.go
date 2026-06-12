package actiontypes

import (
	"context"
	"net/http"
	"time"

	dto "github.com/devpablocristo/nexus/internal/actiontypes/handler/dto"
	domain "github.com/devpablocristo/nexus/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

type actionTypeUsecase interface {
	Create(ctx context.Context, at domain.ActionType) (domain.ActionType, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.ActionType, error)
	List(ctx context.Context) ([]domain.ActionType, error)
	ListForOrg(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.ActionType, error)
	Update(ctx context.Context, at domain.ActionType) (domain.ActionType, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type Handler struct {
	uc actionTypeUsecase
}

func NewHandler(uc actionTypeUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/action-types", h.create)
	mux.HandleFunc("GET /v1/action-types", h.list)
	mux.HandleFunc("GET /v1/action-types/{id}", h.getByID)
	mux.HandleFunc("PATCH /v1/action-types/{id}", h.update)
	mux.HandleFunc("DELETE /v1/action-types/{id}", h.deleteByID)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusActionTypesAdmin) {
		return
	}
	var body dto.CreateActionTypeRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Name == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "name is required")
		return
	}
	orgID, ok := effectiveActionTypeOrg(r, body.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "action type org is not allowed for this principal")
		return
	}

	at := domain.ActionType{
		OrgID:              orgID,
		Name:               body.Name,
		Description:        body.Description,
		Category:           body.Category,
		RiskClass:          domain.RiskClass(body.RiskClass),
		Schema:             body.Schema,
		Reversible:         body.Reversible,
		RequiresBreakGlass: body.RequiresBreakGlass,
	}

	created, err := h.uc.Create(r.Context(), at)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create action type")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusActionTypesAdmin) {
		return
	}
	var (
		list []domain.ActionType
		err  error
	)
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		rawOrgID := r.URL.Query().Get("org_id")
		orgID := normalizeOrgPtr(&rawOrgID)
		if orgID != nil {
			list, err = h.uc.ListForOrg(r.Context(), orgID, true)
		} else {
			list, err = h.uc.List(r.Context())
		}
	} else {
		orgID, includeGlobal := listActionTypesOrg(r)
		if orgID == nil && !includeGlobal {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
			return
		}
		list, err = h.uc.ListForOrg(r.Context(), orgID, includeGlobal)
	}
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list action types")
		return
	}
	out := make([]dto.ActionTypeResponse, 0, len(list))
	for _, at := range list {
		out = append(out, toResponse(at))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusActionTypesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	at, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canAccessActionTypeOrg(r, at.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "action type org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toResponse(at))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusActionTypesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	at, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canWriteActionTypeOrg(r, at.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "action type org is not writable for this principal")
		return
	}
	var body dto.UpdateActionTypeRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.OrgID != nil {
		orgID, ok := effectiveActionTypeOrg(r, body.OrgID)
		if !ok {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "action type org is not allowed for this principal")
			return
		}
		at.OrgID = orgID
	}
	if body.Name != nil {
		at.Name = *body.Name
	}
	if body.Description != nil {
		at.Description = *body.Description
	}
	if body.Category != nil {
		at.Category = *body.Category
	}
	if body.RiskClass != nil {
		at.RiskClass = domain.RiskClass(*body.RiskClass)
	}
	if body.Schema != nil {
		at.Schema = *body.Schema
	}
	if body.Reversible != nil {
		at.Reversible = *body.Reversible
	}
	if body.RequiresBreakGlass != nil {
		at.RequiresBreakGlass = *body.RequiresBreakGlass
	}
	if body.Enabled != nil {
		at.Enabled = *body.Enabled
	}

	updated, err := h.uc.Update(r.Context(), at)
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) deleteByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusActionTypesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	at, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canWriteActionTypeOrg(r, at.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "action type org is not writable for this principal")
		return
	}
	if err := h.uc.DeleteByID(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toResponse(at domain.ActionType) dto.ActionTypeResponse {
	orgID := ""
	if at.OrgID != nil {
		orgID = *at.OrgID
	}
	return dto.ActionTypeResponse{
		ID:                 at.ID.String(),
		OrgID:              orgID,
		Name:               at.Name,
		Description:        at.Description,
		Category:           at.Category,
		RiskClass:          string(at.RiskClass),
		Schema:             at.Schema,
		Reversible:         at.Reversible,
		RequiresBreakGlass: at.RequiresBreakGlass,
		Enabled:            at.Enabled,
		CreatedAt:          at.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          at.UpdatedAt.Format(time.RFC3339),
	}
}

func writeError(w http.ResponseWriter, err error) {
	if domainerr.IsNotFound(err) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "action type not found")
		return
	}
	httpjson.WriteFlatInternalError(w, err, "action type operation failed")
}
