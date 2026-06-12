package nexus_assist

import (
	"context"
	"net/http"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"

	gadto "github.com/devpablocristo/companion/internal/nexus_assist/handler/dto"
)

// proposerSurface es la superficie del Proposer expuesta al handler.
type proposerSurface interface {
	AnalyzeAndPropose(ctx context.Context, orgID string) (analyzed, submitted int, errs []string, err error)
}

// contextualizerSurface es la superficie del Contextualizer expuesta al handler.
type contextualizerSurface interface {
	Explain(ctx context.Context, requestID string) (summary string, degraded bool, err error)
}

// Handler expone /companion/v1/nexus-assist/* sobre Proposer + Contextualizer.
type Handler struct {
	proposer       proposerSurface
	contextualizer contextualizerSurface
}

const (
	scopeCompanionNexusAssistRead  = "companion:nexus-assist:read"
	scopeCompanionNexusAssistAdmin = "companion:nexus-assist:admin"
)

func NewHandler(p proposerSurface, c contextualizerSurface) *Handler {
	return &Handler{proposer: p, contextualizer: c}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/nexus-assist/propose", h.propose)
	mux.HandleFunc("GET /v1/nexus-assist/explain/{request_id}", h.explain)
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionNexusAssistAdmin) {
		return
	}
	if h.proposer == nil {
		httpjson.WriteFlatError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "proposer not configured")
		return
	}
	orgID := identityctx.FromRequest(r).CustomerOrgID
	if orgID == "" {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "nexus assist propose requires org context")
		return
	}
	analyzed, submitted, errs, err := h.proposer.AnalyzeAndPropose(r.Context(), orgID)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "analyze and propose failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, gadto.ProposeResponse{
		PatternsAnalyzed:   analyzed,
		ProposalsSubmitted: submitted,
		Errors:             errs,
	})
}

func (h *Handler) explain(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionNexusAssistRead, scopeCompanionNexusAssistAdmin) {
		return
	}
	if h.contextualizer == nil {
		httpjson.WriteFlatError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "contextualizer not configured")
		return
	}
	requestID := strings.TrimSpace(r.PathValue("request_id"))
	if requestID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "request_id is required")
		return
	}
	summary, degraded, err := h.contextualizer.Explain(r.Context(), requestID)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "explain request failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, gadto.ExplainResponse{
		RequestID: requestID,
		Summary:   summary,
		Degraded:  degraded,
	})
}

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing required scope")
	return false
}
