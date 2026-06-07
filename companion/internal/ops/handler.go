package ops

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeOpsRead      = "companion:ops:read"
	scopeRuntimeAdmin = "companion:runtime:admin"
	scopeCrossOrg     = "companion:cross_org"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ops/console", h.console)
	mux.HandleFunc("GET /v1/ops/alerts", h.alerts)
	mux.HandleFunc("POST /v1/ops/alerts/dispatch", h.dispatchAlerts)
	mux.HandleFunc("GET /v1/ops/metrics", h.metrics)
	mux.HandleFunc("GET /v1/ops/slos", h.slos)
}

func (h *Handler) console(w http.ResponseWriter, r *http.Request) {
	q, ok := opsQuery(w, r)
	if !ok {
		return
	}
	console, err := h.uc.GetConsole(r.Context(), q)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "get ops console failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, console)
}

func (h *Handler) alerts(w http.ResponseWriter, r *http.Request) {
	q, ok := opsQuery(w, r)
	if !ok {
		return
	}
	alerts, err := h.uc.ListAlerts(r.Context(), q)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list ops alerts failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"alerts": alerts})
}

func (h *Handler) dispatchAlerts(w http.ResponseWriter, r *http.Request) {
	if !requireOpsAdminScope(w, r) {
		return
	}
	q, ok := parseOpsQuery(w, r)
	if !ok {
		return
	}
	result, err := h.uc.DispatchAlerts(r.Context(), q)
	if err != nil {
		if errors.Is(err, ErrAlertSinkNotConfigured) {
			httpjson.WriteFlatError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED", "ops alert webhook is not configured")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "dispatch ops alerts failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusAccepted, result)
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	q, ok := opsQuery(w, r)
	if !ok {
		return
	}
	metrics, err := h.uc.ListMetrics(r.Context(), q)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list ops metrics failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"metrics": metrics})
}

func (h *Handler) slos(w http.ResponseWriter, r *http.Request) {
	q, ok := opsQuery(w, r)
	if !ok {
		return
	}
	slos, err := h.uc.ListSLOs(r.Context(), q)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list ops slos failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"slos": slos})
}

func opsQuery(w http.ResponseWriter, r *http.Request) (Query, bool) {
	if !requireOpsScope(w, r) {
		return Query{}, false
	}
	return parseOpsQuery(w, r)
}

func parseOpsQuery(w http.ResponseWriter, r *http.Request) (Query, bool) {
	orgID, ok := identityctx.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeCrossOrg)
	if !ok || strings.TrimSpace(orgID) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return Query{}, false
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "limit must be a positive integer")
			return Query{}, false
		}
		limit = parsed
	}
	return Query{
		OrgID:          orgID,
		ProductSurface: strings.TrimSpace(r.URL.Query().Get("product_surface")),
		Period:         strings.TrimSpace(r.URL.Query().Get("period")),
		Limit:          limit,
	}, true
}

func requireOpsScope(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "ops endpoints require authenticated context")
		return false
	}
	if identityctx.HasAnyScope(r, scopeOpsRead, scopeRuntimeAdmin, scopeCrossOrg) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing ops scope")
	return false
}

func requireOpsAdminScope(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "ops endpoints require authenticated context")
		return false
	}
	if identityctx.HasAnyScope(r, scopeRuntimeAdmin, scopeCrossOrg) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing ops admin scope")
	return false
}
