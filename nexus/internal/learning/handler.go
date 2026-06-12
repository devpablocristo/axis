package learning

import (
	"context"
	"net/http"
	"strings"

	learningdto "github.com/devpablocristo/nexus/internal/learning/handler/dto"
	learningdomain "github.com/devpablocristo/nexus/internal/learning/usecases/domain"
	"github.com/devpablocristo/nexus/internal/orgctx"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

const defaultListLimit = 50

type learningUsecase interface {
	ListPendingProposals(ctx context.Context, limit int, orgID *string, allowAll bool) ([]learningdomain.PolicyProposal, error)
	GetProposalByID(ctx context.Context, id uuid.UUID) (learningdomain.PolicyProposal, error)
	AcceptProposal(ctx context.Context, id uuid.UUID, decidedBy string) (*uuid.UUID, error)
	DismissProposal(ctx context.Context, id uuid.UUID, decidedBy string) error
	AnalyzeAndPropose(ctx context.Context) (int, error)
	IngestProposal(ctx context.Context, candidate learningdomain.PolicyProposal) (learningdomain.PolicyProposal, error)
}

type Handler struct {
	uc learningUsecase
}

func NewHandler(uc learningUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/learning/proposals", h.listProposals)
	mux.HandleFunc("POST /v1/learning/proposals", h.createProposal)
	mux.HandleFunc("GET /v1/learning/proposals/{id}", h.getProposal)
	mux.HandleFunc("POST /v1/learning/proposals/{id}/accept", h.accept)
	mux.HandleFunc("POST /v1/learning/proposals/{id}/dismiss", h.dismiss)
	mux.HandleFunc("POST /v1/learning/analyze", h.analyze)
}

func (h *Handler) listProposals(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusLearningRead, scopeNexusLearningAdmin) {
		return
	}
	orgID, allowAll, ok := proposalOrgScope(r)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	list, err := h.uc.ListPendingProposals(r.Context(), defaultListLimit, orgID, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list proposals failed")
		return
	}
	out := make([]learningdto.ProposalResponse, 0, len(list))
	for _, p := range list {
		out = append(out, toProposalResponse(p))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getProposal(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusLearningRead, scopeNexusLearningAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	p, err := h.uc.GetProposalByID(r.Context(), id)
	if err != nil {
		writeLearningUsecaseError(w, err)
		return
	}
	if !canAccessProposalOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "proposal org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toProposalResponse(p))
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusLearningAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	var body learningdto.ProposalDecisionRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	p, err := h.uc.GetProposalByID(r.Context(), id)
	if err != nil {
		writeLearningUsecaseError(w, err)
		return
	}
	if !canAccessProposalOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "proposal org is not allowed for this principal")
		return
	}
	policyID, err := h.uc.AcceptProposal(r.Context(), id, decisionActorID(r, body.DecidedBy))
	if err != nil {
		writeLearningUsecaseError(w, err)
		return
	}
	resp := map[string]any{"status": "accepted"}
	if policyID != nil {
		resp["policy_id"] = policyID.String()
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusLearningAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	var body learningdto.ProposalDecisionRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	p, err := h.uc.GetProposalByID(r.Context(), id)
	if err != nil {
		writeLearningUsecaseError(w, err)
		return
	}
	if !canAccessProposalOrg(r, p) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "proposal org is not allowed for this principal")
		return
	}
	if err := h.uc.DismissProposal(r.Context(), id, decisionActorID(r, body.DecidedBy)); err != nil {
		writeLearningUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]string{"status": "dismissed"})
}

func (h *Handler) analyze(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusLearningAdmin) {
		return
	}
	count, err := h.uc.AnalyzeAndPropose(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "analyze failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"proposals_created": count})
}

func (h *Handler) createProposal(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusLearningPropose, scopeNexusLearningAdmin) {
		return
	}
	var body learningdto.ProposalCreateRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	orgID, ok := bindProposalOrgToPrincipal(r, body.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "proposal org is not allowed for this principal")
		return
	}
	candidate := learningdomain.PolicyProposal{
		OrgID:               orgID,
		ProposedName:        strings.TrimSpace(body.ProposedName),
		ProposedDescription: strings.TrimSpace(body.ProposedDescription),
		ProposedExpression:  strings.TrimSpace(body.ProposedExpression),
		ProposedEffect:      strings.TrimSpace(body.ProposedEffect),
		ProposedActionType:  body.ProposedActionType,
		ProposedPriority:    body.ProposedPriority,
		PatternSummary:      body.PatternSummary,
		Confidence:          body.Confidence,
		SampleSize:          body.SampleSize,
		TimeWindow:          body.TimeWindow,
	}
	saved, err := h.uc.IngestProposal(r.Context(), candidate)
	if err != nil {
		if domainerr.IsConflict(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
			return
		}
		// Validaciones del usecase devuelven errores planos: tratar como 400.
		msg := err.Error()
		if strings.Contains(msg, "is required") {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", msg)
			return
		}
		httpjson.WriteFlatInternalError(w, err, "create proposal failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, toProposalResponse(saved))
}

// toProposalResponse convierte entidad de dominio a DTO HTTP.
func toProposalResponse(p learningdomain.PolicyProposal) learningdto.ProposalResponse {
	resp := learningdto.ProposalResponse{
		ID:                  p.ID.String(),
		ProposedName:        p.ProposedName,
		ProposedDescription: p.ProposedDescription,
		ProposedExpression:  p.ProposedExpression,
		ProposedEffect:      p.ProposedEffect,
		ProposedActionType:  p.ProposedActionType,
		ProposedPriority:    p.ProposedPriority,
		PatternSummary:      p.PatternSummary,
		Confidence:          p.Confidence,
		SampleSize:          p.SampleSize,
		TimeWindow:          p.TimeWindow,
		Status:              string(p.Status),
		DecidedBy:           p.DecidedBy,
		CreatedAt:           p.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if p.OrgID != nil {
		resp.OrgID = strings.TrimSpace(*p.OrgID)
	}
	if p.DecidedAt != nil {
		s := p.DecidedAt.Format("2006-01-02T15:04:05Z")
		resp.DecidedAt = &s
	}
	if p.PolicyID != nil {
		s := p.PolicyID.String()
		resp.PolicyID = &s
	}
	return resp
}

func writeLearningUsecaseError(w http.ResponseWriter, err error) {
	if domainerr.IsConflict(err) {
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "proposal is not pending")
		return
	}
	if domainerr.IsNotFound(err) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "proposal not found")
		return
	}
	httpjson.WriteFlatInternalError(w, err, "learning operation failed")
}

func decisionActorID(r *http.Request, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	return strings.TrimSpace(r.Header.Get("X-User-ID"))
}

func proposalOrgScope(r *http.Request) (*string, bool, bool) {
	if identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		// cross_org puede acotar la vista a un org puntual (X-Org-ID inbound,
		// preservado en orgctx antes del rebind del middleware de authn).
		if orgID := orgctx.Narrowed(r, r.Header.Get("X-Org-ID")); orgID != "" {
			return &orgID, false, true
		}
		return nil, true, true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID != "" {
		return &orgID, false, true
	}
	if identityhttp.HasNoAuthContext(r) {
		return nil, true, true
	}
	return nil, false, false
}

func bindProposalOrgToPrincipal(r *http.Request, requested string) (*string, bool) {
	requested = strings.TrimSpace(requested)
	principalOrg := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if principalOrg != "" {
		if requested != "" && requested != principalOrg {
			return nil, false
		}
		return &principalOrg, true
	}
	if requested == "" {
		if identityhttp.HasNoAuthContext(r) {
			return nil, true
		}
		return nil, false
	}
	if identityhttp.HasAnyScope(r, scopeNexusCrossOrg) || identityhttp.HasNoAuthContext(r) {
		return &requested, true
	}
	return nil, false
}

func canAccessProposalOrg(r *http.Request, p learningdomain.PolicyProposal) bool {
	if identityhttp.HasAnyScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID != "" {
		return p.OrgID != nil && strings.TrimSpace(*p.OrgID) == orgID
	}
	if identityhttp.HasNoAuthContext(r) {
		return true
	}
	return false
}
