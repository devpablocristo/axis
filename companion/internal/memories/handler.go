package memories

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
	scopeMemoryRead   = "companion:memory:read"
	scopeMemoryWrite  = "companion:memory:write"
	scopeMemoryAdmin  = "companion:memory:admin"
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
	mux.HandleFunc("GET /v1/memories", h.listMemories)
	mux.HandleFunc("POST /v1/memories", h.createMemory)
	mux.HandleFunc("GET /v1/memories/{memory_id}", h.getMemory)
	mux.HandleFunc("PATCH /v1/memories/{memory_id}", h.updateMemory)
	mux.HandleFunc("POST /v1/memories/{memory_id}/status", h.setMemoryStatus)
	mux.HandleFunc("GET /v1/memories/{memory_id}/entries", h.listEntries)
	mux.HandleFunc("POST /v1/memories/{memory_id}/entries", h.createEntry)
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := memoryRequestContext(w, r, scopeMemoryRead, scopeMemoryAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	items, err := h.uc.ListMemories(r.Context(), tenantID.String(), orgID, surface, r.URL.Query().Get("lifecycle"))
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"memories": items, "data": items})
}

func (h *Handler) createMemory(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := memoryRequestContext(w, r, scopeMemoryWrite, scopeMemoryAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body Memory
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	if body.TenantID == uuid.Nil {
		body.TenantID = tenantID
	}
	body.OrgID = orgID
	body.ProductSurface = surface
	created, err := h.uc.CreateMemory(r.Context(), body, actorID)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) getMemory(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := memoryRequestContext(w, r, scopeMemoryRead, scopeMemoryAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	item, err := h.uc.GetMemory(r.Context(), tenantID.String(), orgID, surface, r.PathValue("memory_id"))
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) updateMemory(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := memoryRequestContext(w, r, scopeMemoryWrite, scopeMemoryAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body Memory
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	memoryID, err := uuid.Parse(strings.TrimSpace(r.PathValue("memory_id")))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid memory_id")
		return
	}
	body.MemoryID = memoryID
	body.TenantID = tenantID
	body.OrgID = orgID
	body.ProductSurface = surface
	updated, err := h.uc.UpdateMemory(r.Context(), body, actorID)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) setMemoryStatus(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := memoryRequestContext(w, r, scopeMemoryWrite, scopeMemoryAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body struct {
		Status MemoryStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	updated, err := h.uc.SetMemoryStatus(r.Context(), tenantID.String(), orgID, surface, r.PathValue("memory_id"), body.Status, actorID)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) listEntries(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, _, ok := memoryRequestContext(w, r, scopeMemoryRead, scopeMemoryAdmin, scopeRuntimeAdmin, scopeCrossOrg)
	if !ok {
		return
	}
	entries, err := h.uc.ListEntries(r.Context(), tenantID.String(), orgID, surface, r.PathValue("memory_id"))
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"entries": entries, "data": entries})
}

func (h *Handler) createEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, orgID, surface, actorID, ok := memoryRequestContext(w, r, scopeMemoryWrite, scopeMemoryAdmin, scopeRuntimeAdmin)
	if !ok {
		return
	}
	var body MemoryEntry
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	created, err := h.uc.CreateEntry(r.Context(), tenantID.String(), orgID, surface, r.PathValue("memory_id"), body, actorID)
	if err != nil {
		writeMemoryError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, created)
}

func memoryRequestContext(w http.ResponseWriter, r *http.Request, scopes ...string) (uuid.UUID, string, string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory endpoints require authenticated context")
		return uuid.Nil, "", "", "", false
	}
	if !identityctx.HasAnyScope(r, scopes...) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing memory scope")
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

func writeMemoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory not found")
	case errors.Is(err, ErrConflict):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "memory request failed")
	}
}
