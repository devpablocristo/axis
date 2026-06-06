package ops

import (
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
