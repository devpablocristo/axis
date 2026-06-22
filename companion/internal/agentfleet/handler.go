package agentfleet

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

const (
	scopeAgentRead         = "companion:agents:read"
	scopeAgentAdmin        = "companion:agents:admin"
	scopeAgentRuntimeAdmin = "companion:runtime:admin"
	scopeCrossOrg          = "companion:cross_org"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", h.listAgents)
	mux.HandleFunc("GET /v1/agents/{agent_id}", h.getAgent)
	mux.HandleFunc("PUT /v1/agents/{agent_id}", h.putAgent)
	mux.HandleFunc("POST /v1/agents/{agent_id}/disable", h.disableAgent)
	mux.HandleFunc("POST /v1/agents/{agent_id}/archive", h.archiveAgent)
	mux.HandleFunc("POST /v1/agents/{agent_id}/trash", h.trashAgent)
	mux.HandleFunc("POST /v1/agents/{agent_id}/restore", h.restoreAgent)
	mux.HandleFunc("POST /v1/agents/{agent_id}/approve", h.approveAgent)
	mux.HandleFunc("POST /v1/agents/{agent_id}/ignore", h.ignoreAgent)
	mux.HandleFunc("DELETE /v1/agents/{agent_id}", h.deleteAgent)
	mux.HandleFunc("POST /v1/agents/assignments", h.assignAgent)
	mux.HandleFunc("GET /v1/agents/handoffs", h.listHandoffs)
	mux.HandleFunc("POST /v1/agents/handoffs", h.createHandoff)
	mux.HandleFunc("PATCH /v1/agents/handoffs/{id}", h.updateHandoff)
}

func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	agents, err := h.uc.ListAgents(r.Context(), orgID, surface)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list agents failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": agents})
}

func (h *Handler) getAgent(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	agent, err := h.uc.GetAgent(r.Context(), orgID, surface, strings.TrimSpace(r.PathValue("agent_id")))
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, agent)
}

func (h *Handler) putAgent(w http.ResponseWriter, r *http.Request) {
	orgID, surface, actorID, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	if !agentWriteAllowed(w, r) {
		return
	}
	var agent Agent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	agent.OrgID = orgID
	agent.ProductSurface = surface
	agent.AgentID = strings.TrimSpace(r.PathValue("agent_id"))
	agent.CreatedBy = actorID
	saved, err := h.uc.SaveAgent(r.Context(), agent)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, saved)
}

func (h *Handler) disableAgent(w http.ResponseWriter, r *http.Request) {
	orgID, surface, actorID, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	if !agentWriteAllowed(w, r) {
		return
	}
	agent, err := h.uc.DisableAgent(r.Context(), orgID, surface, strings.TrimSpace(r.PathValue("agent_id")), actorID)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, agent)
}

func (h *Handler) archiveAgent(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(ctx actorContext) (Agent, error) {
		return h.uc.ArchiveAgent(ctx.Request.Context(), ctx.OrgID, ctx.Surface, ctx.AgentID, ctx.ActorID)
	})
}

func (h *Handler) trashAgent(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(ctx actorContext) (Agent, error) {
		return h.uc.TrashAgent(ctx.Request.Context(), ctx.OrgID, ctx.Surface, ctx.AgentID, ctx.ActorID)
	})
}

func (h *Handler) restoreAgent(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(ctx actorContext) (Agent, error) {
		return h.uc.RestoreAgent(ctx.Request.Context(), ctx.OrgID, ctx.Surface, ctx.AgentID, ctx.ActorID)
	})
}

func (h *Handler) approveAgent(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(ctx actorContext) (Agent, error) {
		return h.uc.ApproveAgent(ctx.Request.Context(), ctx.OrgID, ctx.Surface, ctx.AgentID, ctx.ActorID)
	})
}

func (h *Handler) ignoreAgent(w http.ResponseWriter, r *http.Request) {
	h.lifecycleAction(w, r, func(ctx actorContext) (Agent, error) {
		return h.uc.IgnoreAgent(ctx.Request.Context(), ctx.OrgID, ctx.Surface, ctx.AgentID, ctx.ActorID)
	})
}

func (h *Handler) deleteAgent(w http.ResponseWriter, r *http.Request) {
	orgID, surface, actorID, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	if !agentWriteAllowed(w, r) {
		return
	}
	if err := h.uc.DeleteAgent(r.Context(), orgID, surface, strings.TrimSpace(r.PathValue("agent_id")), actorID); err != nil {
		writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type actorContext struct {
	Request *http.Request
	OrgID   string
	Surface string
	AgentID string
	ActorID string
}

func (h *Handler) lifecycleAction(w http.ResponseWriter, r *http.Request, action func(actorContext) (Agent, error)) {
	orgID, surface, actorID, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	if !agentWriteAllowed(w, r) {
		return
	}
	agent, err := action(actorContext{
		Request: r,
		OrgID:   orgID,
		Surface: surface,
		AgentID: strings.TrimSpace(r.PathValue("agent_id")),
		ActorID: actorID,
	})
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, agent)
}

func (h *Handler) assignAgent(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	var body AssignmentInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	body.OrgID = orgID
	body.ProductSurface = surface
	result, err := h.uc.AssignAgent(r.Context(), body)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) listHandoffs(w http.ResponseWriter, r *http.Request) {
	orgID, surface, _, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid limit")
			return
		}
		limit = parsed
	}
	handoffs, err := h.uc.ListHandoffs(r.Context(), orgID, surface, limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list handoffs failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": handoffs})
}

func (h *Handler) createHandoff(w http.ResponseWriter, r *http.Request) {
	orgID, surface, actorID, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	var handoff Handoff
	if err := json.NewDecoder(r.Body).Decode(&handoff); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	handoff.OrgID = orgID
	handoff.ProductSurface = surface
	handoff.CreatedBy = actorID
	saved, err := h.uc.CreateHandoff(r.Context(), handoff)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, saved)
}

func (h *Handler) updateHandoff(w http.ResponseWriter, r *http.Request) {
	orgID, surface, actorID, ok := agentRequestContext(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	handoffID := strings.TrimSpace(r.PathValue("id"))
	if _, err := uuid.Parse(handoffID); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid handoff id")
		return
	}
	handoff, err := h.uc.UpdateHandoffStatus(r.Context(), orgID, surface, handoffID, strings.TrimSpace(body.Status), actorID)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, handoff)
}

func agentRequestContext(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "agent fleet endpoints require authenticated admin context")
		return "", "", "", false
	}
	if !identityctx.HasAnyScope(r, scopeAgentRead, scopeAgentAdmin, scopeAgentRuntimeAdmin, scopeCrossOrg) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing agent fleet admin scope")
		return "", "", "", false
	}
	id := identityctx.FromRequest(r)
	orgID, allowed := identityctx.EffectiveOrgID(r, r.URL.Query().Get("org_id"), scopeCrossOrg)
	if !allowed || strings.TrimSpace(orgID) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "customer org context is required")
		return "", "", "", false
	}
	surface := strings.TrimSpace(r.URL.Query().Get("product_surface"))
	if surface == "" {
		surface = id.ProductSurface
	}
	return orgID, surface, id.EffectiveActorID(), true
}

func agentWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasAnyScope(r, scopeAgentAdmin, scopeAgentRuntimeAdmin) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing agent fleet write scope")
	return false
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "agent not found")
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "agent fleet operation failed")
	}
}
