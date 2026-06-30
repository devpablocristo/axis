package virtualemployees

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

const (
	scopeEmployeeRead  = "companion:agents:read"
	scopeEmployeeAdmin = "companion:agents:admin"
	scopeRuntimeAdmin  = "companion:runtime:admin"
	scopeCrossOrg      = "companion:cross_org"
	defaultSurface     = "axis-console"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/virtual-employees", h.listEmployees)
	mux.HandleFunc("POST /v1/virtual-employees", h.createEmployee)
	mux.HandleFunc("GET /v1/virtual-employees/{employee_id}", h.getEmployee)
	mux.HandleFunc("PATCH /v1/virtual-employees/{employee_id}", h.updateEmployee)
	mux.HandleFunc("POST /v1/virtual-employees/{employee_id}/status", h.setEmployeeStatus)
}

func (h *Handler) listEmployees(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := employeeRequestContext(w, r, scopeEmployeeRead, scopeEmployeeAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	items, err := h.uc.ListEmployees(r.Context(), tenantID.String(), orgID, surface, r.URL.Query().Get("lifecycle"))
	if err != nil {
		writeEmployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"virtual_employees": items, "data": items})
}

func (h *Handler) createEmployee(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := employeeRequestContext(w, r, scopeEmployeeAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body VirtualEmployee
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	body.TenantID = tenantID
	body.OrgID = orgID
	body.ProductSurface = surface
	created, err := h.uc.CreateEmployee(r.Context(), body, actorID)
	if err != nil {
		writeEmployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) getEmployee(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := employeeRequestContext(w, r, scopeEmployeeRead, scopeEmployeeAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	item, err := h.uc.GetEmployee(r.Context(), tenantID.String(), orgID, surface, r.PathValue("employee_id"))
	if err != nil {
		writeEmployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) updateEmployee(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := employeeRequestContext(w, r, scopeEmployeeAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body VirtualEmployee
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	employeeID, err := uuid.Parse(strings.TrimSpace(r.PathValue("employee_id")))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid employee_id")
		return
	}
	body.EmployeeID = employeeID
	body.TenantID = tenantID
	body.OrgID = orgID
	body.ProductSurface = surface
	updated, err := h.uc.UpdateEmployee(r.Context(), body, actorID)
	if err != nil {
		writeEmployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) setEmployeeStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := employeeRequestContext(w, r, scopeEmployeeAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body struct {
		Status EmployeeStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	updated, err := h.uc.SetEmployeeStatus(r.Context(), tenantID.String(), orgID, surface, r.PathValue("employee_id"), body.Status, actorID)
	if err != nil {
		writeEmployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func employeeRequestContext(w http.ResponseWriter, r *http.Request, scopes ...string) (uuid.UUID, string, string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "virtual employee endpoints require authenticated context")
		return uuid.Nil, "", "", "", false
	}
	if !identityctx.HasAnyScope(r, scopes...) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing virtual employee scope")
		return uuid.Nil, "", "", "", false
	}
	id := identityctx.FromRequest(r)
	orgID, allowed := identityctx.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeCrossOrg)
	if !allowed || strings.TrimSpace(orgID) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return uuid.Nil, "", "", "", false
	}
	surface := strings.TrimSpace(r.URL.Query().Get("product_surface"))
	if surface == "" {
		surface = strings.TrimSpace(id.ProductSurface)
	}
	if surface == "" {
		surface = defaultSurface
	}
	tenantID, err := requestTenantID(r)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "tenant_id UUID is required")
		return uuid.Nil, "", "", "", false
	}
	return tenantID, orgID, surface, id.EffectiveActorID(), true
}

func requestTenantID(r *http.Request) (uuid.UUID, error) {
	for _, raw := range []string{
		r.URL.Query().Get("tenant_id"),
		r.Header.Get("X-Tenant-ID"),
		r.Header.Get("X-Axis-Tenant-ID"),
	} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		return uuid.Parse(raw)
	}
	return uuid.Nil, ErrValidation
}

func writeEmployeeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "virtual employee not found")
	case errors.Is(err, ErrConflict):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "virtual employee request failed")
	}
}
