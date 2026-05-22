package requests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/platform/http/go/httpjson"

	requestdto "github.com/devpablocristo/nexus/internal/requests/handler/dto"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// Port mínimo: solo lo que el handler necesita
const (
	defaultListLimit     = 50
	maxListLimit         = 1000
	maxIdempotencyKeyLen = 256
)

type requestUsecase interface {
	Submit(ctx context.Context, in SubmitInput) (SubmitOutput, error)
	Simulate(ctx context.Context, in SubmitInput) (SimulateOutput, error)
	ReplaySimulate(ctx context.Context, in ReplaySimulateInput) (ReplaySimulateOutput, error)
	GetByID(ctx context.Context, id uuid.UUID) (requestdomain.Request, error)
	List(ctx context.Context, status, actionType string, limit int, orgID *string, allowAll bool) ([]requestdomain.Request, error)
	ReportResult(ctx context.Context, requestID uuid.UUID, in ReportResultInput) error
	Attest(ctx context.Context, requestID uuid.UUID, in AttestInput) (requestdomain.Attestation, error)
	GetAttestation(ctx context.Context, requestID uuid.UUID) (requestdomain.Attestation, error)
	BatchSimulate(ctx context.Context, in BatchSimulateInput) (BatchSimulateOutput, error)
	SimulateApproval(ctx context.Context, in ApprovalSimulateInput) (ApprovalSimulateOutput, error)
}

type Handler struct {
	uc requestUsecase
}

func NewHandler(uc requestUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/requests", h.submit)
	mux.HandleFunc("POST /v1/requests/simulate", h.simulate)
	mux.HandleFunc("POST /v1/requests/simulate/replay", h.replaySimulate)
	mux.HandleFunc("GET /v1/requests", h.list)
	mux.HandleFunc("GET /v1/requests/{id}", h.getByID)
	mux.HandleFunc("POST /v1/requests/{id}/result", h.reportResult)
	mux.HandleFunc("POST /v1/requests/{id}/attest", h.attest)
	mux.HandleFunc("GET /v1/requests/{id}/attestation", h.getAttestation)
	mux.HandleFunc("POST /v1/requests/simulate/batch", h.batchSimulate)
	mux.HandleFunc("POST /v1/requests/simulate/approval", h.simulateApproval)
}

func (h *Handler) simulate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead, scopeNexusRequestsWrite) {
		return
	}
	var body requestdto.SimulateRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.ActionType == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "action_type is required")
		return
	}
	params, ok := bindParamsToPrincipalOrg(r, body.Params)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}

	out, err := h.uc.Simulate(r.Context(), SubmitInput{
		RequesterType:  body.RequesterType,
		RequesterID:    body.RequesterID,
		RequesterName:  body.RequesterName,
		ActionType:     body.ActionType,
		TargetSystem:   body.TargetSystem,
		TargetResource: body.TargetResource,
		ActionBinding:  body.ActionBinding,
		Params:         params,
		Reason:         body.Reason,
		Context:        body.Context,
	})
	if err != nil {
		slog.Error("simulate failed", "error", err)
		httpjson.WriteFlatInternalError(w, err, "simulate request")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, requestdto.SimulateResponse{
		Decision:             out.Decision,
		RiskLevel:            out.RiskLevel,
		DecisionReason:       out.DecisionReason,
		Status:               out.Status,
		PolicyMatched:        out.PolicyMatched,
		RiskAssessment:       out.RiskAssessment,
		WouldRequireApproval: out.WouldRequireApproval,
		AISummary:            out.AISummary,
	})
}

func (h *Handler) replaySimulate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) || !requireScope(w, r, scopeNexusCrossOrg) {
		return
	}
	var body requestdto.ReplaySimulateRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Expression == "" || body.Effect == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "expression and effect are required")
		return
	}

	out, err := h.uc.ReplaySimulate(r.Context(), ReplaySimulateInput{
		Expression: body.Expression,
		Effect:     body.Effect,
		Limit:      body.Limit,
	})
	if err != nil {
		slog.Error("replay simulate failed", "error", err)
		httpjson.WriteFlatInternalError(w, err, "replay simulate")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsWrite) {
		return
	}
	var body requestdto.SubmitRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.RequesterType == "" || body.RequesterID == "" || body.ActionType == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "requester_type, requester_id and action_type are required")
		return
	}
	var idemKey *string
	if k := strings.TrimSpace(r.Header.Get("Idempotency-Key")); k != "" {
		if len(k) > maxIdempotencyKeyLen {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "idempotency key too long")
			return
		}
		idemKey = &k
	} else if k := strings.TrimSpace(body.IdempotencyKey); k != "" {
		if len(k) > maxIdempotencyKeyLen {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "idempotency key too long")
			return
		}
		idemKey = &k
	}
	params, ok := bindParamsToPrincipalOrg(r, body.Params)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}
	out, err := h.uc.Submit(r.Context(), SubmitInput{
		IdempotencyKey: idemKey,
		RequesterType:  body.RequesterType,
		RequesterID:    body.RequesterID,
		RequesterName:  body.RequesterName,
		ActionType:     body.ActionType,
		TargetSystem:   body.TargetSystem,
		TargetResource: body.TargetResource,
		ActionBinding:  body.ActionBinding,
		Params:         params,
		Reason:         body.Reason,
		Context:        body.Context,
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "unknown action_type") ||
			strings.Contains(errMsg, "is disabled") ||
			strings.Contains(errMsg, "not delegated") {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", errMsg)
			return
		}
		if strings.Contains(errMsg, "missing required param") ||
			strings.Contains(errMsg, "action_type schema") ||
			strings.Contains(errMsg, "action_binding") {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", errMsg)
			return
		}
		httpjson.WriteFlatInternalError(w, err, "request submission failed")
		return
	}
	resp := requestdto.SubmitResponse{
		RequestID:      out.RequestID.String(),
		Decision:       out.Decision,
		RiskLevel:      out.RiskLevel,
		DecisionReason: out.DecisionReason,
		Status:         out.Status,
		BindingHash:    out.BindingHash,
		AISummary:      out.AISummary,
		AIDegraded:     out.AIDegraded,
	}
	if out.Approval != nil {
		resp.Approval = &requestdto.ApprovalPayload{
			ID:        out.Approval.ID.String(),
			ExpiresAt: out.Approval.ExpiresAt.Format(time.RFC3339),
		}
	}
	httpjson.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) {
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	actionType := q.Get("action_type")
	limit := defaultListLimit
	if l := q.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= maxListLimit {
			limit = parsed
		}
	}
	// Tenancy filter aplicado en SQL (no post-filter): así el LIMIT no se
	// llena con rows de otros orgs antes del filtro y el caller ve el conteo
	// real para su tenant.
	orgID, allowAll, ok := requestOrgScope(r)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	list, err := h.uc.List(r.Context(), status, actionType, limit, orgID, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list requests failed")
		return
	}
	out := make([]requestdto.RequestResponse, 0, len(list))
	for _, req := range list {
		out = append(out, toRequestResponse(req))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

// requestOrgScope extrae la regla de tenancy del request HTTP. Espeja la
// semántica de canAccessRequestOrg pero la traduce a parámetros que el repo
// pueda aplicar en SQL.
//   - cross-org admin scope sin X-Org-ID → allowAll=true.
//   - cross-org admin scope con X-Org-ID → orgID=&value, allowAll=false.
//   - X-Org-ID presente → orgID=&value, allowAll=false.
//   - sin auth context (dev/local) → allowAll=true para no romper smoke.
//   - sino → fail closed.
func requestOrgScope(r *http.Request) (*string, bool, bool) {
	if requestHasScope(r, scopeNexusCrossOrg) {
		if orgID := strings.TrimSpace(r.Header.Get("X-Org-ID")); orgID != "" {
			return &orgID, false, true
		}
		return nil, true, true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID != "" {
		return &orgID, false, true
	}
	if requestHasNoAuthContext(r) {
		return nil, true, true
	}
	return nil, false, false
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	req, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get request failed")
		return
	}
	if !canAccessRequestOrg(r, req) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toRequestResponse(req))
}

func (h *Handler) reportResult(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsResult) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	var body requestdto.ReportResultRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	resultKey := resultKeyFromRequest(r, body.ResultID)
	if len(resultKey) > maxIdempotencyKeyLen {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "result idempotency key too long")
		return
	}
	req, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get request failed")
		return
	}
	if !canAccessRequestOrg(r, req) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}
	err = h.uc.ReportResult(r.Context(), id, ReportResultInput{
		ResultKey:    resultKey,
		ActorID:      strings.TrimSpace(r.Header.Get("X-User-ID")),
		OrgID:        req.OrgID,
		Success:      body.Success,
		Result:       body.Result,
		DurationMs:   body.DurationMs,
		ErrorMessage: body.ErrorMessage,
	})
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		if domainerr.IsConflict(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "request is not executable")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "report result failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) attest(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusEvidenceWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	var body requestdto.AttestRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Status == "" || body.Attester == "" || body.Signature == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "status, attester and signature are required")
		return
	}
	req, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get request failed")
		return
	}
	if !canAccessRequestOrg(r, req) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}

	attestation, err := h.uc.Attest(r.Context(), id, AttestInput{
		Status:       body.Status,
		ProviderRefs: body.ProviderRefs,
		Signature:    body.Signature,
		Attester:     body.Attester,
		Metadata:     body.Metadata,
	})
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		if strings.Contains(err.Error(), "status must be") {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "request must be executed or failed to attest")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "attest failed")
		return
	}

	httpjson.WriteJSON(w, http.StatusCreated, toAttestResponse(attestation))
}

func (h *Handler) getAttestation(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	req, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get request failed")
		return
	}
	if !canAccessRequestOrg(r, req) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}
	attestation, err := h.uc.GetAttestation(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrAttestationNotFound) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "attestation not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get attestation failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toAttestResponse(attestation))
}

func (h *Handler) batchSimulate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead, scopeNexusRequestsWrite) {
		return
	}
	var body requestdto.BatchSimulateRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if len(body.Requests) == 0 {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "requests array is required")
		return
	}
	if len(body.Requests) > 100 {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "max 100 requests per batch")
		return
	}

	inputs := make([]SubmitInput, 0, len(body.Requests))
	for _, req := range body.Requests {
		params, ok := bindParamsToPrincipalOrg(r, req.Params)
		if !ok {
			httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
			return
		}
		inputs = append(inputs, SubmitInput{
			RequesterType:  req.RequesterType,
			RequesterID:    req.RequesterID,
			RequesterName:  req.RequesterName,
			ActionType:     req.ActionType,
			TargetSystem:   req.TargetSystem,
			TargetResource: req.TargetResource,
			ActionBinding:  req.ActionBinding,
			Params:         params,
			Reason:         req.Reason,
			Context:        req.Context,
		})
	}

	out, err := h.uc.BatchSimulate(r.Context(), BatchSimulateInput{Requests: inputs})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "batch simulate failed")
		return
	}

	resp := requestdto.BatchSimulateResponse{
		Total:           out.Total,
		Allowed:         out.Allowed,
		Denied:          out.Denied,
		RequireApproval: out.RequireApproval,
		ByRisk:          out.ByRisk,
		Results:         make([]requestdto.BatchSimulateItem, 0, len(out.Results)),
	}
	for _, item := range out.Results {
		resp.Results = append(resp.Results, requestdto.BatchSimulateItem{
			ActionType:     item.ActionType,
			RequesterID:    item.RequesterID,
			TargetSystem:   item.TargetSystem,
			Decision:       item.Decision,
			RiskLevel:      item.RiskLevel,
			DecisionReason: item.DecisionReason,
			PolicyMatched:  item.PolicyMatched,
		})
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) simulateApproval(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) {
		return
	}
	var body requestdto.ApprovalSimulateRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.RequestID == "" || body.Action == "" || body.ApproverID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "request_id, action and approver_id are required")
		return
	}
	if body.Action != "approve" && body.Action != "reject" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "action must be approve or reject")
		return
	}

	reqID, err := uuid.Parse(body.RequestID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid request_id")
		return
	}
	req, err := h.uc.GetByID(r.Context(), reqID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get request failed")
		return
	}
	if !canAccessRequestOrg(r, req) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "request org is not allowed for this principal")
		return
	}

	out, err := h.uc.SimulateApproval(r.Context(), ApprovalSimulateInput{
		RequestID:  reqID,
		Action:     body.Action,
		ApproverID: body.ApproverID,
	})
	if err != nil {
		if domainerr.IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "request not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "simulate approval failed")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, requestdto.ApprovalSimulateResponse{
		CurrentStatus:     out.CurrentStatus,
		SimulatedStatus:   out.SimulatedStatus,
		BreakGlass:        out.BreakGlass,
		RequiredApprovals: out.RequiredApprovals,
		CurrentApprovals:  out.CurrentApprovals,
		AfterApprovals:    out.AfterApprovals,
		WouldFinalize:     out.WouldFinalize,
		AlreadyDecided:    out.AlreadyDecided,
		Reason:            out.Reason,
	})
}

func toAttestResponse(a requestdomain.Attestation) requestdto.AttestResponse {
	return requestdto.AttestResponse{
		ID:                a.ID.String(),
		RequestID:         a.RequestID.String(),
		Status:            a.Status,
		ProviderRefs:      a.ProviderRefs,
		Signature:         a.Signature,
		Attester:          a.Attester,
		Metadata:          a.Metadata,
		CreatedAt:         a.CreatedAt.Format(time.RFC3339),
		Verified:          a.Verified,
		VerificationError: a.VerificationError,
	}
}

// --- Helpers ---

func toRequestResponse(req requestdomain.Request) requestdto.RequestResponse {
	resp := requestdto.RequestResponse{
		ID:             req.ID.String(),
		RequesterType:  string(req.RequesterType),
		RequesterID:    req.RequesterID,
		RequesterName:  req.RequesterName,
		ActionType:     req.ActionType,
		TargetSystem:   req.TargetSystem,
		TargetResource: req.TargetResource,
		ActionBinding:  req.ActionBinding,
		BindingHash:    req.BindingHash,
		Params:         req.Params,
		Reason:         req.Reason,
		RiskLevel:      string(req.RiskLevel),
		Decision:       string(req.Decision),
		DecisionReason: req.DecisionReason,
		Status:         string(req.Status),
		AISummary:      req.AISummary,
		AIDegraded:     req.AIDegraded,
		CreatedAt:      req.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      req.UpdatedAt.Format(time.RFC3339),
	}
	if req.OrgID != nil {
		resp.OrgID = strings.TrimSpace(*req.OrgID)
	}
	return resp
}

func bindParamsToPrincipalOrg(r *http.Request, params map[string]any) (map[string]any, bool) {
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		if requestHasNoAuthContext(r) {
			return params, true
		}
		if requestHasScope(r, scopeNexusCrossOrg) {
			if raw, exists := params["org_id"]; exists && strings.TrimSpace(rawToString(raw)) != "" {
				return params, true
			}
		}
		return nil, false
	}
	out := make(map[string]any, len(params)+1)
	for key, value := range params {
		out[key] = value
	}
	if raw, exists := out["org_id"]; exists {
		requested := strings.TrimSpace(rawToString(raw))
		if requested != "" && requested != orgID {
			return nil, false
		}
	}
	out["org_id"] = orgID
	return out, true
}

func canAccessRequestOrg(r *http.Request, req requestdomain.Request) bool {
	if requestHasScope(r, scopeNexusCrossOrg) {
		return true
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID != "" {
		if req.OrgID == nil {
			return false
		}
		return strings.TrimSpace(*req.OrgID) == orgID
	}
	if requestHasNoAuthContext(r) {
		return true
	}
	return false
}

func rawToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func resultKeyFromRequest(r *http.Request, bodyResultID string) string {
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		return key
	}
	return strings.TrimSpace(bodyResultID)
}

// logAuditError loguea errores de audit sin fallar la request (best-effort).
func logAuditError(err error, requestID uuid.UUID, event string) {
	if err != nil {
		slog.Error("audit event failed", "error", err, "request_id", requestID, "event", event)
	}
}
