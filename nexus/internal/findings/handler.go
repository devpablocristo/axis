package findings

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	findingdto "github.com/devpablocristo/nexus/internal/findings/handler/dto"
	domain "github.com/devpablocristo/nexus/internal/findings/usecases/domain"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

type findingUsecase interface {
	UpsertRule(ctx context.Context, in CreateRuleInput) (domain.FindingRule, error)
	GetRule(ctx context.Context, id uuid.UUID) (domain.FindingRule, error)
	ListRules(ctx context.Context, in ListRulesInput) ([]domain.FindingRule, error)
	UpdateRule(ctx context.Context, in UpdateRuleInput) (domain.FindingRule, error)
	ArchiveRule(ctx context.Context, orgID, actor string, id uuid.UUID) error
	RestoreRule(ctx context.Context, orgID, actor string, id uuid.UUID) error
	HardDeleteRule(ctx context.Context, orgID, actor string, id uuid.UUID) error
	SubmitFacts(ctx context.Context, in SubmitFactsInput) (SubmitFactsOutput, error)
	GetEvaluation(ctx context.Context, id uuid.UUID) (domain.FactEvaluation, error)
	ListFindings(ctx context.Context, in ListFindingsInput) ([]domain.Finding, error)
	GetFinding(ctx context.Context, id uuid.UUID) (domain.Finding, error)
	UpdateFindingStatus(ctx context.Context, id uuid.UUID, status domain.FindingStatus, note string) (domain.Finding, error)
}

type Handler struct {
	uc findingUsecase
}

func NewHandler(uc findingUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/finding-rules", h.upsertRule)
	mux.HandleFunc("GET /v1/finding-rules", h.listRules)
	mux.HandleFunc("GET /v1/finding-rules/archived", h.listArchivedRules)
	mux.HandleFunc("GET /v1/finding-rules/{id}", h.getRule)
	mux.HandleFunc("PATCH /v1/finding-rules/{id}", h.updateRule)
	mux.HandleFunc("POST /v1/finding-rules/{id}/archive", h.archiveRule)
	mux.HandleFunc("POST /v1/finding-rules/{id}/restore", h.restoreRule)
	mux.HandleFunc("DELETE /v1/finding-rules/{id}/hard", h.hardDeleteRule)
	mux.HandleFunc("POST /v1/fact-evaluations", h.submitFacts)
	mux.HandleFunc("GET /v1/fact-evaluations/{id}", h.getEvaluation)
	mux.HandleFunc("GET /v1/findings", h.listFindings)
	mux.HandleFunc("GET /v1/findings/{id}", h.getFinding)
	mux.HandleFunc("PATCH /v1/findings/{id}", h.updateFinding)
}

func (h *Handler) upsertRule(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsWrite) {
		return
	}
	var body findingdto.FindingRuleRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if !canWriteOwner(r, body.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "owner_system is not allowed for this principal")
		return
	}
	out, err := h.uc.UpsertRule(r.Context(), CreateRuleInput{
		OrgID:          resolveOrgID(r),
		OwnerSystem:    body.OwnerSystem,
		SourceSystem:   body.SourceSystem,
		FactType:       body.FactType,
		Code:           body.Code,
		Name:           body.Name,
		Description:    body.Description,
		Expression:     body.Expression,
		Severity:       body.Severity,
		Title:          body.Title,
		Message:        body.Message,
		Recommendation: body.Recommendation,
		Mode:           domain.RuleMode(body.Mode),
		Enabled:        body.Enabled,
		Priority:       body.Priority,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, ruleResponse(out))
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsRead) {
		return
	}
	out, err := h.uc.ListRules(r.Context(), ListRulesInput{
		OrgID:        resolveOrgID(r),
		OwnerSystem:  r.URL.Query().Get("owner_system"),
		SourceSystem: r.URL.Query().Get("source_system"),
		FactType:     r.URL.Query().Get("fact_type"),
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	resp := make([]findingdto.FindingRuleResponse, 0, len(out))
	for _, rule := range out {
		resp = append(resp, ruleResponse(rule))
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) listArchivedRules(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsRead) {
		return
	}
	out, err := h.uc.ListRules(r.Context(), ListRulesInput{
		OrgID:        resolveOrgID(r),
		OwnerSystem:  r.URL.Query().Get("owner_system"),
		SourceSystem: r.URL.Query().Get("source_system"),
		FactType:     r.URL.Query().Get("fact_type"),
		ArchivedOnly: true,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	resp := make([]findingdto.FindingRuleResponse, 0, len(out))
	for _, rule := range out {
		resp = append(resp, ruleResponse(rule))
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsRead) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	rule, err := h.uc.GetRule(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, rule.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "rule org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, ruleResponse(rule))
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsWrite) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	current, err := h.uc.GetRule(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, current.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "rule org is not allowed for this principal")
		return
	}
	var body findingdto.FindingRuleRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	owner := body.OwnerSystem
	if strings.TrimSpace(owner) == "" {
		owner = current.OwnerSystem
	}
	if !canWriteOwner(r, owner) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "owner_system is not allowed for this principal")
		return
	}
	out, err := h.uc.UpdateRule(r.Context(), UpdateRuleInput{
		ID:             id,
		OwnerSystem:    body.OwnerSystem,
		SourceSystem:   body.SourceSystem,
		FactType:       body.FactType,
		Code:           body.Code,
		Name:           body.Name,
		Description:    body.Description,
		Expression:     body.Expression,
		Severity:       body.Severity,
		Title:          body.Title,
		Message:        body.Message,
		Recommendation: body.Recommendation,
		Mode:           domain.RuleMode(body.Mode),
		Enabled:        body.Enabled,
		Priority:       body.Priority,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, ruleResponse(out))
}

func (h *Handler) archiveRule(w http.ResponseWriter, r *http.Request) {
	h.ruleMutation(w, r, h.uc.ArchiveRule)
}

func (h *Handler) restoreRule(w http.ResponseWriter, r *http.Request) {
	h.ruleMutation(w, r, h.uc.RestoreRule)
}

func (h *Handler) hardDeleteRule(w http.ResponseWriter, r *http.Request) {
	h.ruleMutation(w, r, h.uc.HardDeleteRule)
}

func (h *Handler) ruleMutation(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string, uuid.UUID) error) {
	if !requireScope(w, r, scopeNexusFindingsWrite) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	current, err := h.uc.GetRule(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, current.OrgID) || !canWriteOwner(r, current.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "rule is not allowed for this principal")
		return
	}
	if err := fn(r.Context(), current.OrgID, identityhttp.FromRequest(r).Actor, id); err != nil {
		writeUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) submitFacts(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsWrite) {
		return
	}
	var body findingdto.SubmitFactsRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if !canWriteOwner(r, body.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "owner_system is not allowed for this principal")
		return
	}
	out, err := h.uc.SubmitFacts(r.Context(), SubmitFactsInput{
		OrgID:         resolveOrgID(r),
		OwnerSystem:   body.OwnerSystem,
		SourceSystem:  body.SourceSystem,
		FactType:      body.FactType,
		SourceEventID: body.SourceEventID,
		SubjectType:   body.SubjectType,
		SubjectID:     body.SubjectID,
		Facts:         body.Facts,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	resp := findingdto.SubmitFactsResponse{
		Evaluation: evaluationResponse(out.Evaluation),
		Findings:   findingsResponse(out.Findings),
	}
	httpjson.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) getEvaluation(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsRead) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	out, err := h.uc.GetEvaluation(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, out.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "evaluation org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, evaluationResponse(out))
}

func (h *Handler) listFindings(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsRead) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.uc.ListFindings(r.Context(), ListFindingsInput{
		OrgID:         resolveOrgID(r),
		OwnerSystem:   r.URL.Query().Get("owner_system"),
		SourceSystem:  r.URL.Query().Get("source_system"),
		FactType:      r.URL.Query().Get("fact_type"),
		SubjectID:     r.URL.Query().Get("subject_id"),
		SourceEventID: r.URL.Query().Get("source_event_id"),
		Status:        r.URL.Query().Get("status"),
		Limit:         limit,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, findingsResponse(out))
}

func (h *Handler) getFinding(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsRead) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	out, err := h.uc.GetFinding(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, out.OrgID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "finding org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, findingResponse(out))
}

func (h *Handler) updateFinding(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusFindingsWrite) {
		return
	}
	id, ok := parsePathUUID(w, r)
	if !ok {
		return
	}
	current, err := h.uc.GetFinding(r.Context(), id)
	if err != nil {
		writeUsecaseError(w, err)
		return
	}
	if !canAccessOrg(r, current.OrgID) || !canWriteOwner(r, current.OwnerSystem) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "finding is not allowed for this principal")
		return
	}
	var body findingdto.UpdateFindingRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	out, err := h.uc.UpdateFindingStatus(r.Context(), id, domain.FindingStatus(body.Status), body.ResolutionNote)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, findingResponse(out))
}

func resolveOrgID(r *http.Request) string {
	orgID, _ := identityhttp.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeNexusCrossOrg)
	return orgID
}

func canAccessOrg(r *http.Request, orgID string) bool {
	return identityhttp.CanAccessOrg(r, orgID, scopeNexusCrossOrg)
}

func parsePathUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func writeUsecaseError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	httpjson.WriteFlatInternalError(w, err, "findings request failed")
}

func ruleResponse(rule domain.FindingRule) findingdto.FindingRuleResponse {
	resp := findingdto.FindingRuleResponse{
		ID:             rule.ID.String(),
		OrgID:          rule.OrgID,
		OwnerSystem:    rule.OwnerSystem,
		SourceSystem:   rule.SourceSystem,
		FactType:       rule.FactType,
		Code:           rule.Code,
		Name:           rule.Name,
		Description:    rule.Description,
		Expression:     rule.Expression,
		Severity:       rule.Severity,
		Title:          rule.Title,
		Message:        rule.Message,
		Recommendation: rule.Recommendation,
		Mode:           string(rule.Mode),
		Enabled:        rule.Enabled,
		Priority:       rule.Priority,
		CreatedAt:      rule.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      rule.UpdatedAt.Format(time.RFC3339),
	}
	if rule.ArchivedAt != nil {
		resp.ArchivedAt = rule.ArchivedAt.Format(time.RFC3339)
	}
	return resp
}

func evaluationResponse(item domain.FactEvaluation) findingdto.FactEvaluationResponse {
	return findingdto.FactEvaluationResponse{
		ID:            item.ID.String(),
		OrgID:         item.OrgID,
		OwnerSystem:   item.OwnerSystem,
		SourceSystem:  item.SourceSystem,
		FactType:      item.FactType,
		SourceEventID: item.SourceEventID,
		SubjectType:   item.SubjectType,
		SubjectID:     item.SubjectID,
		Facts:         item.Facts,
		CreatedAt:     item.CreatedAt.Format(time.RFC3339),
	}
}

func findingsResponse(items []domain.Finding) []findingdto.FindingResponse {
	out := make([]findingdto.FindingResponse, 0, len(items))
	for _, item := range items {
		out = append(out, findingResponse(item))
	}
	return out
}

func findingResponse(item domain.Finding) findingdto.FindingResponse {
	return findingdto.FindingResponse{
		ID:             item.ID.String(),
		OrgID:          item.OrgID,
		EvaluationID:   item.EvaluationID.String(),
		RuleID:         item.RuleID.String(),
		OwnerSystem:    item.OwnerSystem,
		SourceSystem:   item.SourceSystem,
		FactType:       item.FactType,
		SourceEventID:  item.SourceEventID,
		SubjectType:    item.SubjectType,
		SubjectID:      item.SubjectID,
		Code:           item.Code,
		Severity:       item.Severity,
		Title:          item.Title,
		Message:        item.Message,
		Recommendation: item.Recommendation,
		Evidence:       item.Evidence,
		Status:         string(item.Status),
		ResolutionNote: item.ResolutionNote,
		CreatedAt:      item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      item.UpdatedAt.Format(time.RFC3339),
	}
}
