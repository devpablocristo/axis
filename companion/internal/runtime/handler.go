package runtime

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

const defaultTraceListLimit = 50

// traceUsecase es la superficie del repo expuesta al handler.
type traceUsecase interface {
	GetByID(ctx context.Context, runID uuid.UUID) (StoredTrace, error)
	ListByOrg(ctx context.Context, orgID string, limit int) ([]StoredTrace, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]StoredTrace, error)
}

// TraceHandler HTTP adapter para consultar run traces persistidos.
type TraceHandler struct {
	repo traceUsecase
}

// NewTraceHandler crea el handler.
func NewTraceHandler(repo traceUsecase) *TraceHandler {
	return &TraceHandler{repo: repo}
}

// Register registra rutas en el mux.
func (h *TraceHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/run-traces/{run_id}", h.getByID)
	mux.HandleFunc("GET /v1/run-traces", h.list)
}

func (h *TraceHandler) getByID(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(r.PathValue("run_id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid run_id")
		return
	}
	trace, err := h.repo.GetByID(r.Context(), runID)
	if err != nil {
		if errors.Is(err, ErrTraceNotFound) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "run trace not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get run trace failed")
		return
	}
	if !canAccessTraceOrg(r, trace.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "run trace belongs to a different org")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, trace)
}

func (h *TraceHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	taskIDRaw := strings.TrimSpace(q.Get("task_id"))

	if taskIDRaw != "" {
		taskID, err := uuid.Parse(taskIDRaw)
		if err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid task_id")
			return
		}
		traces, err := h.repo.ListByTask(r.Context(), taskID)
		if err != nil {
			httpjson.WriteFlatInternalError(w, err, "list run traces by task failed")
			return
		}
		filtered := filterTracesByOrg(r, traces)
		httpjson.WriteJSON(w, http.StatusOK, map[string]any{"traces": filtered})
		return
	}

	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "X-Org-ID header is required when task_id is not provided")
		return
	}
	limit := defaultTraceListLimit
	if rawLimit := q.Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	traces, err := h.repo.ListByOrg(r.Context(), orgID, limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list run traces by org failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"traces": traces})
}

// canAccessTraceOrg implementa la regla de visibilidad por org. Si el caller
// no envía X-Org-ID, se permite (compatibilidad con auth modes que aún no
// resuelven org_id en el header). Si lo envía, debe coincidir.
func canAccessTraceOrg(r *http.Request, traceOrgID string) bool {
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		return true
	}
	return strings.TrimSpace(traceOrgID) == orgID
}

func filterTracesByOrg(r *http.Request, traces []StoredTrace) []StoredTrace {
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		return traces
	}
	out := make([]StoredTrace, 0, len(traces))
	for _, t := range traces {
		if strings.TrimSpace(t.OrgID) == orgID {
			out = append(out, t)
		}
	}
	return out
}
