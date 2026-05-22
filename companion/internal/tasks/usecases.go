package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/platform/concurrency/go/worker"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
)

// Identidad del servicio Companion ante Governance (documentado en README).
const (
	CompanionRequesterType     = "service"
	CompanionRequesterID       = "nexus_companion"
	CompanionRequesterName     = "Nexus Companion"
	ActionTypePropose          = "companion.propose"
	TaskActionInvestigate      = "investigate"
	TaskActionPropose          = "propose"
	TaskActionSyncGovernance   = "sync_governance"
	TaskActionSetExecutionPlan = "set_execution_plan"
	TaskActionExecuteConnector = "execute_connector"
	TaskActionRetryExecution   = "retry_execution"
	TaskActionVerifyExecution  = "verify_execution"

	TaskArtifactConnectorExecution    = "connector_execution"
	TaskArtifactExecutionError        = "connector_execution_error"
	TaskArtifactExecutionVerification = "execution_verification"

	taskMemoryCurrentKey  = "current"
	taskMemoryKindFacts   = "task_facts"
	taskMemoryKindSummary = "task_summary"

	defaultGovernanceSyncInterval = 30 * time.Second
	maxGovernanceSyncBackoff      = 10 * time.Minute
)

// marshalOrEmpty serializa v a JSON. Si falla (typically map con channels o
// funcs metidos por error), loguea y devuelve "{}" para que el caller no
// rompa pipelines de proyección/audit por un payload mal formado.
func marshalOrEmpty(label string, v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("tasks marshal payload failed", "label", label, "error", err)
		return json.RawMessage(`{}`)
	}
	return b
}

type governanceGateway interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body governanceclient.SubmitRequestBody) (governanceclient.SubmitResponse, error)
	GetRequest(ctx context.Context, id string) (governanceclient.RequestSummary, int, error)
	ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error)
}

type taskExecutor interface {
	GetConnector(ctx context.Context, id uuid.UUID) (connectordomain.Connector, error)
	BuildActionBinding(ctx context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error)
	Execute(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error)
}

type taskMemoryWriter interface {
	UpsertTaskMemory(ctx context.Context, taskID uuid.UUID, kind, key string, contentText string, payload json.RawMessage) error
}

// agentMemoryWriter es el puerto al que el chat flow persiste conversaciones
// durables (agent_conversations + agent_conversation_messages). Es
// best-effort: errores se logean pero no rompen el chat.
type agentMemoryWriter interface {
	StartConversation(ctx context.Context, orgID, userID, productSurface, title string) (uuid.UUID, error)
	AppendMessage(ctx context.Context, conversationID uuid.UUID, orgID, role, content string) error
}

// ChatOrchestrator interfaz del runtime del compañero.
type ChatOrchestrator interface {
	Run(ctx context.Context, in OrchestratorInput) (OrchestratorResult, error)
}

// OrchestratorInput entrada para el runtime.
type OrchestratorInput struct {
	UserID         string
	OrgID          string
	AuthScopes     []string
	Message        string
	Messages       []domain.TaskMessage
	TaskID         *uuid.UUID // opcional: vincula el trace a una task
	ProductSurface string     // opcional: "companion" (default) | "ponti" | "pymes" — afecta routing
}

// OrchestratorResult resultado del runtime.
type OrchestratorResult struct {
	Reply string
}

// Usecases lógica de tareas e integración con Nexus governance.
type Usecases struct {
	repo                   Repository
	governance             governanceGateway
	orchestrator           ChatOrchestrator // opcional en tests; productivo usa Gemini
	executor               taskExecutor
	taskMemory             taskMemoryWriter
	agentMemory            agentMemoryWriter
	governanceSyncInterval time.Duration
}

func NewUsecases(repo Repository, governance governanceGateway) *Usecases {
	return &Usecases{
		repo:                   repo,
		governance:             governance,
		governanceSyncInterval: defaultGovernanceSyncInterval,
	}
}

// SetOrchestrator inyecta el runtime del compañero. Opcional: si no se llama, Chat solo persiste.
func (u *Usecases) SetOrchestrator(o ChatOrchestrator) {
	u.orchestrator = o
}

func (u *Usecases) SetExecutor(executor taskExecutor) {
	u.executor = executor
}

func (u *Usecases) SetTaskMemory(writer taskMemoryWriter) {
	u.taskMemory = writer
}

// SetAgentMemory inyecta el persistor de conversaciones durables. Opcional:
// si no se llama, el chat sigue funcionando pero no se persiste en las
// tablas agent_*.
func (u *Usecases) SetAgentMemory(writer agentMemoryWriter) {
	u.agentMemory = writer
}

func (u *Usecases) SetGovernanceSyncInterval(interval time.Duration) {
	if interval <= 0 {
		u.governanceSyncInterval = defaultGovernanceSyncInterval
		return
	}
	u.governanceSyncInterval = interval
}

type CreateTaskInput struct {
	OrgID       string
	Title       string
	Goal        string
	Priority    string
	CreatedBy   string
	AssignedTo  string
	Channel     string
	Summary     string
	ContextJSON json.RawMessage
}

func (u *Usecases) Create(ctx context.Context, in CreateTaskInput) (domain.Task, error) {
	if in.Title == "" {
		return domain.Task{}, fmt.Errorf("title is required")
	}
	t := domain.Task{
		Title:       in.Title,
		OrgID:       in.OrgID,
		Goal:        in.Goal,
		Status:      domain.TaskStatusNew,
		Priority:    in.Priority,
		CreatedBy:   in.CreatedBy,
		AssignedTo:  in.AssignedTo,
		Channel:     in.Channel,
		Summary:     in.Summary,
		ContextJSON: in.ContextJSON,
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if len(t.ContextJSON) == 0 {
		t.ContextJSON = json.RawMessage(`{}`)
	}
	out, err := u.repo.CreateTask(ctx, t)
	if err != nil {
		return domain.Task{}, err
	}
	u.syncTaskMemory(ctx, out.ID, "create")
	slog.Info("companion task created", "task_id", out.ID.String(), "title", out.Title, "created_by", out.CreatedBy)
	return out, nil
}

func (u *Usecases) List(ctx context.Context, orgID string, limit int) ([]domain.Task, error) {
	return u.repo.ListTasks(ctx, orgID, limit)
}

func (u *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	return u.repo.GetTaskByID(ctx, id)
}

type LinkedGovernanceRequest struct {
	ActionID uuid.UUID                        `json:"action_id"`
	Request  *governanceclient.RequestSummary `json:"request,omitempty"`
}

type TaskDetail struct {
	Task                     domain.Task                     `json:"task"`
	Messages                 []domain.TaskMessage            `json:"messages"`
	Actions                  []domain.TaskAction             `json:"actions"`
	Artifacts                []domain.TaskArtifact           `json:"artifacts"`
	LinkedGovernanceRequests []LinkedGovernanceRequest       `json:"linked_governance_requests"`
	GovernanceSync           *domain.TaskGovernanceSyncState `json:"governance_sync,omitempty"`
	ExecutionPlan            *domain.TaskExecutionPlan       `json:"execution_plan,omitempty"`
	ExecutionState           *domain.TaskExecutionState      `json:"execution_state,omitempty"`
}

func (u *Usecases) GetDetail(ctx context.Context, id uuid.UUID) (TaskDetail, error) {
	var out TaskDetail
	t, err := u.repo.GetTaskByID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Task = t
	out.Messages, err = u.repo.ListMessagesByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Actions, err = u.repo.ListActionsByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Artifacts, err = u.repo.ListArtifactsByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	state, stateErr := u.repo.GetGovernanceSyncState(ctx, id)
	if stateErr == nil {
		out.GovernanceSync = &state
	} else if !domainerr.IsNotFound(stateErr) {
		return out, stateErr
	}
	plan, planErr := u.repo.GetExecutionPlan(ctx, id)
	if planErr == nil {
		out.ExecutionPlan = &plan
	} else if !domainerr.IsNotFound(planErr) {
		return out, planErr
	}
	executionState, executionStateErr := u.repo.GetExecutionState(ctx, id)
	if executionStateErr == nil {
		out.ExecutionState = &executionState
	} else if !domainerr.IsNotFound(executionStateErr) {
		return out, executionStateErr
	}
	seen := make(map[uuid.UUID]struct{})
	for _, a := range out.Actions {
		if a.GovernanceRequestID == nil {
			continue
		}
		rid := *a.GovernanceRequestID
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		sum, st, gErr := u.governance.GetRequest(ctx, rid.String())
		lr := LinkedGovernanceRequest{ActionID: a.ID}
		if gErr != nil {
			slog.Error("governance get request failed", "error", gErr, "request_id", rid)
			out.LinkedGovernanceRequests = append(out.LinkedGovernanceRequests, lr)
			continue
		}
		if st == 404 {
			out.LinkedGovernanceRequests = append(out.LinkedGovernanceRequests, lr)
			continue
		}
		lr.Request = &sum
		out.LinkedGovernanceRequests = append(out.LinkedGovernanceRequests, lr)
	}
	return out, nil
}

type AddMessageInput struct {
	AuthorType string
	AuthorID   string
	Body       string
}

func (u *Usecases) AddMessage(ctx context.Context, taskID uuid.UUID, in AddMessageInput) (domain.TaskMessage, error) {
	if in.Body == "" {
		return domain.TaskMessage{}, fmt.Errorf("body is required")
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskMessage{}, err
	}
	at := in.AuthorType
	if at == "" {
		at = "user"
	}
	return u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     taskID,
		AuthorType: at,
		AuthorID:   in.AuthorID,
		Body:       in.Body,
	})
}

// ChatInput entrada para el endpoint de chat conversacional.
type ChatInput struct {
	TaskID         *uuid.UUID // nil = crear tarea nueva
	UserID         string
	OrgID          string
	AuthScopes     []string
	Message        string
	Channel        string // "api", "watcher", "whatsapp", product-specific channels, etc.
	ProductSurface string // opcional: "companion" | "ponti" | "pymes". Afecta routing del agent.
}

// ChatResult resultado del chat.
type ChatResult struct {
	Task     domain.Task
	Messages []domain.TaskMessage
}

// agentConversationContextKey nombre del field en task.context_json que guarda
// el agent_conversations.id asociado a la task. Permite reusar la misma
// conversation_id en mensajes sucesivos del mismo task.
const agentConversationContextKey = "agent_conversation_id"

// Chat combina crear/reusar tarea + agregar mensaje del usuario.
// Es el endpoint principal para la interfaz conversacional del suscriptor.
func (u *Usecases) Chat(ctx context.Context, in ChatInput) (ChatResult, error) {
	if in.Message == "" {
		return ChatResult{}, fmt.Errorf("message is required")
	}

	var t domain.Task
	var err error
	newTask := false

	if in.TaskID != nil {
		// Reusar tarea existente
		t, err = u.repo.GetTaskByID(ctx, *in.TaskID)
		if err != nil {
			return ChatResult{}, err
		}
	} else {
		// Crear tarea nueva con el primer mensaje como título
		title := in.Message
		if len(title) > 80 {
			title = title[:80]
		}
		channel := in.Channel
		if channel == "" {
			channel = "api"
		}
		t, err = u.repo.CreateTask(ctx, domain.Task{
			Title:     title,
			OrgID:     in.OrgID,
			Status:    domain.TaskStatusNew,
			Priority:  "normal",
			CreatedBy: in.UserID,
			Channel:   channel,
		})
		if err != nil {
			return ChatResult{}, fmt.Errorf("create chat task: %w", err)
		}
		newTask = true
		slog.Info("companion chat started", "task_id", t.ID.String(), "user_id", in.UserID)
	}

	// Conversación durable en agent_conversations (best-effort). Si arrancamos
	// task nueva: creamos conversation y stasheamos su id en task.context_json
	// para reusar en mensajes sucesivos. Si task existente: reusamos el id ya
	// guardado.
	convID := u.ensureAgentConversation(ctx, &t, in, newTask)

	// Agregar mensaje del usuario
	_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     t.ID,
		AuthorType: "user",
		AuthorID:   in.UserID,
		Body:       in.Message,
	})
	if err != nil {
		return ChatResult{}, fmt.Errorf("insert chat message: %w", err)
	}
	u.persistAgentMessage(ctx, convID, t.OrgID, "user", in.Message)

	// Si hay orchestrator, generar respuesta del compañero
	if u.orchestrator != nil {
		existingMsgs, listErr := u.repo.ListMessagesByTaskID(ctx, t.ID)
		if listErr != nil {
			slog.Error("chat list messages for orchestrator", "error", listErr)
		} else {
			orgID := in.OrgID
			if orgID == "" {
				orgID = t.OrgID
			}
			taskID := t.ID
			result, runErr := u.orchestrator.Run(ctx, OrchestratorInput{
				UserID:         in.UserID,
				OrgID:          orgID,
				AuthScopes:     in.AuthScopes,
				Message:        in.Message,
				Messages:       existingMsgs,
				TaskID:         &taskID,
				ProductSurface: in.ProductSurface,
			})
			if runErr != nil {
				slog.Error("orchestrator failed", "error", runErr)
				return ChatResult{}, fmt.Errorf("run companion runtime: %w", runErr)
			} else if result.Reply != "" {
				// Guardar respuesta del compañero como mensaje del sistema
				_, insertErr := u.repo.InsertMessage(ctx, domain.TaskMessage{
					TaskID:     t.ID,
					AuthorType: "system",
					AuthorID:   "nexus",
					Body:       result.Reply,
				})
				if insertErr != nil {
					slog.Error("insert orchestrator reply", "error", insertErr)
				}
				u.persistAgentMessage(ctx, convID, t.OrgID, "assistant", result.Reply)
			}
		}
	}

	// Devolver hilo completo (incluyendo respuesta del compañero si hubo)
	msgs, err := u.repo.ListMessagesByTaskID(ctx, t.ID)
	if err != nil {
		return ChatResult{}, fmt.Errorf("list chat messages: %w", err)
	}

	return ChatResult{Task: t, Messages: msgs}, nil
}

// ensureAgentConversation obtiene o crea la conversación durable asociada a la
// task. Si la task ya tenía un id stasheado en context_json, lo reusa. Si no,
// crea una nueva y persiste el id en context_json. Best-effort: nunca falla el
// chat, solo logea.
func (u *Usecases) ensureAgentConversation(ctx context.Context, t *domain.Task, in ChatInput, newTask bool) uuid.UUID {
	if u.agentMemory == nil {
		return uuid.Nil
	}
	if !newTask {
		if existing := extractAgentConversationID(t.ContextJSON); existing != uuid.Nil {
			return existing
		}
	}
	productSurface := in.ProductSurface
	if productSurface == "" {
		productSurface = "companion"
	}
	convID, err := u.agentMemory.StartConversation(ctx, t.OrgID, in.UserID, productSurface, t.Title)
	if err != nil {
		slog.Error("agent memory start conversation", "error", err, "task_id", t.ID)
		return uuid.Nil
	}
	if updated, ok := mergeAgentConversationID(t.ContextJSON, convID); ok {
		t.ContextJSON = updated
		if _, err := u.repo.UpdateTask(ctx, *t); err != nil {
			slog.Error("update task context with conversation_id", "error", err, "task_id", t.ID)
		}
	}
	return convID
}

func (u *Usecases) persistAgentMessage(ctx context.Context, convID uuid.UUID, orgID, role, content string) {
	if u.agentMemory == nil || convID == uuid.Nil || content == "" {
		return
	}
	if err := u.agentMemory.AppendMessage(ctx, convID, orgID, role, content); err != nil {
		slog.Error("agent memory append message", "error", err, "conversation_id", convID, "role", role)
	}
}

func extractAgentConversationID(raw json.RawMessage) uuid.UUID {
	if len(raw) == 0 {
		return uuid.Nil
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return uuid.Nil
	}
	v, ok := holder[agentConversationContextKey].(string)
	if !ok {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

func mergeAgentConversationID(raw json.RawMessage, convID uuid.UUID) (json.RawMessage, bool) {
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[agentConversationContextKey] = convID.String()
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
}

type InvestigateInput struct {
	Note string
}

func (u *Usecases) applyTaskEvent(ctx context.Context, t domain.Task, event string) (domain.Task, error) {
	to, err := companionTaskMachine().Transition(t.Status, event)
	if err != nil {
		return domain.Task{}, ErrInvalidTaskState
	}
	t.Status = to
	if to == domain.TaskStatusDone || to == domain.TaskStatusFailed {
		now := time.Now().UTC()
		t.ClosedAt = &now
	} else {
		t.ClosedAt = nil
	}
	return u.repo.UpdateTask(ctx, t)
}

func (u *Usecases) governanceSyncIntervalOrDefault() time.Duration {
	if u.governanceSyncInterval <= 0 {
		return defaultGovernanceSyncInterval
	}
	return u.governanceSyncInterval
}

func nextGovernanceSyncAt(now time.Time, interval time.Duration, consecutiveFailures int) time.Time {
	if interval <= 0 {
		interval = defaultGovernanceSyncInterval
	}
	if consecutiveFailures <= 0 {
		return now.Add(interval)
	}
	delay := interval
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxGovernanceSyncBackoff/2 {
			delay = maxGovernanceSyncBackoff
			break
		}
		delay *= 2
	}
	if delay > maxGovernanceSyncBackoff {
		delay = maxGovernanceSyncBackoff
	}
	return now.Add(delay)
}

func governanceSnapshotChanged(prev *domain.TaskGovernanceSyncState, next domain.TaskGovernanceSyncState) bool {
	if prev == nil {
		return next.GovernanceRequestID != uuid.Nil ||
			next.LastGovernanceStatus != "" ||
			next.LastGovernanceHTTPStatus != 0 ||
			next.LastError != ""
	}
	return prev.GovernanceRequestID != next.GovernanceRequestID ||
		prev.LastGovernanceStatus != next.LastGovernanceStatus ||
		prev.LastGovernanceHTTPStatus != next.LastGovernanceHTTPStatus ||
		prev.LastError != next.LastError
}

func executionPlanChanged(prev *domain.TaskExecutionPlan, next domain.TaskExecutionPlan) bool {
	if prev == nil {
		return next.ConnectorID != uuid.Nil || next.Operation != "" || len(next.Payload) > 0 || next.IdempotencyKey != ""
	}
	return prev.ConnectorID != next.ConnectorID ||
		prev.Operation != next.Operation ||
		!bytes.Equal(prev.Payload, next.Payload) ||
		prev.IdempotencyKey != next.IdempotencyKey
}

func isApprovedGovernanceStatus(status string) bool {
	switch normalizeGovernanceStatus(status) {
	case "allowed", "approved", "executed":
		return true
	default:
		return false
	}
}

func (u *Usecases) getExecutionPlan(ctx context.Context, taskID uuid.UUID) (*domain.TaskExecutionPlan, error) {
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		return &plan, nil
	}
	if domainerr.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

func (u *Usecases) getExecutionState(ctx context.Context, taskID uuid.UUID) (*domain.TaskExecutionState, error) {
	state, err := u.repo.GetExecutionState(ctx, taskID)
	if err == nil {
		return &state, nil
	}
	if domainerr.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

type taskMemorySnapshot struct {
	Task           domain.Task
	GovernanceSync *domain.TaskGovernanceSyncState
	ExecutionPlan  *domain.TaskExecutionPlan
	ExecutionState *domain.TaskExecutionState
}

func (u *Usecases) loadTaskMemorySnapshot(ctx context.Context, taskID uuid.UUID) (taskMemorySnapshot, error) {
	task, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return taskMemorySnapshot{}, err
	}
	snapshot := taskMemorySnapshot{Task: task}

	governanceSync, err := u.repo.GetGovernanceSyncState(ctx, taskID)
	if err == nil {
		snapshot.GovernanceSync = &governanceSync
		snapshot.Task.GovernanceStatus = governanceSync.LastGovernanceStatus
		snapshot.Task.GovernanceLastCheckedAt = &governanceSync.LastCheckedAt
		snapshot.Task.GovernanceSyncError = governanceSync.LastError
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	executionPlan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		snapshot.ExecutionPlan = &executionPlan
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	executionState, err := u.repo.GetExecutionState(ctx, taskID)
	if err == nil {
		snapshot.ExecutionState = &executionState
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	return snapshot, nil
}

func nextTaskStep(snapshot taskMemorySnapshot) string {
	switch snapshot.Task.Status {
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		if snapshot.ExecutionPlan == nil {
			return "define execution plan and propose to governance"
		}
		return "propose to governance"
	case domain.TaskStatusWaitingForApproval:
		return "wait for governance resolution or sync from governance"
	case domain.TaskStatusWaitingForInput:
		if snapshot.ExecutionPlan != nil {
			return "execute the approved task manually"
		}
		return "provide the missing execution input"
	case domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return "observe execution and verification"
	case domain.TaskStatusFailed:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.Retryable && isApprovedGovernanceStatus(snapshot.Task.GovernanceStatus) {
			return "inspect failure and retry execution"
		}
		if snapshot.Task.GovernanceStatus == "rejected" || snapshot.Task.GovernanceStatus == "denied" {
			return "inspect governance decision and adjust the task"
		}
		return "inspect failure details"
	case domain.TaskStatusDone:
		return "closed"
	default:
		return "inspect task status"
	}
}

func buildTaskSummary(snapshot taskMemorySnapshot) string {
	title := strings.TrimSpace(snapshot.Task.Title)
	if title == "" {
		title = snapshot.Task.ID.String()
	}
	prefix := fmt.Sprintf("Task %q", title)

	switch snapshot.Task.Status {
	case domain.TaskStatusNew:
		return fmt.Sprintf("%s was created and is ready for investigation.", prefix)
	case domain.TaskStatusInvestigating:
		return fmt.Sprintf("%s is under investigation. Next step: %s.", prefix, nextTaskStep(snapshot))
	case domain.TaskStatusWaitingForApproval:
		if snapshot.GovernanceSync != nil && snapshot.GovernanceSync.GovernanceRequestID != uuid.Nil {
			return fmt.Sprintf("%s is waiting for Governance. Request %s is currently %s.", prefix, snapshot.GovernanceSync.GovernanceRequestID.String(), formatStatusForMemory(snapshot.GovernanceSync.LastGovernanceStatus))
		}
		return fmt.Sprintf("%s is waiting for Governance approval.", prefix)
	case domain.TaskStatusWaitingForInput:
		if snapshot.ExecutionPlan != nil {
			return fmt.Sprintf("%s is approved and ready for manual execution via %s.", prefix, snapshot.ExecutionPlan.Operation)
		}
		return fmt.Sprintf("%s is approved and waiting for additional input.", prefix)
	case domain.TaskStatusExecuting:
		return fmt.Sprintf("%s is executing the configured connector action.", prefix)
	case domain.TaskStatusVerifying:
		return fmt.Sprintf("%s finished execution and is being verified.", prefix)
	case domain.TaskStatusDone:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.VerificationResult.Status == domain.VerificationStatusVerified {
			return fmt.Sprintf("%s completed successfully and the latest execution was verified.", prefix)
		}
		if isApprovedGovernanceStatus(snapshot.Task.GovernanceStatus) {
			return fmt.Sprintf("%s completed successfully after Governance resolved %s.", prefix, formatStatusForMemory(snapshot.Task.GovernanceStatus))
		}
		return fmt.Sprintf("%s completed successfully.", prefix)
	case domain.TaskStatusFailed:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.LastError != "" {
			if snapshot.ExecutionState.Retryable {
				return fmt.Sprintf("%s failed during execution. Retry is available. Last error: %s.", prefix, snapshot.ExecutionState.LastError)
			}
			return fmt.Sprintf("%s failed during execution. Last error: %s.", prefix, snapshot.ExecutionState.LastError)
		}
		if snapshot.Task.GovernanceStatus != "" {
			return fmt.Sprintf("%s failed because Governance resolved %s.", prefix, formatStatusForMemory(snapshot.Task.GovernanceStatus))
		}
		return fmt.Sprintf("%s failed and needs operator attention.", prefix)
	default:
		return fmt.Sprintf("%s is in status %s.", prefix, formatStatusForMemory(snapshot.Task.Status))
	}
}

func formatStatusForMemory(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "unknown"
	}
	return strings.ReplaceAll(status, "_", " ")
}

func buildTaskFactsPayload(snapshot taskMemorySnapshot, reason string) json.RawMessage {
	payload := map[string]any{
		"projection_reason":  reason,
		"task_id":            snapshot.Task.ID.String(),
		"title":              snapshot.Task.Title,
		"goal":               snapshot.Task.Goal,
		"status":             snapshot.Task.Status,
		"priority":           snapshot.Task.Priority,
		"created_by":         snapshot.Task.CreatedBy,
		"assigned_to":        snapshot.Task.AssignedTo,
		"channel":            snapshot.Task.Channel,
		"summary":            snapshot.Task.Summary,
		"next_step":          nextTaskStep(snapshot),
		"attention_required": snapshot.Task.Status == domain.TaskStatusWaitingForApproval || snapshot.Task.Status == domain.TaskStatusWaitingForInput || snapshot.Task.Status == domain.TaskStatusFailed,
		"updated_at":         snapshot.Task.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if snapshot.Task.CreatedAt.IsZero() {
		payload["created_at"] = ""
	} else {
		payload["created_at"] = snapshot.Task.CreatedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.ClosedAt != nil {
		payload["closed_at"] = snapshot.Task.ClosedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.GovernanceStatus != "" {
		payload["governance_status"] = snapshot.Task.GovernanceStatus
	}
	if snapshot.Task.GovernanceLastCheckedAt != nil {
		payload["governance_last_checked_at"] = snapshot.Task.GovernanceLastCheckedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.GovernanceSyncError != "" {
		payload["governance_sync_error"] = snapshot.Task.GovernanceSyncError
	}
	if snapshot.GovernanceSync != nil {
		payload["governance"] = map[string]any{
			"governance_request_id": snapshot.GovernanceSync.GovernanceRequestID.String(),
			"status":                snapshot.GovernanceSync.LastGovernanceStatus,
			"http_status":           snapshot.GovernanceSync.LastGovernanceHTTPStatus,
			"last_checked_at":       snapshot.GovernanceSync.LastCheckedAt.UTC().Format(time.RFC3339),
			"next_check_at":         snapshot.GovernanceSync.NextCheckAt.UTC().Format(time.RFC3339),
			"consecutive_failures":  snapshot.GovernanceSync.ConsecutiveFailures,
			"last_error":            snapshot.GovernanceSync.LastError,
		}
	}
	if snapshot.ExecutionPlan != nil {
		payload["execution_plan"] = map[string]any{
			"connector_id":    snapshot.ExecutionPlan.ConnectorID.String(),
			"operation":       snapshot.ExecutionPlan.Operation,
			"payload":         json.RawMessage(snapshot.ExecutionPlan.Payload),
			"idempotency_key": snapshot.ExecutionPlan.IdempotencyKey,
			"updated_at":      snapshot.ExecutionPlan.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	if snapshot.ExecutionState != nil {
		payload["execution"] = map[string]any{
			"last_execution_id":       snapshot.ExecutionState.LastExecutionID.String(),
			"last_execution_status":   snapshot.ExecutionState.LastExecutionStatus,
			"retryable":               snapshot.ExecutionState.Retryable,
			"retry_count":             snapshot.ExecutionState.RetryCount,
			"last_error":              snapshot.ExecutionState.LastError,
			"last_attempted_at":       snapshot.ExecutionState.LastAttemptedAt.UTC().Format(time.RFC3339),
			"verification_status":     snapshot.ExecutionState.VerificationResult.Status,
			"verification_summary":    snapshot.ExecutionState.VerificationResult.Summary,
			"verification_checked_at": snapshot.ExecutionState.VerificationResult.CheckedAt.UTC().Format(time.RFC3339),
		}
	}
	return marshalOrEmpty("task_facts", payload)
}

func (u *Usecases) syncTaskMemory(ctx context.Context, taskID uuid.UUID, reason string) {
	if u.taskMemory == nil {
		return
	}
	snapshot, err := u.loadTaskMemorySnapshot(ctx, taskID)
	if err != nil {
		slog.Warn("companion project task memory failed", "task_id", taskID.String(), "reason", reason, "error", err)
		return
	}
	summaryPayload := marshalOrEmpty("task_summary", map[string]any{
		"projection_reason": reason,
		"status":            snapshot.Task.Status,
		"governance_status": snapshot.Task.GovernanceStatus,
		"next_step":         nextTaskStep(snapshot),
	})
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindSummary, taskMemoryCurrentKey, buildTaskSummary(snapshot), summaryPayload); err != nil {
		slog.Warn("companion upsert task summary failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindFacts, taskMemoryCurrentKey, "", buildTaskFactsPayload(snapshot, reason)); err != nil {
		slog.Warn("companion upsert task facts failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
}

func buildGovernanceSyncActionPayload(origin string, prev *domain.TaskGovernanceSyncState, next domain.TaskGovernanceSyncState, beforeStatus, afterStatus, event string) json.RawMessage {
	type syncSnapshot struct {
		GovernanceRequestID string `json:"governance_request_id,omitempty"`
		Status              string `json:"status,omitempty"`
		HTTPStatus          int    `json:"http_status,omitempty"`
		Error               string `json:"error,omitempty"`
	}
	payload := map[string]any{
		"origin":             origin,
		"task_status_before": beforeStatus,
		"task_status_after":  afterStatus,
	}
	if event != "" {
		payload["transition_event"] = event
	}
	current := syncSnapshot{
		Status:     next.LastGovernanceStatus,
		HTTPStatus: next.LastGovernanceHTTPStatus,
		Error:      next.LastError,
	}
	if next.GovernanceRequestID != uuid.Nil {
		current.GovernanceRequestID = next.GovernanceRequestID.String()
	}
	payload["current"] = current
	if prev != nil {
		previous := syncSnapshot{
			Status:     prev.LastGovernanceStatus,
			HTTPStatus: prev.LastGovernanceHTTPStatus,
			Error:      prev.LastError,
		}
		if prev.GovernanceRequestID != uuid.Nil {
			previous.GovernanceRequestID = prev.GovernanceRequestID.String()
		}
		payload["previous"] = previous
	}
	return marshalOrEmpty("governance_sync_payload", payload)
}

func (u *Usecases) latestGovernanceRequestIDForTask(ctx context.Context, taskID uuid.UUID, state *domain.TaskGovernanceSyncState) (uuid.UUID, error) {
	if state != nil && state.GovernanceRequestID != uuid.Nil {
		return state.GovernanceRequestID, nil
	}
	return u.repo.LatestProposeGovernanceRequestID(ctx, taskID)
}

func (u *Usecases) persistGovernanceSyncAction(ctx context.Context, taskID uuid.UUID, governanceRequestID uuid.UUID, origin string, prev *domain.TaskGovernanceSyncState, next domain.TaskGovernanceSyncState, beforeStatus, afterStatus, event string) {
	payload := buildGovernanceSyncActionPayload(origin, prev, next, beforeStatus, afterStatus, event)
	governanceRequestIDCopy := governanceRequestID
	if _, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:              taskID,
		ActionType:          TaskActionSyncGovernance,
		Payload:             payload,
		GovernanceRequestID: &governanceRequestIDCopy,
	}); err != nil {
		slog.Warn("companion sync_governance action failed", "task_id", taskID.String(), "governance_request_id", governanceRequestID.String(), "error", err)
	}
}

func (u *Usecases) syncTaskWithGovernance(ctx context.Context, t domain.Task, origin string) (domain.Task, *domain.TaskGovernanceSyncState, error) {
	if t.Status != domain.TaskStatusWaitingForApproval {
		return t, nil, nil
	}

	var prevState *domain.TaskGovernanceSyncState
	currentState, err := u.repo.GetGovernanceSyncState(ctx, t.ID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.Task{}, nil, err
	}

	rid, err := u.latestGovernanceRequestIDForTask(ctx, t.ID, prevState)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return t, prevState, nil
		}
		return domain.Task{}, prevState, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskGovernanceSyncState{
		TaskID:              t.ID,
		GovernanceRequestID: rid,
		LastCheckedAt:       now,
		NextCheckAt:         nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastGovernanceStatus = prevState.LastGovernanceStatus
		nextState.LastGovernanceHTTPStatus = prevState.LastGovernanceHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, st, gErr := u.governance.GetRequest(ctx, rid.String())
	beforeStatus := t.Status
	appliedEvent := ""

	if gErr != nil {
		nextState.LastGovernanceHTTPStatus = st
		nextState.LastError = gErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertGovernanceSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if governanceSnapshotChanged(prevState, stateOut) {
			u.persistGovernanceSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "governance_sync_error")
		}
		return domain.Task{}, &stateOut, fmt.Errorf("governance get request: %w", gErr)
	}

	nextState.LastGovernanceHTTPStatus = st

	if st == http.StatusNotFound {
		nextState.LastError = "governance request not found"
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertGovernanceSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if governanceSnapshotChanged(prevState, stateOut) {
			u.persistGovernanceSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "governance_sync_not_found")
		}
		t.GovernanceStatus = stateOut.LastGovernanceStatus
		t.GovernanceLastCheckedAt = &stateOut.LastCheckedAt
		t.GovernanceSyncError = stateOut.LastError
		return t, &stateOut, nil
	}
	if normalizedStatus := normalizeGovernanceStatus(sum.Status); normalizedStatus != "" {
		nextState.LastGovernanceStatus = normalizedStatus
	}

	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), 0)

	plan, planErr := u.getExecutionPlan(ctx, t.ID)
	if planErr != nil {
		return domain.Task{}, prevState, planErr
	}
	ev, apply := eventFromGovernanceRequestStatusWithExecutionPlan(sum.Status, plan != nil)
	if apply {
		appliedEvent = ev
		t, err = u.applyTaskEvent(ctx, t, ev)
		if err != nil {
			return domain.Task{}, prevState, err
		}
	}

	stateOut, upErr := u.repo.UpsertGovernanceSyncState(ctx, nextState)
	if upErr != nil {
		return domain.Task{}, prevState, upErr
	}
	if governanceSnapshotChanged(prevState, stateOut) || beforeStatus != t.Status {
		u.persistGovernanceSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
		u.syncTaskMemory(ctx, t.ID, "governance_sync")
	}
	t.GovernanceStatus = stateOut.LastGovernanceStatus
	t.GovernanceLastCheckedAt = &stateOut.LastCheckedAt
	t.GovernanceSyncError = stateOut.LastError

	slog.Info("companion task synced from governance",
		"task_id", t.ID.String(),
		"governance_request_id", rid.String(),
		"governance_status", stateOut.LastGovernanceStatus,
		"task_status", t.Status,
		"origin", origin,
	)
	return t, &stateOut, nil
}

func (u *Usecases) Investigate(ctx context.Context, taskID uuid.UUID, in InvestigateInput) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, err = u.applyTaskEvent(ctx, t, evInvestigate)
	if err != nil {
		return domain.Task{}, err
	}
	if in.Note != "" {
		_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
			TaskID:     taskID,
			AuthorType: "system",
			AuthorID:   "nexus_companion",
			Body:       in.Note,
		})
		if err != nil {
			return domain.Task{}, err
		}
	}
	u.syncTaskMemory(ctx, taskID, "investigate")
	return t, nil
}

type ProposeInput struct {
	Note           string
	TargetSystem   string
	TargetResource string
	SessionID      string
}

func (u *Usecases) Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, governanceclient.SubmitResponse, error) {
	var zeroA domain.TaskAction
	var zeroSub governanceclient.SubmitResponse
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, err
	}
	switch t.Status {
	case domain.TaskStatusDone, domain.TaskStatusFailed:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	case domain.TaskStatusWaitingForApproval:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		// ok
	default:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	}

	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domain.Task{}, zeroA, zeroSub, fmt.Errorf("execution plan is required before propose")
		}
		return domain.Task{}, zeroA, zeroSub, err
	}
	if u.executor == nil {
		return domain.Task{}, zeroA, zeroSub, fmt.Errorf("task execution is not configured")
	}
	idempotencyKey := plan.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = defaultExecutionIdempotencyKey(t.ID, nil)
	}
	binding, bindingHash, err := u.executor.BuildActionBinding(ctx, connectordomain.ExecutionSpec{
		ConnectorID:    plan.ConnectorID,
		OrgID:          t.OrgID,
		ActorID:        executionActorID(t),
		ProductSurface: "companion",
		Operation:      plan.Operation,
		Payload:        plan.Payload,
		IdempotencyKey: idempotencyKey,
		TaskID:         &t.ID,
	})
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, fmt.Errorf("build action binding: %w", err)
	}

	payload := map[string]any{
		"note":         in.Note,
		"binding_hash": bindingHash,
	}
	pj := marshalOrEmpty("propose_action_payload", payload)
	action, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionPropose,
		Payload:    pj,
	})
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, err
	}

	nexusMeta := map[string]any{
		"origin":       "companion",
		"task_id":      taskID.String(),
		"proposed_by":  CompanionRequesterID,
		"human_owner":  t.CreatedBy,
		"action_id":    action.ID.String(),
		"binding_hash": bindingHash,
	}
	if in.SessionID != "" {
		nexusMeta["session_id"] = in.SessionID
	}
	params := map[string]any{
		"org_id":         t.OrgID,
		"nexus":          nexusMeta,
		"action_binding": binding,
	}

	ctxJSON := map[string]any{
		"task_title": t.Title,
		"task_goal":  t.Goal,
		"note":       in.Note,
	}
	ctxStr := marshalOrEmpty("propose_context", ctxJSON)

	reason := t.Title
	if in.Note != "" {
		reason = t.Title + ": " + in.Note
	}

	idem := fmt.Sprintf("companion-propose-%s", action.ID.String())
	submitBody := governanceclient.SubmitRequestBody{
		RequesterType:  CompanionRequesterType,
		RequesterID:    CompanionRequesterID,
		RequesterName:  CompanionRequesterName,
		ActionType:     ActionTypePropose,
		TargetSystem:   stringFromBinding(binding, "target_system", in.TargetSystem),
		TargetResource: stringFromBinding(binding, "target_resource", in.TargetResource),
		Params:         params,
		Reason:         reason,
		Context:        string(ctxStr),
	}

	submitOut, subErr := u.governance.SubmitRequest(ctx, idem, submitBody)
	if subErr != nil {
		slog.Warn("companion propose governance submit failed",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"error", subErr,
		)
		_ = u.repo.UpdateActionGovernanceResult(ctx, action.ID, nil, subErr.Error())
		t2, ge := u.repo.GetTaskByID(ctx, taskID)
		if ge != nil {
			return domain.Task{}, action, zeroSub, ge
		}
		return t2, action, zeroSub, fmt.Errorf("%w: %v", ErrGovernanceSubmit, subErr)
	}
	reqUUID, perr := uuid.Parse(submitOut.RequestID)
	if perr != nil {
		_ = u.repo.UpdateActionGovernanceResult(ctx, action.ID, nil, "invalid request_id from governance")
		return domain.Task{}, action, zeroSub, fmt.Errorf("parse request_id: %w", perr)
	}
	if err := u.repo.UpdateActionGovernanceResult(ctx, action.ID, &reqUUID, ""); err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	now := time.Now().UTC()
	state, err := u.repo.UpsertGovernanceSyncState(ctx, domain.TaskGovernanceSyncState{
		TaskID:                   taskID,
		GovernanceRequestID:      reqUUID,
		LastGovernanceStatus:     normalizeGovernanceStatus(submitOut.Status),
		LastGovernanceHTTPStatus: http.StatusCreated,
		LastCheckedAt:            now,
		LastError:                "",
		ConsecutiveFailures:      0,
		NextCheckAt:              nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), 0),
	})
	if err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	ev, evErr := eventFromSubmitResponseWithExecutionPlan(submitOut, true)
	if evErr != nil {
		slog.Error("companion propose unexpected governance status",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"governance_status", submitOut.Status,
			"error", evErr,
		)
		return domain.Task{}, action, submitOut, evErr
	}
	t, err = u.applyTaskEvent(ctx, t, ev)
	if err != nil {
		return domain.Task{}, action, submitOut, err
	}
	t.GovernanceStatus = state.LastGovernanceStatus
	t.GovernanceLastCheckedAt = &state.LastCheckedAt
	t.GovernanceSyncError = state.LastError
	action.GovernanceRequestID = &reqUUID
	slog.Info("companion propose submitted to governance",
		"task_id", taskID.String(),
		"action_id", action.ID.String(),
		"governance_request_id", reqUUID.String(),
		"governance_decision", submitOut.Decision,
		"governance_status", submitOut.Status,
		"task_status", t.Status,
	)
	u.syncTaskMemory(ctx, taskID, "propose")
	return t, action, submitOut, nil
}

type SetExecutionPlanInput struct {
	ConnectorID    uuid.UUID
	Operation      string
	Payload        json.RawMessage
	IdempotencyKey string
}

func (u *Usecases) SetExecutionPlan(ctx context.Context, taskID uuid.UUID, in SetExecutionPlanInput) (domain.TaskExecutionPlan, error) {
	if in.ConnectorID == uuid.Nil {
		return domain.TaskExecutionPlan{}, fmt.Errorf("connector_id is required")
	}
	if in.Operation == "" {
		return domain.TaskExecutionPlan{}, fmt.Errorf("operation is required")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}
	switch t.Status {
	case domain.TaskStatusDone, domain.TaskStatusFailed, domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return domain.TaskExecutionPlan{}, ErrInvalidTaskState
	}

	if u.executor != nil {
		if _, err := u.executor.GetConnector(ctx, in.ConnectorID); err != nil {
			return domain.TaskExecutionPlan{}, fmt.Errorf("get connector: %w", err)
		}
	}

	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage(`{}`)
	}

	var prevPlan *domain.TaskExecutionPlan
	currentPlan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		currentCopy := currentPlan
		prevPlan = &currentCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.TaskExecutionPlan{}, err
	}

	plan, err := u.repo.UpsertExecutionPlan(ctx, domain.TaskExecutionPlan{
		TaskID:         taskID,
		ConnectorID:    in.ConnectorID,
		Operation:      in.Operation,
		Payload:        in.Payload,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}

	if executionPlanChanged(prevPlan, plan) {
		payload := marshalOrEmpty("execution_plan_action", map[string]any{
			"connector_id":    plan.ConnectorID.String(),
			"operation":       plan.Operation,
			"payload":         json.RawMessage(plan.Payload),
			"idempotency_key": plan.IdempotencyKey,
		})
		if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
			TaskID:     taskID,
			ActionType: TaskActionSetExecutionPlan,
			Payload:    payload,
		}); insertErr != nil {
			slog.Warn("companion set execution plan action failed", "task_id", taskID.String(), "error", insertErr)
		}
	}
	u.syncTaskMemory(ctx, taskID, "set_execution_plan")

	return plan, nil
}

type ExecuteTaskOutput struct {
	Task           domain.Task
	Plan           domain.TaskExecutionPlan
	Execution      connectordomain.ExecutionResult
	ExecutionState domain.TaskExecutionState
}

func buildConnectorExecutionPayload(result connectordomain.ExecutionResult) json.RawMessage {
	return marshalOrEmpty("connector_execution_payload", map[string]any{
		"id":              result.ID.String(),
		"connector_id":    result.ConnectorID.String(),
		"org_id":          result.OrgID,
		"actor_id":        result.ActorID,
		"operation":       result.Operation,
		"status":          result.Status,
		"external_ref":    result.ExternalRef,
		"payload":         json.RawMessage(result.Payload),
		"result":          json.RawMessage(result.ResultJSON),
		"evidence":        json.RawMessage(result.EvidenceJSON),
		"error_message":   result.ErrorMessage,
		"retryable":       result.Retryable,
		"duration_ms":     result.DurationMS,
		"idempotency_key": result.IdempotencyKey,
		"governance_request_id": func() string {
			if result.GovernanceRequestID != nil {
				return result.GovernanceRequestID.String()
			}
			return ""
		}(),
	})
}

func buildVerificationPayload(result connectordomain.ExecutionResult, verification domain.TaskVerificationResult) json.RawMessage {
	return marshalOrEmpty("verification_payload", map[string]any{
		"execution_id":        result.ID.String(),
		"execution_status":    result.Status,
		"verification_status": verification.Status,
		"summary":             verification.Summary,
		"checked_at":          verification.CheckedAt,
		"details":             json.RawMessage(verification.Details),
		"retryable":           result.Retryable,
	})
}

func hasResultPayload(result json.RawMessage) bool {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 {
		return false
	}
	switch string(trimmed) {
	case "{}", "null", "[]":
		return false
	default:
		return true
	}
}

func hasVerificationEvidence(result connectordomain.ExecutionResult) bool {
	if strings.TrimSpace(result.ExternalRef) != "" {
		return true
	}
	return hasResultPayload(result.ResultJSON)
}

func verifyExecutionResult(result connectordomain.ExecutionResult) domain.TaskVerificationResult {
	checkedAt := time.Now().UTC()
	details := marshalOrEmpty("verification_details", map[string]any{
		"execution_status":       result.Status,
		"external_ref_present":   strings.TrimSpace(result.ExternalRef) != "",
		"result_payload_present": hasResultPayload(result.ResultJSON),
		"retryable":              result.Retryable,
		"error_message":          result.ErrorMessage,
	})

	switch result.Status {
	case connectordomain.ExecSuccess:
		if hasVerificationEvidence(result) {
			return domain.TaskVerificationResult{
				Status:    domain.VerificationStatusVerified,
				Summary:   "connector execution verified from returned evidence",
				CheckedAt: checkedAt,
				Details:   details,
			}
		}
		return domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   "verification failed: connector returned no evidence",
			CheckedAt: checkedAt,
			Details:   details,
		}
	default:
		summary := "execution failed before verification"
		if result.ErrorMessage != "" {
			summary = result.ErrorMessage
		}
		return domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   summary,
			CheckedAt: checkedAt,
			Details:   details,
		}
	}
}

func buildExecutionState(prev *domain.TaskExecutionState, taskID uuid.UUID, result connectordomain.ExecutionResult, verification domain.TaskVerificationResult, isRetry bool) domain.TaskExecutionState {
	retryCount := 0
	createdAt := time.Now().UTC()
	if prev != nil {
		retryCount = prev.RetryCount
		createdAt = prev.CreatedAt
	}
	if isRetry {
		retryCount++
	}
	lastError := result.ErrorMessage
	if lastError == "" && verification.Status == domain.VerificationStatusFailed {
		lastError = verification.Summary
	}
	retryable := result.Retryable
	if verification.Status == domain.VerificationStatusFailed {
		retryable = true
	}
	if verification.Status == domain.VerificationStatusVerified {
		retryable = false
		lastError = ""
	}
	return domain.TaskExecutionState{
		TaskID:              taskID,
		LastExecutionID:     result.ID,
		LastExecutionStatus: result.Status,
		Retryable:           retryable,
		RetryCount:          retryCount,
		LastError:           lastError,
		LastAttemptedAt:     result.CreatedAt,
		VerificationResult:  verification,
		CreatedAt:           createdAt,
	}
}

func defaultExecutionIdempotencyKey(taskID uuid.UUID, governanceRequestID *uuid.UUID) string {
	return fmt.Sprintf("task-execute-%s", taskID.String())
}

func stringFromBinding(binding map[string]any, key, defaultValue string) string {
	if value := strings.TrimSpace(fmt.Sprint(binding[key])); value != "" && value != "<nil>" {
		return value
	}
	return defaultValue
}

func executionActorID(t domain.Task) string {
	if actor := strings.TrimSpace(t.AssignedTo); actor != "" {
		return actor
	}
	if actor := strings.TrimSpace(t.CreatedBy); actor != "" {
		return actor
	}
	return CompanionRequesterID
}

func (u *Usecases) refreshGovernanceSnapshot(ctx context.Context, taskID uuid.UUID, origin string) (*domain.TaskGovernanceSyncState, error) {
	var prevState *domain.TaskGovernanceSyncState
	currentState, err := u.repo.GetGovernanceSyncState(ctx, taskID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return nil, err
	}

	governanceRequestID, err := u.latestGovernanceRequestIDForTask(ctx, taskID, prevState)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskGovernanceSyncState{
		TaskID:              taskID,
		GovernanceRequestID: governanceRequestID,
		LastCheckedAt:       now,
		NextCheckAt:         nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastGovernanceStatus = prevState.LastGovernanceStatus
		nextState.LastGovernanceHTTPStatus = prevState.LastGovernanceHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, statusCode, getErr := u.governance.GetRequest(ctx, governanceRequestID.String())
	if getErr != nil {
		nextState.LastGovernanceHTTPStatus = statusCode
		nextState.LastError = getErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upsertErr := u.repo.UpsertGovernanceSyncState(ctx, nextState)
		if upsertErr != nil {
			return nil, upsertErr
		}
		return &stateOut, fmt.Errorf("governance get request: %w", getErr)
	}

	nextState.LastGovernanceHTTPStatus = statusCode
	nextState.LastGovernanceStatus = normalizeGovernanceStatus(sum.Status)
	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextGovernanceSyncAt(now, u.governanceSyncIntervalOrDefault(), 0)

	stateOut, upsertErr := u.repo.UpsertGovernanceSyncState(ctx, nextState)
	if upsertErr != nil {
		return nil, upsertErr
	}
	if governanceSnapshotChanged(prevState, stateOut) {
		u.persistGovernanceSyncAction(ctx, taskID, governanceRequestID, origin, prevState, stateOut, "", "", "")
	}
	return &stateOut, nil
}

func (u *Usecases) runTaskExecution(ctx context.Context, t domain.Task, plan domain.TaskExecutionPlan, prevState *domain.TaskExecutionState, startEvent string) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput

	t, err := u.applyTaskEvent(ctx, t, startEvent)
	if err != nil {
		return out, err
	}

	var governanceRequestID *uuid.UUID
	if syncState, syncErr := u.repo.GetGovernanceSyncState(ctx, t.ID); syncErr == nil && syncState.GovernanceRequestID != uuid.Nil {
		governanceRequestID = &syncState.GovernanceRequestID
	}
	idempotencyKey := plan.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = defaultExecutionIdempotencyKey(t.ID, governanceRequestID)
	}

	result, execErr := u.executor.Execute(ctx, connectordomain.ExecutionSpec{
		ConnectorID:         plan.ConnectorID,
		OrgID:               t.OrgID,
		ActorID:             executionActorID(t),
		ProductSurface:      "companion",
		Operation:           plan.Operation,
		Payload:             plan.Payload,
		IdempotencyKey:      idempotencyKey,
		TaskID:              &t.ID,
		GovernanceRequestID: governanceRequestID,
	})
	if execErr != nil {
		result = connectordomain.ExecutionResult{
			ID:                  uuid.New(),
			ConnectorID:         plan.ConnectorID,
			OrgID:               t.OrgID,
			ActorID:             executionActorID(t),
			Operation:           plan.Operation,
			Status:              connectordomain.ExecFailure,
			Payload:             plan.Payload,
			ResultJSON:          json.RawMessage(`{}`),
			ErrorMessage:        execErr.Error(),
			Retryable:           true,
			IdempotencyKey:      idempotencyKey,
			TaskID:              &t.ID,
			GovernanceRequestID: governanceRequestID,
			CreatedAt:           time.Now().UTC(),
		}
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	u.reportExecutionToGovernance(ctx, governanceRequestID, result)

	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:              t.ID,
		ActionType:          TaskActionExecuteConnector,
		Payload:             buildConnectorExecutionPayload(result),
		GovernanceRequestID: governanceRequestID,
		ErrorMessage:        result.ErrorMessage,
	}); insertErr != nil {
		slog.Warn("companion execute connector action failed", "task_id", t.ID.String(), "error", insertErr)
	}

	artifactKind := TaskArtifactConnectorExecution
	if result.Status != connectordomain.ExecSuccess {
		artifactKind = TaskArtifactExecutionError
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  t.ID,
		Kind:    artifactKind,
		URI:     result.ExternalRef,
		Payload: buildConnectorExecutionPayload(result),
	}); artifactErr != nil {
		slog.Warn("companion execute connector artifact failed", "task_id", t.ID.String(), "error", artifactErr)
	}

	verification := verifyExecutionResult(result)
	if _, verifyErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:              t.ID,
		ActionType:          TaskActionVerifyExecution,
		Payload:             buildVerificationPayload(result, verification),
		GovernanceRequestID: governanceRequestID,
		ErrorMessage: func() string {
			if verification.Status == domain.VerificationStatusFailed {
				return verification.Summary
			}
			return ""
		}(),
	}); verifyErr != nil {
		slog.Warn("companion verify execution action failed", "task_id", t.ID.String(), "error", verifyErr)
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  t.ID,
		Kind:    TaskArtifactExecutionVerification,
		URI:     result.ExternalRef,
		Payload: buildVerificationPayload(result, verification),
	}); artifactErr != nil {
		slog.Warn("companion verify execution artifact failed", "task_id", t.ID.String(), "error", artifactErr)
	}

	executionState, stateErr := u.repo.UpsertExecutionState(ctx, buildExecutionState(prevState, t.ID, result, verification, startEvent == evRetryExecution))
	if stateErr != nil {
		return out, stateErr
	}

	switch {
	case result.Status == connectordomain.ExecSuccess && verification.Status == domain.VerificationStatusVerified:
		t, err = u.applyTaskEvent(ctx, t, evExecutionSucceeded)
		if err != nil {
			return out, err
		}
		t, err = u.applyTaskEvent(ctx, t, evExecutionVerified)
		if err != nil {
			return out, err
		}
	case result.Status == connectordomain.ExecSuccess && verification.Status == domain.VerificationStatusFailed:
		t, err = u.applyTaskEvent(ctx, t, evExecutionSucceeded)
		if err != nil {
			return out, err
		}
		t, err = u.applyTaskEvent(ctx, t, evExecutionFailed)
		if err != nil {
			return out, err
		}
	default:
		t, err = u.applyTaskEvent(ctx, t, evExecutionFailed)
		if err != nil {
			return out, err
		}
	}

	t.GovernanceStatus = normalizeGovernanceStatus(t.GovernanceStatus)
	out.Task = t
	out.Plan = plan
	out.Execution = result
	out.ExecutionState = executionState
	u.syncTaskMemory(ctx, t.ID, "execution")
	return out, nil
}

func (u *Usecases) reportExecutionToGovernance(ctx context.Context, governanceRequestID *uuid.UUID, result connectordomain.ExecutionResult) {
	if u.governance == nil || governanceRequestID == nil || *governanceRequestID == uuid.Nil {
		return
	}
	success := result.Status == connectordomain.ExecSuccess
	var resultPayload map[string]any
	if len(result.ResultJSON) > 0 {
		if err := json.Unmarshal(result.ResultJSON, &resultPayload); err != nil {
			resultPayload = map[string]any{"raw": string(result.ResultJSON)}
		}
	}
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	resultPayload["connector_execution_id"] = result.ID.String()
	resultPayload["connector_id"] = result.ConnectorID.String()
	resultPayload["operation"] = result.Operation
	resultPayload["external_ref"] = result.ExternalRef
	resultPayload["org_id"] = result.OrgID
	resultPayload["actor_id"] = result.ActorID
	if len(result.EvidenceJSON) > 0 {
		resultPayload["evidence"] = json.RawMessage(result.EvidenceJSON)
	}
	status, err := u.governance.ReportResult(ctx, governanceRequestID.String(), success, resultPayload, result.DurationMS, result.ErrorMessage)
	if err != nil || status >= http.StatusBadRequest {
		slog.Warn("report execution to governance failed",
			"governance_request_id", governanceRequestID.String(),
			"status", status,
			"error", err)
	}
}

func (u *Usecases) ExecuteTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, fmt.Errorf("execution plan is required")
		}
		return out, err
	}

	var governanceRequestID string
	if t.Status == domain.TaskStatusWaitingForApproval {
		syncedTask, state, syncErr := u.syncTaskWithGovernance(ctx, t, "execute")
		if state != nil {
			syncedTask.GovernanceStatus = state.LastGovernanceStatus
			syncedTask.GovernanceLastCheckedAt = &state.LastCheckedAt
			syncedTask.GovernanceSyncError = state.LastError
			governanceRequestID = state.GovernanceRequestID.String()
		}
		if syncErr != nil {
			return out, syncErr
		}
		t = syncedTask
	}

	if !isApprovedGovernanceStatus(t.GovernanceStatus) {
		return out, u.governanceBlockedError(governanceRequestID, t.GovernanceStatus, "execute")
	}
	if t.Status != domain.TaskStatusWaitingForInput {
		return out, ErrInvalidTaskState
	}

	prevState, stateErr := u.getExecutionState(ctx, taskID)
	if stateErr != nil {
		return out, stateErr
	}
	return u.runTaskExecution(ctx, t, plan, prevState, evStartExecution)
}

func (u *Usecases) RetryTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, fmt.Errorf("execution plan is required")
		}
		return out, err
	}
	state, err := u.repo.GetExecutionState(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, ErrInvalidTaskState
		}
		return out, err
	}
	if t.Status != domain.TaskStatusFailed || !state.Retryable {
		return out, ErrInvalidTaskState
	}

	snapshot, snapshotErr := u.refreshGovernanceSnapshot(ctx, taskID, "retry")
	if snapshotErr != nil {
		return out, snapshotErr
	}
	t.GovernanceStatus = snapshot.LastGovernanceStatus
	t.GovernanceLastCheckedAt = &snapshot.LastCheckedAt
	t.GovernanceSyncError = snapshot.LastError
	if !isApprovedGovernanceStatus(snapshot.LastGovernanceStatus) {
		return out, u.governanceBlockedError(snapshot.GovernanceRequestID.String(), snapshot.LastGovernanceStatus, "retry")
	}

	payload := marshalOrEmpty("retry_execution_action", map[string]any{
		"retry_count_before":    state.RetryCount,
		"last_execution_status": state.LastExecutionStatus,
		"last_error":            state.LastError,
	})
	governanceRequestID := snapshot.GovernanceRequestID
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:              taskID,
		ActionType:          TaskActionRetryExecution,
		Payload:             payload,
		GovernanceRequestID: &governanceRequestID,
	}); insertErr != nil {
		slog.Warn("companion retry execution action failed", "task_id", taskID.String(), "error", insertErr)
	}

	return u.runTaskExecution(ctx, t, plan, &state, evRetryExecution)
}

// SyncTaskGovernance consulta Governance y aplica transición si el request ya resolvió (tareas en espera).
func (u *Usecases) SyncTaskGovernance(ctx context.Context, taskID uuid.UUID) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, state, err := u.syncTaskWithGovernance(ctx, t, "manual")
	if state != nil {
		t.GovernanceStatus = state.LastGovernanceStatus
		t.GovernanceLastCheckedAt = &state.LastCheckedAt
		t.GovernanceSyncError = state.LastError
	}
	if err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

// SyncPendingGovernanceTasks sincroniza un lote de tareas en waiting_for_approval.
func (u *Usecases) SyncPendingGovernanceTasks(ctx context.Context, limit int) {
	if limit <= 0 {
		limit = 50
	}
	list, err := u.repo.ListTasksPendingGovernanceSync(ctx, time.Now().UTC(), limit)
	if err != nil {
		slog.Error("companion sync list waiting tasks", "error", err)
		return
	}
	for _, item := range list {
		if _, _, sErr := u.syncTaskWithGovernance(ctx, item, "loop"); sErr != nil {
			slog.Warn("companion sync task failed", "task_id", item.ID.String(), "error", sErr)
		}
	}
}

// RunGovernanceSyncLoop ejecuta SyncPendingGovernanceTasks periódicamente hasta que ctx termina.
func (u *Usecases) RunGovernanceSyncLoop(ctx context.Context, interval time.Duration, batch int) {
	if batch <= 0 {
		return
	}
	worker.RunPeriodic(ctx, interval, "governance-sync", func(c context.Context) {
		runCtx, cancel := context.WithTimeout(c, 2*time.Minute)
		u.SyncPendingGovernanceTasks(runCtx, batch)
		cancel()
	})
}

// ErrInvalidStatus para handlers.
func IsNotFound(err error) bool {
	return domainerr.IsNotFound(err)
}

// IsInvalidTaskState indica conflicto de estado (FSM / reglas de negocio).
func IsInvalidTaskState(err error) bool {
	return errors.Is(err, ErrInvalidTaskState)
}

// governanceBlockedError devuelve un error estructurado cuando una operación de
// task se bloquea porque la governance en Nexus no está aprobada.
func (u *Usecases) governanceBlockedError(governanceRequestID, governanceStatus, reason string) error {
	return &GovernanceBlockedError{
		GovernanceRequestID: governanceRequestID,
		GovernanceStatus:    governanceStatus,
		Reason:              reason,
	}
}

// NotifyAlert implementa watchers.ChatNotifier.
// Crea una tarea-alerta y agrega el mensaje como sistema.
func (u *Usecases) NotifyAlert(ctx context.Context, orgID, message string) error {
	title := message
	if len(title) > 80 {
		title = title[:80]
	}
	t, err := u.repo.CreateTask(ctx, domain.Task{
		Title:     title,
		OrgID:     orgID,
		Status:    domain.TaskStatusNew,
		Priority:  "high",
		CreatedBy: orgID,
		Channel:   "watcher",
	})
	if err != nil {
		return fmt.Errorf("create alert task: %w", err)
	}
	_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     t.ID,
		AuthorType: "system",
		AuthorID:   "nexus-watcher",
		Body:       message,
	})
	if err != nil {
		return fmt.Errorf("insert alert message: %w", err)
	}
	slog.Info("watcher alert pushed to chat", "task_id", t.ID, "org_id", orgID)
	return nil
}
