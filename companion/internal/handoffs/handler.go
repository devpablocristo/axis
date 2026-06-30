package handoffs

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeHandoffRead  = "companion:agents:read"
	scopeHandoffAdmin = "companion:agents:admin"
	scopeRuntimeAdmin = "companion:runtime:admin"
	scopeCrossOrg     = "companion:cross_org"
	defaultSurface    = "axis-console"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/handoffs", h.list)
	mux.HandleFunc("POST /v1/handoffs", h.create)
	mux.HandleFunc("GET /v1/handoffs/{handoff_id}", h.get)
	mux.HandleFunc("PATCH /v1/handoffs/{handoff_id}", h.update)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := handoffRequestContext(w, r, scopeHandoffRead, scopeHandoffAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid limit")
			return
		}
		limit = parsed
	}
	items, err := h.uc.List(r.Context(), tenantID.String(), orgID, surface, Status(r.URL.Query().Get("status")), limit)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"handoffs": items, "data": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := handoffRequestContext(w, r, scopeHandoffAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body Handoff
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	body.TenantID = tenantID
	body.OrgID = orgID
	body.ProductSurface = surface
	body.CreatedBy = actorID
	created, err := h.uc.Create(r.Context(), body)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := handoffRequestContext(w, r, scopeHandoffRead, scopeHandoffAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	item, err := h.uc.Get(r.Context(), tenantID.String(), orgID, surface, r.PathValue("handoff_id"))
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := handoffRequestContext(w, r, scopeHandoffAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body Handoff
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	body.CreatedBy = actorID
	updated, err := h.uc.Update(r.Context(), tenantID.String(), orgID, surface, r.PathValue("handoff_id"), body)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func handoffRequestContext(w http.ResponseWriter, r *http.Request, scopes ...string) (uuid.UUID, string, string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "handoff endpoints require authenticated context")
		return uuid.Nil, "", "", "", false
	}
	if !identityctx.HasAnyScope(r, scopes...) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing handoff scope")
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
	tenantID, err := handoffTenantID(r)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "tenant_id UUID is required")
		return uuid.Nil, "", "", "", false
	}
	return tenantID, orgID, surface, id.EffectiveActorID(), true
}

func handoffTenantID(r *http.Request) (uuid.UUID, error) {
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

func writeHandoffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "handoff not found")
	case errors.Is(err, ErrConflict):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "handoff request failed")
	}
}
