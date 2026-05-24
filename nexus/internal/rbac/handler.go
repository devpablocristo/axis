package rbac

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	dto "github.com/devpablocristo/nexus/internal/rbac/handler/dto"
	domain "github.com/devpablocristo/nexus/internal/rbac/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

type rbacUsecase interface {
	Grant(ctx context.Context, a domain.Assignment) (domain.Assignment, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Assignment, error)
	List(ctx context.Context, filter ListFilter) ([]domain.Assignment, error)
	Check(ctx context.Context, orgID, userID string, role domain.Role) (bool, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type Handler struct {
	uc rbacUsecase
}

func NewHandler(uc rbacUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/rbac/assignments", h.grant)
	mux.HandleFunc("GET /v1/rbac/assignments", h.list)
	mux.HandleFunc("GET /v1/rbac/assignments/{id}", h.getByID)
	mux.HandleFunc("DELETE /v1/rbac/assignments/{id}", h.deleteByID)
	mux.HandleFunc("POST /v1/rbac/assignments/{id}/archive", h.archive)
	mux.HandleFunc("POST /v1/rbac/assignments/{id}/restore", h.restore)
	mux.HandleFunc("GET /v1/rbac/check", h.check)
}

func (h *Handler) grant(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	var body dto.GrantAssignmentRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if !canAccessOrg(r, body.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org not allowed for this principal")
		return
	}
	a := domain.Assignment{
		OrgID:     body.OrgID,
		UserID:    body.UserID,
		Role:      domain.Role(body.Role),
		GrantedBy: principalUserID(r),
	}
	created, err := h.uc.Grant(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	q := r.URL.Query()
	filter := ListFilter{
		OrgID:          q.Get("org_id"),
		UserID:         q.Get("user_id"),
		Role:           q.Get("role"),
		IncludeRevoked: q.Get("include_revoked") == "true",
	}
	// Si el principal no tiene cross_org, fuerza el filtro a su propia org.
	if !identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		ownOrg := principalOrgID(r)
		if ownOrg == "" {
			httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": []dto.AssignmentResponse{}})
			return
		}
		filter.OrgID = ownOrg
	}
	list, err := h.uc.List(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]dto.AssignmentResponse, 0, len(list))
	for _, a := range list {
		out = append(out, toResponse(a))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	a, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canAccessOrg(r, a.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toResponse(a))
}

func (h *Handler) deleteByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	a, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canAccessOrg(r, a.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org not allowed for this principal")
		return
	}
	if err := h.uc.DeleteByID(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	a, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canAccessOrg(r, a.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org not allowed for this principal")
		return
	}
	if err := h.uc.Revoke(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	a, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !canAccessOrg(r, a.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org not allowed for this principal")
		return
	}
	if err := h.uc.Restore(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) check(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRBACAdmin) {
		return
	}
	q := r.URL.Query()
	orgID := q.Get("org_id")
	userID := q.Get("user_id")
	role := q.Get("role")
	if !canAccessOrg(r, orgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org not allowed for this principal")
		return
	}
	granted, err := h.uc.Check(r.Context(), orgID, userID, domain.Role(role))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.CheckResponse{
		OrgID:   orgID,
		UserID:  userID,
		Role:    role,
		Granted: granted,
	})
}

func toResponse(a domain.Assignment) dto.AssignmentResponse {
	resp := dto.AssignmentResponse{
		ID:        a.ID.String(),
		OrgID:     a.OrgID,
		UserID:    a.UserID,
		Role:      string(a.Role),
		GrantedBy: a.GrantedBy,
		GrantedAt: a.GrantedAt.Format(time.RFC3339),
	}
	if a.RevokedAt != nil {
		s := a.RevokedAt.Format(time.RFC3339)
		resp.RevokedAt = &s
	}
	return resp
}

func principalUserID(r *http.Request) string {
	return r.Header.Get("X-User-ID")
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case domainerr.IsValidation(err):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case domainerr.IsNotFound(err):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "assignment not found")
	case domainerr.IsConflict(err):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "assignment already exists")
	default:
		httpjson.WriteFlatInternalError(w, err, "rbac operation failed")
	}
}
