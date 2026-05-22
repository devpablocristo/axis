package approvals

import (
	"context"
	"errors"
	"net/http"
	"strings"

	approvaldto "github.com/devpablocristo/nexus/internal/approvals/handler/dto"
	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

const defaultListLimit = 50

type approvalUsecase interface {
	ListPending(ctx context.Context, limit int, orgID *string, allowAll bool) ([]approvaldomain.Approval, error)
	GetByID(ctx context.Context, approvalID uuid.UUID) (approvaldomain.Approval, error)
	Approve(ctx context.Context, approvalID uuid.UUID, decidedBy, note string) error
	Reject(ctx context.Context, approvalID uuid.UUID, decidedBy, note string) error
}

type Handler struct {
	uc approvalUsecase
}

func NewHandler(uc approvalUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/approvals/pending", h.listPending)
	mux.HandleFunc("POST /v1/approvals/{id}/approve", h.approve)
	mux.HandleFunc("POST /v1/approvals/{id}/reject", h.reject)
}

func (h *Handler) listPending(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusApprovalsDecide) {
		return
	}
	// Tenancy filter en SQL para no truncar el LIMIT con rows de otros orgs.
	orgID, allowAll, ok := requestOrgScope(r)
	if !ok {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	list, err := h.uc.ListPending(r.Context(), defaultListLimit, orgID, allowAll)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list pending approvals failed")
		return
	}
	out := make([]approvaldto.ApprovalResponse, 0, len(list))
	for _, a := range list {
		out = append(out, toApprovalResponse(a))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusApprovalsDecide) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	var body approvaldto.ApprovalDecisionRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	actorID := decisionActorID(r, body.DecidedBy)
	if actorID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "decided_by or authenticated user is required")
		return
	}
	approval, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeApprovalUsecaseError(w, err)
		return
	}
	if !canAccessApprovalOrg(r, approval) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "approval org is not allowed for this principal")
		return
	}
	if err := h.uc.Approve(r.Context(), id, actorID, body.Note); err != nil {
		writeApprovalUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusApprovalsDecide) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	var body approvaldto.ApprovalDecisionRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	actorID := decisionActorID(r, body.DecidedBy)
	if actorID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "decided_by or authenticated user is required")
		return
	}
	approval, err := h.uc.GetByID(r.Context(), id)
	if err != nil {
		writeApprovalUsecaseError(w, err)
		return
	}
	if !canAccessApprovalOrg(r, approval) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "approval org is not allowed for this principal")
		return
	}
	if err := h.uc.Reject(r.Context(), id, actorID, body.Note); err != nil {
		writeApprovalUsecaseError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// toApprovalResponse convierte entidad de dominio a DTO HTTP.
func toApprovalResponse(a approvaldomain.Approval) approvaldto.ApprovalResponse {
	approveCount := 0
	for _, d := range a.Decisions {
		if d.Action == "approve" {
			approveCount++
		}
	}

	resp := approvaldto.ApprovalResponse{
		ID:                a.ID.String(),
		RequestID:         a.RequestID.String(),
		Status:            string(a.Status),
		DecidedBy:         a.DecidedBy,
		DecisionNote:      a.DecisionNote,
		ExpiresAt:         a.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		CreatedAt:         a.CreatedAt.Format("2006-01-02T15:04:05Z"),
		BreakGlass:        a.BreakGlass,
		RequiredApprovals: a.RequiredApprovals,
		CurrentApprovals:  approveCount,
	}
	if a.OrgID != nil {
		resp.OrgID = strings.TrimSpace(*a.OrgID)
	}
	if a.DecidedAt != nil {
		s := a.DecidedAt.Format("2006-01-02T15:04:05Z")
		resp.DecidedAt = &s
	}
	for _, d := range a.Decisions {
		resp.Decisions = append(resp.Decisions, approvaldto.ApprovalDecisionDTO{
			ApproverID: d.ApproverID,
			Action:     d.Action,
			Note:       d.Note,
			DecidedAt:  d.DecidedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return resp
}

func writeApprovalUsecaseError(w http.ResponseWriter, err error) {
	if domainerr.IsConflict(err) {
		// Usar el mensaje del error de dominio (distingue "not pending" vs "already decided")
		var de domainerr.Error
		msg := "conflict"
		if errors.As(err, &de) {
			msg = de.Message()
		}
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", msg)
		return
	}
	if domainerr.IsNotFound(err) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "approval not found")
		return
	}
	httpjson.WriteFlatInternalError(w, err, "approval operation failed")
}

func decisionActorID(r *http.Request, explicit string) string {
	if actor := strings.TrimSpace(r.Header.Get("X-User-ID")); actor != "" {
		return actor
	}
	return strings.TrimSpace(explicit)
}
