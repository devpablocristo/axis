package httpadapter

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/virployees"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

const (
	scopeVirployeeRead      = "companion:virployees:read"
	scopeVirployeeWrite     = "companion:virployees:write"
	scopeVirployeeAdmin     = "companion:virployees:admin"
	scopeAxisVirployeeRead  = "axis:virployees:read"
	scopeAxisVirployeeWrite = "axis:virployees:write"
	scopeAxisVirployeeAdmin = "axis:virployees:admin"
	scopeRuntimeAdmin       = "companion:runtime:admin"
	scopeCrossOrg           = "companion:cross_org"
	defaultSurface          = "axis-console"
)

type Handler struct {
	uc *virployees.Usecases
}

func NewHandler(uc *virployees.Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/virployees", h.listVirployees)
	mux.HandleFunc("POST /v1/virployees", h.createVirployee)
	mux.HandleFunc("GET /v1/virployees/{virployee_id}", h.getVirployee)
	mux.HandleFunc("PATCH /v1/virployees/{virployee_id}", h.updateVirployee)
	mux.HandleFunc("POST /v1/virployees/{virployee_id}/status", h.setVirployeeStatus)
}

func (h *Handler) listVirployees(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := virployeeRequestContext(w, r, scopeVirployeeRead, scopeVirployeeAdmin, scopeAxisVirployeeRead, scopeAxisVirployeeAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	items, err := h.uc.ListVirployees(r.Context(), tenantID.String(), orgID, surface, r.URL.Query().Get("lifecycle"))
	if err != nil {
		writeVirployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"virployees": items, "data": items})
}

func (h *Handler) createVirployee(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := virployeeRequestContext(w, r, scopeVirployeeWrite, scopeVirployeeAdmin, scopeAxisVirployeeWrite, scopeAxisVirployeeAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body virployees.Virployee
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	body.TenantID = tenantID
	body.OrgID = orgID
	body.ProductSurface = surface
	created, err := h.uc.CreateVirployee(r.Context(), body, actorID)
	if err != nil {
		writeVirployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) getVirployee(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := virployeeRequestContext(w, r, scopeVirployeeRead, scopeVirployeeAdmin, scopeAxisVirployeeRead, scopeAxisVirployeeAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	item, err := h.uc.GetVirployee(r.Context(), tenantID.String(), orgID, surface, r.PathValue("virployee_id"))
	if err != nil {
		writeVirployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) updateVirployee(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := virployeeRequestContext(w, r, scopeVirployeeWrite, scopeVirployeeAdmin, scopeAxisVirployeeWrite, scopeAxisVirployeeAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body virployees.Virployee
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	virployeeID, err := uuid.Parse(strings.TrimSpace(r.PathValue("virployee_id")))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid virployee_id")
		return
	}
	body.VirployeeID = virployeeID
	body.TenantID = tenantID
	body.OrgID = orgID
	body.ProductSurface = surface
	updated, err := h.uc.UpdateVirployee(r.Context(), body, actorID)
	if err != nil {
		writeVirployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) setVirployeeStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := virployeeRequestContext(w, r, scopeVirployeeWrite, scopeVirployeeAdmin, scopeAxisVirployeeWrite, scopeAxisVirployeeAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body struct {
		Status virployees.VirployeeStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	updated, err := h.uc.SetVirployeeStatus(r.Context(), tenantID.String(), orgID, surface, r.PathValue("virployee_id"), body.Status, actorID)
	if err != nil {
		writeVirployeeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func virployeeRequestContext(w http.ResponseWriter, r *http.Request, scopes ...string) (uuid.UUID, string, string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "virployee endpoints require authenticated context")
		return uuid.Nil, "", "", "", false
	}
	if !identityctx.HasAnyScope(r, scopes...) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing virployee scope")
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
	return uuid.Nil, virployees.ErrValidation
}

func writeVirployeeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, virployees.ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, virployees.ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "virployee not found")
	case errors.Is(err, virployees.ErrConflict):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "virployee request failed")
	}
}
