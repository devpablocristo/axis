package dashboard

import (
	"context"
	"net/http"

	dashboarddto "github.com/devpablocristo/nexus/internal/dashboard/handler/dto"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const maxListLimit = 1000

type requestLister interface {
	List(ctx context.Context, status, actionType string, limit int, orgID *string, allowAll bool) ([]requestdomain.Request, error)
}

type Handler struct {
	requests requestLister
}

func NewHandler(requests requestLister) *Handler {
	return &Handler{requests: requests}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/metrics/summary", h.summary)
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusDashboardRead) {
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "7d"
	}
	// Tenancy: por default solo se ven métricas del org del principal. Para
	// vista cross-org se requiere scope nexus:cross_org explícito; sin él el
	// dashboard de un org NO puede ver agregados de otros (V7 lockdown).
	// Sin contexto de auth (dev/test) se permite cross-org como antes.
	var orgFilter *string
	allowAll := false
	switch {
	case requestHasNoAuthContext(r):
		allowAll = true
	case requestHasScope(r, scopeNexusCrossOrg):
		// Admin global: si pasa X-Org-ID se filtra ese org; sin header ve todo.
		orgFilter = principalOrgID(r)
		allowAll = orgFilter == nil
	default:
		// Caller scopeado: solo su propio org. Sin org_id fallamos cerrado.
		orgFilter = principalOrgID(r)
		if orgFilter == nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
			return
		}
	}
	all, err := h.requests.List(r.Context(), "", "", maxListLimit, orgFilter, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "dashboard query failed")
		return
	}

	var allowed, denied, pendingApproval, approved, rejected int
	for _, req := range all {
		switch req.Status {
		case requestdomain.StatusAllowed:
			allowed++
		case requestdomain.StatusDenied:
			denied++
		case requestdomain.StatusPendingApproval:
			pendingApproval++
		case requestdomain.StatusApproved, requestdomain.StatusExecuted:
			approved++
		case requestdomain.StatusRejected:
			rejected++
		}
	}

	httpjson.WriteJSON(w, http.StatusOK, dashboarddto.SummaryResponse{
		Period:          period,
		TotalRequests:   len(all),
		Allowed:         allowed,
		Denied:          denied,
		PendingApproval: pendingApproval,
		Approved:        approved,
		Rejected:        rejected,
	})
}
