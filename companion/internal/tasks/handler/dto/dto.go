package dto

import (
	"encoding/json"
	"time"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	contracts "github.com/devpablocristo/platform/contracts/ai/go"
	"github.com/google/uuid"
)

type CreateTaskRequest struct {
	Title       string          `json:"title"`
	Goal        string          `json:"goal,omitempty"`
	Priority    string          `json:"priority,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	AssignedTo  string          `json:"assigned_to,omitempty"`
	Channel     string          `json:"channel,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	ContextJSON json.RawMessage `json:"context_json,omitempty"`
}

type TaskResponse struct {
	ID                 string          `json:"id"`
	OrgID              string          `json:"org_id,omitempty"`
	Title              string          `json:"title"`
	Goal               string          `json:"goal"`
	Status             string          `json:"status"`
	Priority           string          `json:"priority"`
	CreatedBy          string          `json:"created_by"`
	AssignedTo         string          `json:"assigned_to"`
	Channel            string          `json:"channel"`
	Summary            string          `json:"summary"`
	ContextJSON        json.RawMessage `json:"context_json"`
	NexusStatus        string          `json:"nexus_status,omitempty"`
	NexusLastCheckedAt *string         `json:"nexus_last_checked_at,omitempty"`
	NexusSyncError     string          `json:"nexus_sync_error,omitempty"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
	ClosedAt           *string         `json:"closed_at,omitempty"`
}

type MessageResponse struct {
	ID         string          `json:"id"`
	AuthorType string          `json:"author_type"`
	AuthorID   string          `json:"author_id"`
	Body       string          `json:"body"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

type ActionResponse struct {
	ID             string          `json:"id"`
	ActionType     string          `json:"action_type"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	NexusRequestID *string         `json:"nexus_request_id,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

type ArtifactResponse struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	URI       string          `json:"uri"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt string          `json:"created_at"`
}

type LinkedNexusRequestResponse struct {
	ActionID string                      `json:"action_id"`
	Request  *nexusclient.RequestSummary `json:"request,omitempty"`
}

type NexusSyncStateResponse struct {
	NexusRequestID      string `json:"nexus_request_id"`
	LastNexusStatus     string `json:"last_nexus_status,omitempty"`
	LastNexusHTTPStatus int    `json:"last_nexus_http_status"`
	LastCheckedAt       string `json:"last_checked_at"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	NextCheckAt         string `json:"next_check_at"`
}

type TaskExecutionPlanResponse struct {
	ConnectorID    string          `json:"connector_id"`
	Operation      string          `json:"operation"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

type TaskVerificationResultResponse struct {
	Status    string          `json:"status"`
	Summary   string          `json:"summary,omitempty"`
	CheckedAt string          `json:"checked_at"`
	Details   json.RawMessage `json:"details,omitempty"`
}

type TaskExecutionStateResponse struct {
	LastExecutionID     string                         `json:"last_execution_id"`
	LastExecutionStatus string                         `json:"last_execution_status"`
	Retryable           bool                           `json:"retryable"`
	RetryCount          int                            `json:"retry_count"`
	LastError           string                         `json:"last_error,omitempty"`
	LastAttemptedAt     string                         `json:"last_attempted_at"`
	VerificationResult  TaskVerificationResultResponse `json:"verification_result"`
}

type TaskDetailResponse struct {
	Task                TaskResponse                 `json:"task"`
	Messages            []MessageResponse            `json:"messages"`
	Actions             []ActionResponse             `json:"actions"`
	Artifacts           []ArtifactResponse           `json:"artifacts"`
	LinkedNexusRequests []LinkedNexusRequestResponse `json:"linked_nexus_requests"`
	NexusSync           *NexusSyncStateResponse      `json:"nexus_sync,omitempty"`
	ExecutionPlan       *TaskExecutionPlanResponse   `json:"execution_plan,omitempty"`
	ExecutionState      *TaskExecutionStateResponse  `json:"execution_state,omitempty"`
}

type AddMessageRequest struct {
	AuthorType string `json:"author_type,omitempty"`
	AuthorID   string `json:"author_id,omitempty"`
	Body       string `json:"body"`
}

// ChatRequest endpoint conversacional para el suscriptor.
type ChatRequest struct {
	TaskID         string `json:"task_id,omitempty"` // vacío = crear nueva conversación
	Message        string `json:"message"`
	Channel        string `json:"channel,omitempty"`         // default: "api"
	ProductSurface string `json:"product_surface,omitempty"` // "companion" | "ponti" | "pymes"
}

// ChatResponse respuesta del chat con tarea y mensajes.
//
// Incluye los campos canónicos del contrato compartido platform/contracts/ai/go
// (chat_id, reply, blocks) que consumen frontends/BFFs operativos.
// task + messages son estado operativo estable de la API de Companion.
type ChatResponse struct {
	// Canon contract fields (mirror github.com/devpablocristo/platform/contracts/ai/go ChatResponse).
	ChatID uuid.UUID             `json:"chat_id,omitempty"`
	Reply  string                `json:"reply"`
	Blocks []contracts.ChatBlock `json:"blocks,omitempty"`

	// Companion-specific extras: la task FSM + el log completo de mensajes.
	Task     TaskResponse      `json:"task"`
	Messages []MessageResponse `json:"messages"`
}

type InvestigateRequest struct {
	Note string `json:"note,omitempty"`
}

type ProposeRequest struct {
	Note           string `json:"note,omitempty"`
	TargetSystem   string `json:"target_system,omitempty"`
	TargetResource string `json:"target_resource,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
}

type ProposeResponse struct {
	Task        TaskResponse   `json:"task"`
	Action      ActionResponse `json:"action"`
	NexusSubmit struct {
		RequestID      string `json:"request_id"`
		Decision       string `json:"decision"`
		Status         string `json:"status"`
		RiskLevel      string `json:"risk_level"`
		DecisionReason string `json:"decision_reason"`
	} `json:"nexus_submit"`
}

type SetExecutionPlanRequest struct {
	ConnectorID    string          `json:"connector_id"`
	Operation      string          `json:"operation"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

type ExecuteTaskResponse struct {
	Task           TaskResponse                `json:"task"`
	Plan           TaskExecutionPlanResponse   `json:"plan"`
	Execution      ExecutionResultResponse     `json:"execution"`
	ExecutionState *TaskExecutionStateResponse `json:"execution_state,omitempty"`
}

type ExecutionResultResponse struct {
	ID             string          `json:"id"`
	ConnectorID    string          `json:"connector_id"`
	OrgID          string          `json:"org_id,omitempty"`
	ActorID        string          `json:"actor_id,omitempty"`
	Operation      string          `json:"operation"`
	Status         string          `json:"status"`
	ExternalRef    string          `json:"external_ref"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	Retryable      bool            `json:"retryable"`
	DurationMS     int64           `json:"duration_ms"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	NexusRequestID *string         `json:"nexus_request_id,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

func TaskToResponse(t domain.Task) TaskResponse {
	var closed *string
	var nexusLastChecked *string
	if t.ClosedAt != nil {
		s := t.ClosedAt.UTC().Format(time.RFC3339)
		closed = &s
	}
	if t.NexusLastCheckedAt != nil {
		s := t.NexusLastCheckedAt.UTC().Format(time.RFC3339)
		nexusLastChecked = &s
	}
	return TaskResponse{
		ID:                 t.ID.String(),
		OrgID:              t.OrgID,
		Title:              t.Title,
		Goal:               t.Goal,
		Status:             t.Status,
		Priority:           t.Priority,
		CreatedBy:          t.CreatedBy,
		AssignedTo:         t.AssignedTo,
		Channel:            t.Channel,
		Summary:            t.Summary,
		ContextJSON:        t.ContextJSON,
		NexusStatus:        t.NexusStatus,
		NexusLastCheckedAt: nexusLastChecked,
		NexusSyncError:     t.NexusSyncError,
		CreatedAt:          t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          t.UpdatedAt.UTC().Format(time.RFC3339),
		ClosedAt:           closed,
	}
}

func MessageToResponse(m domain.TaskMessage) MessageResponse {
	return MessageResponse{
		ID:         m.ID.String(),
		AuthorType: m.AuthorType,
		AuthorID:   m.AuthorID,
		Body:       m.Body,
		Metadata:   m.Metadata,
		CreatedAt:  m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ChatResponseFromResult arma la respuesta canónica del chat a partir de la
// task y la lista de mensajes. Popula los campos contract (chat_id, reply,
// blocks) además de task + messages específicos de Companion.
//
//   - chat_id viene de task.context_json.agent_conversation_id (Sprint 5'
//     wiring); si no hay, queda uuid.Nil y se omite por omitempty.
//   - reply es el último mensaje del assistant (author_type=system o
//     assistant); si no hay, queda vacío.
//   - blocks contiene un único ChatTextBlock con el reply (text-only v0.1).
func ChatResponseFromResult(task domain.Task, messages []domain.TaskMessage) ChatResponse {
	msgs := make([]MessageResponse, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, MessageToResponse(m))
	}

	resp := ChatResponse{
		Task:     TaskToResponse(task),
		Messages: msgs,
		ChatID:   extractChatID(task.ContextJSON),
	}

	// Buscar último mensaje assistant/system para reply + blocks.
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.AuthorType == "assistant" || m.AuthorType == "system" {
			resp.Reply = m.Body
			if m.Body != "" {
				resp.Blocks = []contracts.ChatBlock{{Type: "text", Text: m.Body}}
			}
			break
		}
	}
	return resp
}

// extractChatID lee task.context_json.agent_conversation_id (poblado en
// Sprint 5' al persistir el chat en agent_conversations). Devuelve uuid.Nil
// si no existe o no es parseable.
func extractChatID(raw json.RawMessage) uuid.UUID {
	if len(raw) == 0 {
		return uuid.Nil
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return uuid.Nil
	}
	v, ok := holder["agent_conversation_id"].(string)
	if !ok {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

func ActionToResponse(a domain.TaskAction) ActionResponse {
	var rid *string
	if a.NexusRequestID != nil {
		s := a.NexusRequestID.String()
		rid = &s
	}
	return ActionResponse{
		ID:             a.ID.String(),
		ActionType:     a.ActionType,
		Payload:        a.Payload,
		NexusRequestID: rid,
		ErrorMessage:   a.ErrorMessage,
		CreatedAt:      a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func ArtifactToResponse(a domain.TaskArtifact) ArtifactResponse {
	return ArtifactResponse{
		ID:        a.ID.String(),
		Kind:      a.Kind,
		URI:       a.URI,
		Payload:   a.Payload,
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func NexusSyncToResponse(s domain.TaskNexusSyncState) *NexusSyncStateResponse {
	return &NexusSyncStateResponse{
		NexusRequestID:      s.NexusRequestID.String(),
		LastNexusStatus:     s.LastNexusStatus,
		LastNexusHTTPStatus: s.LastNexusHTTPStatus,
		LastCheckedAt:       s.LastCheckedAt.UTC().Format(time.RFC3339),
		LastError:           s.LastError,
		ConsecutiveFailures: s.ConsecutiveFailures,
		NextCheckAt:         s.NextCheckAt.UTC().Format(time.RFC3339),
	}
}

func ExecutionPlanToResponse(plan domain.TaskExecutionPlan) *TaskExecutionPlanResponse {
	return &TaskExecutionPlanResponse{
		ConnectorID:    plan.ConnectorID.String(),
		Operation:      plan.Operation,
		Payload:        plan.Payload,
		IdempotencyKey: plan.IdempotencyKey,
		CreatedAt:      plan.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      plan.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func VerificationResultToResponse(result domain.TaskVerificationResult) TaskVerificationResultResponse {
	return TaskVerificationResultResponse{
		Status:    result.Status,
		Summary:   result.Summary,
		CheckedAt: result.CheckedAt.UTC().Format(time.RFC3339),
		Details:   result.Details,
	}
}

func ExecutionStateToResponse(state domain.TaskExecutionState) *TaskExecutionStateResponse {
	return &TaskExecutionStateResponse{
		LastExecutionID:     state.LastExecutionID.String(),
		LastExecutionStatus: state.LastExecutionStatus,
		Retryable:           state.Retryable,
		RetryCount:          state.RetryCount,
		LastError:           state.LastError,
		LastAttemptedAt:     state.LastAttemptedAt.UTC().Format(time.RFC3339),
		VerificationResult:  VerificationResultToResponse(state.VerificationResult),
	}
}

func ExecutionResultToResponse(result connectordomain.ExecutionResult) ExecutionResultResponse {
	var nexusRequestID *string
	if result.NexusRequestID != nil {
		s := result.NexusRequestID.String()
		nexusRequestID = &s
	}
	return ExecutionResultResponse{
		ID:             result.ID.String(),
		ConnectorID:    result.ConnectorID.String(),
		OrgID:          result.OrgID,
		ActorID:        result.ActorID,
		Operation:      result.Operation,
		Status:         result.Status,
		ExternalRef:    result.ExternalRef,
		Payload:        result.Payload,
		Result:         result.ResultJSON,
		Evidence:       result.EvidenceJSON,
		ErrorMessage:   result.ErrorMessage,
		Retryable:      result.Retryable,
		DurationMS:     result.DurationMS,
		IdempotencyKey: result.IdempotencyKey,
		NexusRequestID: nexusRequestID,
		CreatedAt:      result.CreatedAt.UTC().Format(time.RFC3339),
	}
}
