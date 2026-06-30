package jobroles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeAgentRead     = "companion:agents:read"
	scopeAgentAdmin    = "companion:agents:admin"
	scopeRuntimeAdmin  = "companion:runtime:admin"
	scopeCrossOrg      = "companion:cross_org"
	defaultProductSurf = "axis-console"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/job-roles", h.listJobRoles)
	mux.HandleFunc("GET /v1/job-roles/{job_role_id}", h.getJobRole)
	mux.HandleFunc("PUT /v1/job-roles/{job_role_id}", h.putJobRole)
	mux.HandleFunc("POST /v1/job-roles/{job_role_id}/archive", h.archiveJobRole)
	mux.HandleFunc("POST /v1/job-roles/{job_role_id}/restore", h.restoreJobRole)
	mux.HandleFunc("GET /v1/job-roles/{job_role_id}/versions", h.listVersions)
}

func (h *Handler) listJobRoles(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := jobRoleRequestContext(w, r)
	if !ok {
		return
	}
	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	roles, err := h.uc.ListJobRoles(r.Context(), orgID, surface, r.URL.Query().Get("lifecycle"), includeArchived)
	if err != nil {
		writeJobRoleError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"job_roles": roles, "data": roles})
}

func (h *Handler) getJobRole(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := jobRoleRequestContext(w, r)
	if !ok {
		return
	}
	role, err := h.uc.GetJobRole(r.Context(), orgID, surface, r.PathValue("job_role_id"))
	if err != nil {
		writeJobRoleError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, role)
}

func (h *Handler) putJobRole(w http.ResponseWriter, r *http.Request) {
	orgID, surface, actorID, ok := jobRoleRequestContext(w, r)
	if !ok {
		return
	}
	if !jobRoleWriteAllowed(w, r) {
		return
	}
	var role JobRole
	if err := json.NewDecoder(r.Body).Decode(&role); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	role.OrgID = orgID
	role.ProductSurface = surface
	role.JobRoleID = strings.TrimSpace(r.PathValue("job_role_id"))
	role.CreatedBy = actorID
	saved, err := h.uc.UpsertJobRole(r.Context(), role)
	if err != nil {
		writeJobRoleError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handler) archiveJobRole(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, h.uc.ArchiveJobRole)
}

func (h *Handler) restoreJobRole(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, h.uc.RestoreJobRole)
}

func (h *Handler) lifecycleAction(w http.ResponseWriter, r *http.Request, action func(context.Context, string, string, string, string) (JobRole, error)) {
	orgID, surface, actorID, ok := jobRoleRequestContext(w, r)
	if !ok {
		return
	}
	if !jobRoleWriteAllowed(w, r) {
		return
	}
	role, err := action(r.Context(), orgID, surface, r.PathValue("job_role_id"), actorID)
	if err != nil {
		writeJobRoleError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, role)
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := jobRoleRequestContext(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid limit")
			return
		}
		limit = parsed
	}
	versions, err := h.uc.ListVersions(r.Context(), orgID, surface, r.PathValue("job_role_id"), limit)
	if err != nil {
		writeJobRoleError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func jobRoleRequestContext(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "job role endpoints require authenticated admin context")
		return "", "", "", false
	}
	if !identityctx.HasAnyScope(r, scopeAgentRead, scopeAgentAdmin, scopeRuntimeAdmin, scopeCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing job role scope")
		return "", "", "", false
	}
	id := identityctx.FromRequest(r)
	orgID, allowed := identityctx.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeCrossOrg)
	if !allowed || strings.TrimSpace(orgID) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return "", "", "", false
	}
	surface := strings.TrimSpace(r.URL.Query().Get("product_surface"))
	if surface == "" {
		surface = strings.TrimSpace(id.ProductSurface)
	}
	if surface == "" {
		surface = defaultProductSurf
	}
	return orgID, surface, id.EffectiveActorID(), true
}

func jobRoleWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasAnyScope(r, scopeAgentAdmin, scopeRuntimeAdmin) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing job role write scope")
	return false
}

func writeJobRoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "job role not found")
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrConflict):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "job role operation failed")
	}
}
