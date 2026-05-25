package runtime

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

type ObservabilityHandler struct {
	repo ObservabilityRepository
}

const (
	scopeObservabilityRead = "companion:observability:read"
	scopeCostsRead         = "companion:costs:read"
)

func NewObservabilityHandler(repo ObservabilityRepository) *ObservabilityHandler {
	return &ObservabilityHandler{repo: repo}
}

func (h *ObservabilityHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/run-traces/{run_id}/replay", h.replayRun)
	mux.HandleFunc("GET /v1/observability/events", h.listEvents)
	mux.HandleFunc("GET /v1/runtime/costs", h.costs)
}

func (h *ObservabilityHandler) replayRun(w http.ResponseWriter, r *http.Request) {
	if !requireObservabilityScope(w, r, scopeObservabilityRead, scopeCompanionRuntimeAdmin, scopeCompanionCrossOrg) {
		return
	}
	runID, err := uuid.Parse(strings.TrimSpace(r.PathValue("run_id")))
	if err != nil || runID == uuid.Nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid run_id")
		return
	}
	replay, err := h.repo.GetRunReplay(r.Context(), runID)
	if err != nil {
		if errors.Is(err, ErrTraceNotFound) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "run trace not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get run replay failed")
		return
	}
	if !canAccessTraceOrg(r, replay.Trace.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "run trace belongs to a different org")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, replay)
}

func (h *ObservabilityHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	if !requireObservabilityScope(w, r, scopeObservabilityRead, scopeCompanionRuntimeAdmin, scopeCompanionCrossOrg) {
		return
	}
	q := r.URL.Query()
	limit := 100
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	var runID *uuid.UUID
	if raw := strings.TrimSpace(q.Get("run_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil || parsed == uuid.Nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid run_id")
			return
		}
		runID = &parsed
	}
	orgID := identityctx.FromRequest(r).CustomerOrgID
	if orgID == "" && runID == nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context or run_id is required")
		return
	}
	events, err := h.repo.ListObservabilityEvents(r.Context(), orgID, runID, limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list observability events failed")
		return
	}
	filtered := make([]ObservabilityEvent, 0, len(events))
	for _, event := range events {
		if canAccessTraceOrg(r, event.OrgID) {
			filtered = append(filtered, event)
		}
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"events": filtered})
}

func (h *ObservabilityHandler) costs(w http.ResponseWriter, r *http.Request) {
	if !requireObservabilityScope(w, r, scopeCostsRead, scopeCompanionRuntimeAdmin, scopeCompanionCrossOrg) {
		return
	}
	ledger, ok := h.repo.(CostLedger)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusNotImplemented, "NOT_CONFIGURED", "cost ledger is not configured")
		return
	}
	orgID := strings.TrimSpace(identityctx.FromRequest(r).CustomerOrgID)
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return
	}
	if !canAccessTraceOrg(r, orgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "cost report org is not allowed for this principal")
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	summary, err := ledger.GetCostSummary(r.Context(), orgID, period, limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "get cost summary failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, summary)
}

func requireObservabilityScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing observability scope")
	return false
}
