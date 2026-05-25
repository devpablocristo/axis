package ops

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

type summaryUsecase interface {
	Summary(ctx context.Context) (Summary, error)
	ListCallbackDeliveries(ctx context.Context, status string, limit int, orgID *string, allowAll bool) ([]CallbackDelivery, error)
	RetryCallbackDelivery(ctx context.Context, id uuid.UUID, actorID string, orgID *string, allowAll bool) error
	CreateLegalHold(ctx context.Context, hold LegalHold) (LegalHold, error)
	ListLegalHolds(ctx context.Context, orgID *string, allowAll bool) ([]LegalHold, error)
	CreateExportJob(ctx context.Context, job ExportJob) (ExportJob, error)
	ListExportJobs(ctx context.Context, orgID *string, allowAll bool) ([]ExportJob, error)
	RunReconciliation(ctx context.Context, orgID *string, actorID string) (ReconciliationReport, error)
}

type Handler struct {
	uc summaryUsecase
}

func NewHandler(uc summaryUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ops/governance/summary", h.summary)
	mux.HandleFunc("GET /v1/ops/callback-deliveries", h.listCallbackDeliveries)
	mux.HandleFunc("POST /v1/ops/callback-deliveries/{id}/retry", h.retryCallbackDelivery)
	mux.HandleFunc("GET /v1/ops/legal-holds", h.listLegalHolds)
	mux.HandleFunc("POST /v1/ops/legal-holds", h.createLegalHold)
	mux.HandleFunc("GET /v1/ops/exports", h.listExports)
	mux.HandleFunc("POST /v1/ops/exports", h.createExport)
	mux.HandleFunc("POST /v1/ops/reconciliation/run", h.runReconciliation)
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsRead, scopeNexusPoliciesAdmin) {
		return
	}
	out, err := h.uc.Summary(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "load governance ops summary")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) listCallbackDeliveries(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsRead, scopeNexusPoliciesAdmin) {
		return
	}
	orgID, allowAll := opsOrgScope(r)
	list, err := h.uc.ListCallbackDeliveries(r.Context(), r.URL.Query().Get("status"), queryLimit(r, 100), orgID, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list callback deliveries")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

func (h *Handler) retryCallbackDelivery(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	orgID, allowAll := opsOrgScope(r)
	if err := h.uc.RetryCallbackDelivery(r.Context(), id, opsActorID(r), orgID, allowAll); err != nil {
		httpjson.WriteFlatInternalError(w, err, "retry callback delivery")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

type legalHoldRequest struct {
	OrgID       string `json:"org_id,omitempty"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id,omitempty"`
	Reason      string `json:"reason"`
}

func (h *Handler) createLegalHold(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	var body legalHoldRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if strings.TrimSpace(body.SubjectType) == "" || strings.TrimSpace(body.Reason) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "subject_type and reason are required")
		return
	}
	orgID, ok := writableOrg(r, body.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org is not allowed for this principal")
		return
	}
	hold, err := h.uc.CreateLegalHold(r.Context(), LegalHold{
		OrgID: orgID, SubjectType: body.SubjectType, SubjectID: body.SubjectID,
		Reason: body.Reason, CreatedBy: opsActorID(r),
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create legal hold")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, hold)
}

func (h *Handler) listLegalHolds(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsRead, scopeNexusPoliciesAdmin) {
		return
	}
	orgID, allowAll := opsOrgScope(r)
	list, err := h.uc.ListLegalHolds(r.Context(), orgID, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list legal holds")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

type exportRequest struct {
	OrgID       string         `json:"org_id,omitempty"`
	ExportType  string         `json:"export_type"`
	SubjectType string         `json:"subject_type,omitempty"`
	SubjectID   string         `json:"subject_id,omitempty"`
	Manifest    map[string]any `json:"manifest,omitempty"`
}

func (h *Handler) createExport(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	var body exportRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if strings.TrimSpace(body.ExportType) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "export_type is required")
		return
	}
	orgID, ok := writableOrg(r, body.OrgID)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "org is not allowed for this principal")
		return
	}
	job, err := h.uc.CreateExportJob(r.Context(), ExportJob{
		OrgID: orgID, ExportType: body.ExportType, SubjectType: body.SubjectType,
		SubjectID: body.SubjectID, RequestedBy: opsActorID(r), Manifest: body.Manifest,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create governance export")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, job)
}

func (h *Handler) listExports(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsRead, scopeNexusPoliciesAdmin) {
		return
	}
	orgID, allowAll := opsOrgScope(r)
	list, err := h.uc.ListExportJobs(r.Context(), orgID, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list governance exports")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

func (h *Handler) runReconciliation(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusOpsAdmin, scopeNexusPoliciesAdmin) {
		return
	}
	orgID, allowAll := opsOrgScope(r)
	if allowAll {
		orgID = nil
	}
	out, err := h.uc.RunReconciliation(r.Context(), orgID, opsActorID(r))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "run governance reconciliation")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, out)
}

func queryLimit(r *http.Request, fallback int) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func opsOrgScope(r *http.Request) (*string, bool) {
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		if orgID := strings.TrimSpace(identityhttp.PrincipalOrgID(r)); orgID != "" {
			return &orgID, false
		}
		return nil, true
	}
	orgID := strings.TrimSpace(identityhttp.PrincipalOrgID(r))
	if orgID == "" {
		return nil, false
	}
	return &orgID, false
}

func writableOrg(r *http.Request, requested string) (*string, bool) {
	requested = strings.TrimSpace(requested)
	if identityhttp.HasNoAuthContext(r) || identityhttp.HasScope(r, scopeNexusCrossOrg) {
		if requested == "" {
			return nil, true
		}
		return &requested, true
	}
	orgID := strings.TrimSpace(identityhttp.PrincipalOrgID(r))
	if orgID == "" || (requested != "" && requested != orgID) {
		return nil, false
	}
	return &orgID, true
}

func opsActorID(r *http.Request) string {
	if ctx := identityhttp.FromRequest(r); strings.TrimSpace(ctx.Actor) != "" {
		return strings.TrimSpace(ctx.Actor)
	}
	return "system"
}
