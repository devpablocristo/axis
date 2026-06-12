package audit

import (
	"context"
	"net/http"

	auditdto "github.com/devpablocristo/nexus/internal/audit/handler/dto"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

type replayUsecase interface {
	Replay(ctx context.Context, requestID uuid.UUID) (ReplayOutput, error)
	Verify(ctx context.Context, requestID uuid.UUID) (IntegrityOutput, error)
}

type Handler struct {
	uc replayUsecase
}

func NewHandler(uc replayUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/requests/{id}/replay", h.replay)
	mux.HandleFunc("GET /v1/requests/{id}/replay/verify", h.verify)
}

func (h *Handler) replay(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	out, err := h.uc.Replay(r.Context(), id)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "replay failed")
		return
	}
	if !canAccessReplayOrg(r, out) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "replay org is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toReplayResponse(out))
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeNexusRequestsRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	replay, err := h.uc.Replay(r.Context(), id)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "replay failed")
		return
	}
	if !canAccessReplayOrg(r, replay) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "replay org is not allowed for this principal")
		return
	}
	out, err := h.uc.Verify(r.Context(), id)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "verify replay failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, toIntegrityInfo(out))
}

// toReplayResponse convierte el output de dominio a DTO HTTP.
func toReplayResponse(out ReplayOutput) auditdto.ReplayResponse {
	timeline := make([]auditdto.TimelineEntry, 0, len(out.Timeline))
	for _, e := range out.Timeline {
		timeline = append(timeline, auditdto.TimelineEntry{
			Event:   e.Event,
			Actor:   e.Actor,
			At:      e.At,
			Summary: e.Summary,
		})
	}
	return auditdto.ReplayResponse{
		RequestID:     out.RequestID,
		OrgID:         out.OrgID,
		Requester:     auditdto.RequesterInfo{Type: out.Requester.Type, ID: out.Requester.ID},
		ActionType:    out.ActionType,
		Target:        out.Target,
		FinalStatus:   out.FinalStatus,
		DurationTotal: out.DurationTotal,
		Timeline:      timeline,
		Integrity:     toIntegrityInfoPtr(out.Integrity),
	}
}

func toIntegrityInfoPtr(out *IntegrityOutput) *auditdto.IntegrityInfo {
	if out == nil {
		return nil
	}
	info := toIntegrityInfo(*out)
	return &info
}

func toIntegrityInfo(out IntegrityOutput) auditdto.IntegrityInfo {
	return auditdto.IntegrityInfo{
		Status:        out.Status,
		CheckedEvents: out.CheckedEvents,
		FirstHash:     out.FirstHash,
		LastHash:      out.LastHash,
		Error:         out.Error,
	}
}
