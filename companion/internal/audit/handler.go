package audit

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeAuditRead    = "companion:audit:read"
	scopeRuntimeAdmin = "companion:runtime:admin"
	scopeAgentsAdmin  = "companion:agents:admin"
	scopeCrossOrg     = "companion:cross_org"
)

type Handler struct {
	repo *PostgresRepository
}

func NewHandler(repo *PostgresRepository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/audit-events", h.list)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "audit endpoints require authenticated context")
		return
	}
	if !identityctx.HasAnyScope(r, scopeAuditRead, scopeRuntimeAdmin, scopeAgentsAdmin, scopeCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing audit scope")
		return
	}
	tenantID := auditTenantID(r)
	if tenantID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "tenant_id is required")
		return
	}
	var resourceID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("resource_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid resource_id")
			return
		}
		resourceID = parsed
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
	events, err := h.repo.List(r.Context(), Filter{
		TenantID:     tenantID,
		ResourceType: r.URL.Query().Get("resource_type"),
		ResourceID:   resourceID,
		Limit:        limit,
	})
	if err != nil {
		writeAuditError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"audit_events": events, "data": events})
}

func auditTenantID(r *http.Request) string {
	for _, value := range []string{
		r.URL.Query().Get("tenant_id"),
		r.Header.Get("X-Tenant-ID"),
		r.Header.Get("X-Axis-Tenant-ID"),
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func writeAuditError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "audit event not found")
	default:
		httpjson.WriteFlatInternalError(w, err, "audit request failed")
	}
}
