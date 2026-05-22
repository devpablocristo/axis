package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"
	tasksdto "github.com/devpablocristo/companion/internal/tasks/handler/dto"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type taskUsecase interface {
	Create(ctx context.Context, in CreateTaskInput) (domain.Task, error)
	List(ctx context.Context, orgID string, limit int) ([]domain.Task, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Task, error)
	GetDetail(ctx context.Context, id uuid.UUID) (TaskDetail, error)
	AddMessage(ctx context.Context, taskID uuid.UUID, in AddMessageInput) (domain.TaskMessage, error)
	Investigate(ctx context.Context, taskID uuid.UUID, in InvestigateInput) (domain.Task, error)
	Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, nexusclient.SubmitResponse, error)
	SetExecutionPlan(ctx context.Context, taskID uuid.UUID, in SetExecutionPlanInput) (domain.TaskExecutionPlan, error)
	ExecuteTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error)
	RetryTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error)
	SyncTaskNexus(ctx context.Context, taskID uuid.UUID) (domain.Task, error)
	Chat(ctx context.Context, in ChatInput) (ChatResult, error)
}

type Handler struct {
	uc taskUsecase
}

func NewHandler(uc taskUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/tasks", h.create)
	mux.HandleFunc("GET /v1/tasks", h.list)
	mux.HandleFunc("GET /v1/tasks/{id}", h.getByID)
	mux.HandleFunc("POST /v1/tasks/{id}/message", h.addMessage)
	mux.HandleFunc("POST /v1/tasks/{id}/investigate", h.investigate)
	mux.HandleFunc("POST /v1/tasks/{id}/propose", h.propose)
	mux.HandleFunc("PUT /v1/tasks/{id}/execution-plan", h.setExecutionPlan)
	mux.HandleFunc("POST /v1/tasks/{id}/execute", h.execute)
	mux.HandleFunc("POST /v1/tasks/{id}/retry", h.retry)
	mux.HandleFunc("POST /v1/tasks/{id}/sync", h.syncNexus)
	mux.HandleFunc("POST /v1/chat", h.chat)
	mux.HandleFunc("POST /v1/internal/customer-messaging/inbound", h.customerMessagingInbound)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body tasksdto.CreateTaskRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Title == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "title is required")
		return
	}
	t, err := h.uc.Create(r.Context(), CreateTaskInput{
		OrgID:       principalOrgID(r),
		Title:       body.Title,
		Goal:        body.Goal,
		Priority:    body.Priority,
		CreatedBy:   body.CreatedBy,
		AssignedTo:  body.AssignedTo,
		Channel:     body.Channel,
		Summary:     body.Summary,
		ContextJSON: body.ContextJSON,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "create task failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, tasksdto.TaskToResponse(t))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	limit := defaultListLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= maxListLimit {
			limit = n
		}
	}
	list, err := h.uc.List(r.Context(), principalOrgID(r), limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list tasks failed")
		return
	}
	out := make([]tasksdto.TaskResponse, 0, len(list))
	for _, t := range list {
		// canAccessTaskOrg queda como defense-in-depth: el SQL ya filtra,
		// pero si el repo se cambia o un fake olvida el filtro, esto evita
		// fugas cross-org.
		if !canAccessTaskOrg(r, t) {
			continue
		}
		out = append(out, tasksdto.TaskToResponse(t))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	detail, err := h.uc.GetDetail(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return
	}
	if !canAccessTaskOrg(r, detail.Task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return
	}
	resp := tasksdto.TaskDetailResponse{
		Task:                tasksdto.TaskToResponse(detail.Task),
		Messages:            make([]tasksdto.MessageResponse, 0, len(detail.Messages)),
		Actions:             make([]tasksdto.ActionResponse, 0, len(detail.Actions)),
		Artifacts:           make([]tasksdto.ArtifactResponse, 0, len(detail.Artifacts)),
		LinkedNexusRequests: make([]tasksdto.LinkedNexusRequestResponse, 0, len(detail.LinkedNexusRequests)),
	}
	for _, m := range detail.Messages {
		resp.Messages = append(resp.Messages, tasksdto.MessageToResponse(m))
	}
	for _, a := range detail.Actions {
		resp.Actions = append(resp.Actions, tasksdto.ActionToResponse(a))
	}
	for _, ar := range detail.Artifacts {
		resp.Artifacts = append(resp.Artifacts, tasksdto.ArtifactToResponse(ar))
	}
	for _, lr := range detail.LinkedNexusRequests {
		resp.LinkedNexusRequests = append(resp.LinkedNexusRequests, tasksdto.LinkedNexusRequestResponse{
			ActionID: lr.ActionID.String(),
			Request:  lr.Request,
		})
	}
	if detail.NexusSync != nil {
		resp.NexusSync = tasksdto.NexusSyncToResponse(*detail.NexusSync)
	}
	if detail.ExecutionPlan != nil {
		resp.ExecutionPlan = tasksdto.ExecutionPlanToResponse(*detail.ExecutionPlan)
	}
	if detail.ExecutionState != nil {
		resp.ExecutionState = tasksdto.ExecutionStateToResponse(*detail.ExecutionState)
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) addMessage(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.AddMessageRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	m, err := h.uc.AddMessage(r.Context(), id, AddMessageInput{
		AuthorType: body.AuthorType,
		AuthorID:   body.AuthorID,
		Body:       body.Body,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "add message failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, tasksdto.MessageToResponse(m))
}

func (h *Handler) investigate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body tasksdto.InvestigateRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
			return
		}
	}
	t, err := h.uc.Investigate(r.Context(), id, InvestigateInput{Note: body.Note})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "investigate failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskToResponse(t))
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.ProposeRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	t, action, sub, err := h.uc.Propose(r.Context(), id, ProposeInput{
		Note:           body.Note,
		TargetSystem:   body.TargetSystem,
		TargetResource: body.TargetResource,
		SessionID:      body.SessionID,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		if errors.Is(err, ErrNexusSubmit) && t.ID != uuid.Nil {
			httpjson.WriteJSON(w, http.StatusBadGateway, map[string]any{
				"code":    "NEXUS_SUBMIT_FAILED",
				"message": "nexus request failed",
				"task":    tasksdto.TaskToResponse(t),
				"action":  tasksdto.ActionToResponse(action),
			})
			return
		}
		httpjson.WriteFlatInternalError(w, err, "propose failed")
		return
	}
	var pr tasksdto.ProposeResponse
	pr.Task = tasksdto.TaskToResponse(t)
	pr.Action = tasksdto.ActionToResponse(action)
	pr.NexusSubmit.RequestID = sub.RequestID
	pr.NexusSubmit.Decision = sub.Decision
	pr.NexusSubmit.Status = sub.Status
	pr.NexusSubmit.RiskLevel = sub.RiskLevel
	pr.NexusSubmit.DecisionReason = sub.DecisionReason
	httpjson.WriteJSON(w, http.StatusOK, pr)
}

func (h *Handler) syncNexus(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	t, err := h.uc.SyncTaskNexus(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "sync failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskToResponse(t))
}

func (h *Handler) setExecutionPlan(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.SetExecutionPlanRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	connectorID, err := uuid.Parse(body.ConnectorID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid connector_id")
		return
	}
	plan, err := h.uc.SetExecutionPlan(r.Context(), id, SetExecutionPlanInput{
		ConnectorID:    connectorID,
		Operation:      body.Operation,
		Payload:        body.Payload,
		IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "set execution plan failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecutionPlanToResponse(plan))
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	out, err := h.uc.ExecuteTask(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if writeNexusBlocked(w, err) {
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "execute task failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecuteTaskResponse{
		Task:           tasksdto.TaskToResponse(out.Task),
		Plan:           *tasksdto.ExecutionPlanToResponse(out.Plan),
		Execution:      tasksdto.ExecutionResultToResponse(out.Execution),
		ExecutionState: tasksdto.ExecutionStateToResponse(out.ExecutionState),
	})
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	out, err := h.uc.RetryTask(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if writeNexusBlocked(w, err) {
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "retry task failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecuteTaskResponse{
		Task:           tasksdto.TaskToResponse(out.Task),
		Plan:           *tasksdto.ExecutionPlanToResponse(out.Plan),
		Execution:      tasksdto.ExecutionResultToResponse(out.Execution),
		ExecutionState: tasksdto.ExecutionStateToResponse(out.ExecutionState),
	})
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body tasksdto.ChatRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Message == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "message is required")
		return
	}

	var taskID *uuid.UUID
	if body.TaskID != "" {
		parsed, err := uuid.Parse(body.TaskID)
		if err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid task_id")
			return
		}
		if !h.authorizeTaskOrg(w, r, parsed) {
			return
		}
		taskID = &parsed
	}

	// Extraer identidad: header X-User-ID / X-Org-ID (inyectados por el frontend o gateway)
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		userID = "subscriber"
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))

	result, err := h.uc.Chat(r.Context(), ChatInput{
		TaskID:         taskID,
		UserID:         userID,
		OrgID:          orgID,
		AuthScopes:     principalScopes(r),
		Message:        body.Message,
		Channel:        body.Channel,
		ProductSurface: body.ProductSurface,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "chat failed")
		return
	}

	msgs := make([]tasksdto.MessageResponse, 0, len(result.Messages))
	for _, m := range result.Messages {
		msgs = append(msgs, tasksdto.MessageToResponse(m))
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ChatResponseFromResult(result.Task, result.Messages))
}

type customerMessagingInboundRequest struct {
	OrgID         string `json:"org_id"`
	PhoneNumberID string `json:"phone_number_id"`
	FromPhone     string `json:"from_phone"`
	Message       string `json:"message"`
	MessageID     string `json:"message_id,omitempty"`
	ProfileName   string `json:"profile_name,omitempty"`
}

type customerMessagingInboundResponse struct {
	ConversationID string   `json:"conversation_id"`
	Reply          string   `json:"reply"`
	TokensUsed     int      `json:"tokens_used"`
	ToolCalls      []string `json:"tool_calls"`
}

func (h *Handler) customerMessagingInbound(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body customerMessagingInboundRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if strings.TrimSpace(body.OrgID) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "org_id is required")
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "message is required")
		return
	}

	userID := strings.TrimSpace(body.FromPhone)
	if userID == "" {
		userID = "whatsapp"
	} else {
		userID = "whatsapp:" + userID
	}
	if profile := strings.TrimSpace(body.ProfileName); profile != "" {
		userID = userID + ":" + profile
	}

	result, err := h.uc.Chat(r.Context(), ChatInput{
		UserID:         userID,
		OrgID:          strings.TrimSpace(body.OrgID),
		AuthScopes:     principalScopes(r),
		Message:        body.Message,
		Channel:        "whatsapp",
		ProductSurface: "pymes",
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "customer messaging inbound failed")
		return
	}

	httpjson.WriteJSON(w, http.StatusOK, customerMessagingInboundResponse{
		ConversationID: chatConversationID(result.Task.ContextJSON),
		Reply:          lastAssistantReply(result.Messages),
		ToolCalls:      []string{},
	})
}

func chatConversationID(raw json.RawMessage) string {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if id, ok := payload[agentConversationContextKey].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func lastAssistantReply(messages []domain.TaskMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		role := strings.TrimSpace(strings.ToLower(messages[i].AuthorType))
		if role == "assistant" || role == "system" {
			return messages[i].Body
		}
	}
	return ""
}

func (h *Handler) authorizeTaskOrg(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	task, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return false
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return false
	}
	if !canAccessTaskOrg(r, task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return false
	}
	return true
}

// writeNexusBlocked detecta el typed error ErrNexusNotApproved y escribe
// HTTP 412 (precondition_failed) con nexus_request_id y nexus_status en el
// body para que el caller pueda actuar (esperar, refrescar, escalar).
// Devuelve true si manejó el error.
func writeNexusBlocked(w http.ResponseWriter, err error) bool {
	blocked, ok := AsNexusBlocked(err)
	if !ok {
		return false
	}
	httpjson.WriteJSON(w, http.StatusPreconditionFailed, map[string]any{
		"code":             "NEXUS_NOT_APPROVED",
		"message":          "execution requires the linked nexus to be approved",
		"nexus_request_id": blocked.NexusRequestID,
		"nexus_status":     blocked.NexusStatus,
		"reason":           blocked.Reason,
	})
	return true
}
