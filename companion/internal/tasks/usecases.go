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
	"github.com/devpablocristo/platform/security/go/tenant"
	"github.com/google/uuid"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// Identidad del servicio Companion ante Nexus (documentado en README).
const (
	CompanionRequesterType     = "service"
	CompanionRequesterID       = identityctx.CompanionPrincipal
	CompanionRequesterName     = "Companion Employee AI"
	ActionTypePropose          = "companion.propose"
	TaskActionInvestigate      = "investigate"
	TaskActionPropose          = "propose"
	TaskActionSyncNexus        = "sync_nexus"
	TaskActionSetExecutionPlan = "set_execution_plan"
	TaskActionExecuteConnector = "execute_connector"
	TaskActionRetryExecution   = "retry_execution"
	TaskActionVerifyExecution  = "verify_execution"
	TaskActionSetDurablePlan   = "set_durable_plan"
	TaskActionUpdatePlanStep   = "update_plan_step"
	TaskActionPlanCheckpoint   = "plan_checkpoint"
	TaskActionPrepareComp      = "prepare_compensation"
	TaskActionExecuteComp      = "execute_compensation"

	TaskArtifactConnectorExecution    = "connector_execution"
	TaskArtifactExecutionError        = "connector_execution_error"
	TaskArtifactExecutionVerification = "execution_verification"

	taskMemoryCurrentKey  = "current"
	taskMemoryKindFacts   = "task_facts"
	taskMemoryKindSummary = "task_summary"

	defaultNexusSyncInterval = 30 * time.Second
	maxNexusSyncBackoff      = 10 * time.Minute
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

type nexusGateway interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error)
	GetRequest(ctx context.Context, id string) (nexusclient.RequestSummary, int, error)
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
	Identity       identityctx.IdentityContext
	Message        string
	RouteHint      string
	Handoff        json.RawMessage
	Messages       []domain.TaskMessage
	TaskID         *uuid.UUID // opcional: vincula el trace a una task
	ProductSurface string     // opcional: "companion" (default) | "ponti" | "pymes" — afecta routing
	AgentID        string     // opcional: empleado IA persistente que toma ownership de la ejecución
}

type OrchestratorToolCall struct {
	Name           string          `json:"name"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	Allowed        bool            `json:"allowed"`
	DecisionReason string          `json:"decision_reason,omitempty"`
	DurationMS     int64           `json:"duration_ms"`
	Error          string          `json:"error,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
}

// OrchestratorResult resultado del runtime.
type OrchestratorResult struct {
	Reply     string
	RunID     string
	AgentID   string
	ToolCalls []OrchestratorToolCall
}

// Usecases lógica de tareas e integración con Nexus.
type Usecases struct {
	repo              Repository
	nexus             nexusGateway
	orchestrator      ChatOrchestrator // opcional en tests; productivo usa Gemini
	executor          taskExecutor
	taskMemory        taskMemoryWriter
	agentMemory       agentMemoryWriter
	nexusSyncInterval time.Duration
}

func NewUsecases(repo Repository, nexus nexusGateway) *Usecases {
	return &Usecases{
		repo:              repo,
		nexus:             nexus,
		nexusSyncInterval: defaultNexusSyncInterval,
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

func (u *Usecases) SetNexusSyncInterval(interval time.Duration) {
	if interval <= 0 {
		u.nexusSyncInterval = defaultNexusSyncInterval
		return
	}
	u.nexusSyncInterval = interval
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

// List devuelve tareas para un tenant. `orgID` obligatorio; vacío retorna
// `domainerr.TenantMissing` (el repo enforce esta semántica también).
func (u *Usecases) List(ctx context.Context, orgID tenant.ID, limit int) ([]domain.Task, error) {
	return u.repo.ListTasks(ctx, orgID, limit)
}

// ListAll devuelve tareas SIN filtro de tenant. SOLO callable después de
// validar `companion:cross_org` (o dev mode sin auth). El usecase no valida
// scopes; eso vive en el handler.
func (u *Usecases) ListAll(ctx context.Context, limit int) ([]domain.Task, error) {
	return u.repo.ListAllTasks(ctx, limit)
}

func (u *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	return u.repo.GetTaskByID(ctx, id)
}

type LinkedNexusRequest struct {
	ActionID uuid.UUID                   `json:"action_id"`
	Request  *nexusclient.RequestSummary `json:"request,omitempty"`
}

type TaskDetail struct {
	Task                domain.Task                `json:"task"`
	Messages            []domain.TaskMessage       `json:"messages"`
	Actions             []domain.TaskAction        `json:"actions"`
	Artifacts           []domain.TaskArtifact      `json:"artifacts"`
	LinkedNexusRequests []LinkedNexusRequest       `json:"linked_nexus_requests"`
	NexusSync           *domain.TaskNexusSyncState `json:"nexus_sync,omitempty"`
	ExecutionPlan       *domain.TaskExecutionPlan  `json:"execution_plan,omitempty"`
	DurablePlan         *domain.TaskPlan           `json:"durable_plan,omitempty"`
	ExecutionState      *domain.TaskExecutionState `json:"execution_state,omitempty"`
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
	state, stateErr := u.repo.GetNexusSyncState(ctx, id)
	if stateErr == nil {
		out.NexusSync = &state
	} else if !domainerr.IsNotFound(stateErr) {
		return out, stateErr
	}
	plan, planErr := u.repo.GetExecutionPlan(ctx, id)
	if planErr == nil {
		out.ExecutionPlan = &plan
	} else if !domainerr.IsNotFound(planErr) {
		return out, planErr
	}
	durablePlan, durablePlanErr := u.repo.GetTaskPlan(ctx, id)
	if durablePlanErr == nil {
		out.DurablePlan = &durablePlan
	} else if !domainerr.IsNotFound(durablePlanErr) {
		return out, durablePlanErr
	}
	executionState, executionStateErr := u.repo.GetExecutionState(ctx, id)
	if executionStateErr == nil {
		out.ExecutionState = &executionState
	} else if !domainerr.IsNotFound(executionStateErr) {
		return out, executionStateErr
	}
	seen := make(map[uuid.UUID]struct{})
	for _, a := range out.Actions {
		if a.NexusRequestID == nil {
			continue
		}
		rid := *a.NexusRequestID
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		sum, st, gErr := u.nexus.GetRequest(ctx, rid.String())
		lr := LinkedNexusRequest{ActionID: a.ID}
		if gErr != nil {
			slog.Error("nexus get request failed", "error", gErr, "request_id", rid)
			out.LinkedNexusRequests = append(out.LinkedNexusRequests, lr)
			continue
		}
		if st == 404 {
			out.LinkedNexusRequests = append(out.LinkedNexusRequests, lr)
			continue
		}
		lr.Request = &sum
		out.LinkedNexusRequests = append(out.LinkedNexusRequests, lr)
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
	ChatID         *uuid.UUID // nil = resolver por task_id o crear task nueva
	UserID         string
	OrgID          string
	AuthScopes     []string
	Message        string
	Channel        string // "api", "watcher", "whatsapp", product-specific channels, etc.
	ProductSurface string // opcional: "companion" | "ponti" | "pymes". Afecta routing del agent.
	AgentID        string // opcional: empleado IA persistente para esta task/conversación.
	RouteHint      string // opcional: pista de pantalla/módulo para ruteo operativo.
	Handoff        json.RawMessage
	Identity       identityctx.IdentityContext
}

// ChatResult resultado del chat.
type ChatResult struct {
	Task      domain.Task
	Messages  []domain.TaskMessage
	RunID     string
	AgentID   string
	ToolCalls []OrchestratorToolCall
}

// agentConversationContextKey nombre del field en task.context_json que guarda
// el agent_conversations.id asociado a la task. Permite reusar la misma
// conversation_id en mensajes sucesivos del mismo task.
const agentConversationContextKey = "agent_conversation_id"
const agentContextKey = "agent_id"

// Chat combina crear/reusar tarea + agregar mensaje del usuario.
// Es el endpoint principal para la interfaz conversacional del suscriptor.
func (u *Usecases) Chat(ctx context.Context, in ChatInput) (ChatResult, error) {
	if in.Message == "" {
		return ChatResult{}, fmt.Errorf("message is required")
	}
	in.Identity = chatIdentity(in)
	in.UserID = in.Identity.EffectiveActorID()
	in.OrgID = in.Identity.CustomerOrgID
	in.AuthScopes = append([]string(nil), in.Identity.Scopes...)
	in.ProductSurface = in.Identity.ProductSurface
	in.AgentID = strings.TrimSpace(in.AgentID)

	var t domain.Task
	var err error
	newTask := false

	if in.TaskID != nil {
		// Reusar tarea existente
		t, err = u.repo.GetTaskByID(ctx, *in.TaskID)
		if err != nil {
			return ChatResult{}, err
		}
		if in.OrgID != "" && t.OrgID != "" && t.OrgID != in.OrgID {
			return ChatResult{}, ErrNotFound
		}
		if in.AgentID == "" {
			in.AgentID = extractTaskAgentID(t.ContextJSON)
		}
	} else if in.ChatID != nil {
		// Reusar tarea existente a partir del identificador público de conversación.
		t, err = u.repo.GetTaskByAgentConversationID(ctx, *in.ChatID)
		if err != nil {
			return ChatResult{}, err
		}
		if in.OrgID != "" && t.OrgID != "" && t.OrgID != in.OrgID {
			return ChatResult{}, ErrNotFound
		}
		if in.AgentID == "" {
			in.AgentID = extractTaskAgentID(t.ContextJSON)
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
		contextJSON := json.RawMessage(`{}`)
		if in.AgentID != "" {
			if updated, ok := mergeTaskAgentID(contextJSON, in.AgentID); ok {
				contextJSON = updated
			}
		}
		t, err = u.repo.CreateTask(ctx, domain.Task{
			Title:       title,
			OrgID:       in.OrgID,
			Status:      domain.TaskStatusNew,
			Priority:    "normal",
			CreatedBy:   in.UserID,
			Channel:     channel,
			ContextJSON: contextJSON,
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
	u.ensureTaskAgent(ctx, &t, in.AgentID)

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
	runID := ""
	toolCalls := []OrchestratorToolCall{}
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
				Identity:       in.Identity,
				Message:        in.Message,
				RouteHint:      in.RouteHint,
				Handoff:        in.Handoff,
				Messages:       existingMsgs,
				TaskID:         &taskID,
				ProductSurface: in.ProductSurface,
				AgentID:        in.AgentID,
			})
			if runErr != nil {
				slog.Error("orchestrator failed", "error", runErr)
				return ChatResult{}, fmt.Errorf("run companion runtime: %w", runErr)
			} else {
				in.AgentID = result.AgentID
				runID = result.RunID
				toolCalls = result.ToolCalls
				u.ensureTaskAgent(ctx, &t, in.AgentID)
			}
			if runErr == nil && result.Reply != "" {
				// Guardar respuesta del compañero como mensaje del sistema
				_, insertErr := u.repo.InsertMessage(ctx, domain.TaskMessage{
					TaskID:     t.ID,
					AuthorType: "system",
					AuthorID:   in.Identity.CompanionPrincipal,
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

	return ChatResult{Task: t, Messages: msgs, RunID: runID, AgentID: in.AgentID, ToolCalls: toolCalls}, nil
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

func (u *Usecases) ensureTaskAgent(ctx context.Context, t *domain.Task, agentID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || extractTaskAgentID(t.ContextJSON) == agentID {
		return
	}
	if updated, ok := mergeTaskAgentID(t.ContextJSON, agentID); ok {
		t.ContextJSON = updated
		if _, err := u.repo.UpdateTask(ctx, *t); err != nil {
			slog.Error("update task context with agent_id", "error", err, "task_id", t.ID)
		}
	}
}

func (u *Usecases) persistAgentMessage(ctx context.Context, convID uuid.UUID, orgID, role, content string) {
	if u.agentMemory == nil || convID == uuid.Nil || content == "" {
		return
	}
	if err := u.agentMemory.AppendMessage(ctx, convID, orgID, role, content); err != nil {
		slog.Error("agent memory append message", "error", err, "conversation_id", convID, "role", role)
	}
}

func chatIdentity(in ChatInput) identityctx.IdentityContext {
	id := in.Identity
	if id.CustomerOrgID == "" {
		id.CustomerOrgID = in.OrgID
	}
	if id.HumanUserID == "" {
		id.HumanUserID = in.UserID
	}
	if len(id.Scopes) == 0 {
		id.Scopes = append([]string(nil), in.AuthScopes...)
	}
	if id.ProductSurface == "" {
		id.ProductSurface = in.ProductSurface
	}
	return id.WithProductSurface(id.ProductSurface)
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

func extractTaskAgentID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var holder map[string]any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return ""
	}
	value, _ := holder[agentContextKey].(string)
	return strings.TrimSpace(value)
}

func mergeTaskAgentID(raw json.RawMessage, agentID string) (json.RawMessage, bool) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return raw, false
	}
	holder := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &holder); err != nil {
			holder = map[string]any{}
		}
	}
	holder[agentContextKey] = agentID
	out, err := json.Marshal(holder)
	if err != nil {
		return nil, false
	}
	return out, true
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

func (u *Usecases) nexusSyncIntervalOrDefault() time.Duration {
	if u.nexusSyncInterval <= 0 {
		return defaultNexusSyncInterval
	}
	return u.nexusSyncInterval
}

func nextNexusSyncAt(now time.Time, interval time.Duration, consecutiveFailures int) time.Time {
	if interval <= 0 {
		interval = defaultNexusSyncInterval
	}
	if consecutiveFailures <= 0 {
		return now.Add(interval)
	}
	delay := interval
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxNexusSyncBackoff/2 {
			delay = maxNexusSyncBackoff
			break
		}
		delay *= 2
	}
	if delay > maxNexusSyncBackoff {
		delay = maxNexusSyncBackoff
	}
	return now.Add(delay)
}

func nexusSnapshotChanged(prev *domain.TaskNexusSyncState, next domain.TaskNexusSyncState) bool {
	if prev == nil {
		return next.NexusRequestID != uuid.Nil ||
			next.LastNexusStatus != "" ||
			next.LastNexusHTTPStatus != 0 ||
			next.LastError != ""
	}
	return prev.NexusRequestID != next.NexusRequestID ||
		prev.LastNexusStatus != next.LastNexusStatus ||
		prev.LastNexusHTTPStatus != next.LastNexusHTTPStatus ||
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

func isApprovedNexusStatus(status string) bool {
	switch normalizeNexusStatus(status) {
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
	NexusSync      *domain.TaskNexusSyncState
	ExecutionPlan  *domain.TaskExecutionPlan
	DurablePlan    *domain.TaskPlan
	ExecutionState *domain.TaskExecutionState
}

func (u *Usecases) loadTaskMemorySnapshot(ctx context.Context, taskID uuid.UUID) (taskMemorySnapshot, error) {
	task, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return taskMemorySnapshot{}, err
	}
	snapshot := taskMemorySnapshot{Task: task}

	nexusSync, err := u.repo.GetNexusSyncState(ctx, taskID)
	if err == nil {
		snapshot.NexusSync = &nexusSync
		snapshot.Task.NexusStatus = nexusSync.LastNexusStatus
		snapshot.Task.NexusLastCheckedAt = &nexusSync.LastCheckedAt
		snapshot.Task.NexusSyncError = nexusSync.LastError
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	executionPlan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		snapshot.ExecutionPlan = &executionPlan
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	durablePlan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err == nil {
		snapshot.DurablePlan = &durablePlan
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
	if snapshot.DurablePlan != nil && strings.TrimSpace(snapshot.DurablePlan.NextAction) != "" {
		return snapshot.DurablePlan.NextAction
	}
	switch snapshot.Task.Status {
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		if snapshot.ExecutionPlan == nil {
			return "define execution plan and propose to nexus"
		}
		return "propose to nexus"
	case domain.TaskStatusWaitingForApproval:
		return "wait for nexus resolution or sync from nexus"
	case domain.TaskStatusWaitingForInput:
		if snapshot.ExecutionPlan != nil {
			return "execute the approved task manually"
		}
		return "provide the missing execution input"
	case domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return "observe execution and verification"
	case domain.TaskStatusFailed:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.Retryable && isApprovedNexusStatus(snapshot.Task.NexusStatus) {
			return "inspect failure and retry execution"
		}
		if snapshot.Task.NexusStatus == "rejected" || snapshot.Task.NexusStatus == "denied" {
			return "inspect nexus decision and adjust the task"
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
	if snapshot.DurablePlan != nil && strings.TrimSpace(snapshot.DurablePlan.NextAction) != "" {
		return fmt.Sprintf("%s has an active durable plan (%s). Next action: %s.", prefix, formatStatusForMemory(snapshot.DurablePlan.Status), snapshot.DurablePlan.NextAction)
	}

	switch snapshot.Task.Status {
	case domain.TaskStatusNew:
		return fmt.Sprintf("%s was created and is ready for investigation.", prefix)
	case domain.TaskStatusInvestigating:
		return fmt.Sprintf("%s is under investigation. Next step: %s.", prefix, nextTaskStep(snapshot))
	case domain.TaskStatusWaitingForApproval:
		if snapshot.NexusSync != nil && snapshot.NexusSync.NexusRequestID != uuid.Nil {
			return fmt.Sprintf("%s is waiting for Nexus. Request %s is currently %s.", prefix, snapshot.NexusSync.NexusRequestID.String(), formatStatusForMemory(snapshot.NexusSync.LastNexusStatus))
		}
		return fmt.Sprintf("%s is waiting for Nexus approval.", prefix)
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
		if isApprovedNexusStatus(snapshot.Task.NexusStatus) {
			return fmt.Sprintf("%s completed successfully after Nexus resolved %s.", prefix, formatStatusForMemory(snapshot.Task.NexusStatus))
		}
		return fmt.Sprintf("%s completed successfully.", prefix)
	case domain.TaskStatusFailed:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.LastError != "" {
			if snapshot.ExecutionState.Retryable {
				return fmt.Sprintf("%s failed during execution. Retry is available. Last error: %s.", prefix, snapshot.ExecutionState.LastError)
			}
			return fmt.Sprintf("%s failed during execution. Last error: %s.", prefix, snapshot.ExecutionState.LastError)
		}
		if snapshot.Task.NexusStatus != "" {
			return fmt.Sprintf("%s failed because Nexus resolved %s.", prefix, formatStatusForMemory(snapshot.Task.NexusStatus))
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
	if snapshot.Task.NexusStatus != "" {
		payload["nexus_status"] = snapshot.Task.NexusStatus
	}
	if snapshot.Task.NexusLastCheckedAt != nil {
		payload["nexus_last_checked_at"] = snapshot.Task.NexusLastCheckedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.NexusSyncError != "" {
		payload["nexus_sync_error"] = snapshot.Task.NexusSyncError
	}
	if snapshot.NexusSync != nil {
		payload["nexus"] = map[string]any{
			"nexus_request_id":     snapshot.NexusSync.NexusRequestID.String(),
			"status":               snapshot.NexusSync.LastNexusStatus,
			"http_status":          snapshot.NexusSync.LastNexusHTTPStatus,
			"last_checked_at":      snapshot.NexusSync.LastCheckedAt.UTC().Format(time.RFC3339),
			"next_check_at":        snapshot.NexusSync.NextCheckAt.UTC().Format(time.RFC3339),
			"consecutive_failures": snapshot.NexusSync.ConsecutiveFailures,
			"last_error":           snapshot.NexusSync.LastError,
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
	if snapshot.DurablePlan != nil {
		steps := make([]map[string]any, 0, len(snapshot.DurablePlan.Steps))
		for _, step := range snapshot.DurablePlan.Steps {
			steps = append(steps, map[string]any{
				"id":               step.ID.String(),
				"step_key":         step.StepKey,
				"title":            step.Title,
				"status":           step.Status,
				"expected_outcome": step.ExpectedOutcome,
				"postcondition":    step.Postcondition,
				"observation":      step.Observation,
				"blocker":          step.Blocker,
				"error_message":    step.ErrorMessage,
				"attempt_count":    step.AttemptCount,
				"sort_order":       step.SortOrder,
				"completed_at":     formatOptionalTime(step.CompletedAt),
			})
		}
		payload["durable_plan"] = map[string]any{
			"objective":   snapshot.DurablePlan.Objective,
			"status":      snapshot.DurablePlan.Status,
			"strategy":    snapshot.DurablePlan.Strategy,
			"next_action": snapshot.DurablePlan.NextAction,
			"blocker":     snapshot.DurablePlan.Blocker,
			"checkpoint":  json.RawMessage(snapshot.DurablePlan.CheckpointJSON),
			"steps":       steps,
			"updated_at":  snapshot.DurablePlan.UpdatedAt.UTC().Format(time.RFC3339),
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
		"nexus_status":      snapshot.Task.NexusStatus,
		"next_step":         nextTaskStep(snapshot),
	})
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindSummary, taskMemoryCurrentKey, buildTaskSummary(snapshot), summaryPayload); err != nil {
		slog.Warn("companion upsert task summary failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindFacts, taskMemoryCurrentKey, "", buildTaskFactsPayload(snapshot, reason)); err != nil {
		slog.Warn("companion upsert task facts failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
}

func buildNexusSyncActionPayload(origin string, prev *domain.TaskNexusSyncState, next domain.TaskNexusSyncState, beforeStatus, afterStatus, event string) json.RawMessage {
	type syncSnapshot struct {
		NexusRequestID string `json:"nexus_request_id,omitempty"`
		Status         string `json:"status,omitempty"`
		HTTPStatus     int    `json:"http_status,omitempty"`
		Error          string `json:"error,omitempty"`
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
		Status:     next.LastNexusStatus,
		HTTPStatus: next.LastNexusHTTPStatus,
		Error:      next.LastError,
	}
	if next.NexusRequestID != uuid.Nil {
		current.NexusRequestID = next.NexusRequestID.String()
	}
	payload["current"] = current
	if prev != nil {
		previous := syncSnapshot{
			Status:     prev.LastNexusStatus,
			HTTPStatus: prev.LastNexusHTTPStatus,
			Error:      prev.LastError,
		}
		if prev.NexusRequestID != uuid.Nil {
			previous.NexusRequestID = prev.NexusRequestID.String()
		}
		payload["previous"] = previous
	}
	return marshalOrEmpty("nexus_sync_payload", payload)
}

func (u *Usecases) latestNexusRequestIDForTask(ctx context.Context, taskID uuid.UUID, state *domain.TaskNexusSyncState) (uuid.UUID, error) {
	if state != nil && state.NexusRequestID != uuid.Nil {
		return state.NexusRequestID, nil
	}
	return u.repo.LatestProposeNexusRequestID(ctx, taskID)
}

func (u *Usecases) persistNexusSyncAction(ctx context.Context, taskID uuid.UUID, nexusRequestID uuid.UUID, origin string, prev *domain.TaskNexusSyncState, next domain.TaskNexusSyncState, beforeStatus, afterStatus, event string) {
	payload := buildNexusSyncActionPayload(origin, prev, next, beforeStatus, afterStatus, event)
	nexusRequestIDCopy := nexusRequestID
	if _, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionSyncNexus,
		Payload:        payload,
		NexusRequestID: &nexusRequestIDCopy,
	}); err != nil {
		slog.Warn("companion sync_nexus action failed", "task_id", taskID.String(), "nexus_request_id", nexusRequestID.String(), "error", err)
	}
}

func (u *Usecases) syncTaskWithNexus(ctx context.Context, t domain.Task, origin string) (domain.Task, *domain.TaskNexusSyncState, error) {
	if t.Status != domain.TaskStatusWaitingForApproval {
		return t, nil, nil
	}

	var prevState *domain.TaskNexusSyncState
	currentState, err := u.repo.GetNexusSyncState(ctx, t.ID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.Task{}, nil, err
	}

	rid, err := u.latestNexusRequestIDForTask(ctx, t.ID, prevState)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return t, prevState, nil
		}
		return domain.Task{}, prevState, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskNexusSyncState{
		TaskID:         t.ID,
		NexusRequestID: rid,
		LastCheckedAt:  now,
		NextCheckAt:    nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastNexusStatus = prevState.LastNexusStatus
		nextState.LastNexusHTTPStatus = prevState.LastNexusHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, st, gErr := u.nexus.GetRequest(ctx, rid.String())
	beforeStatus := t.Status
	appliedEvent := ""

	if gErr != nil {
		nextState.LastNexusHTTPStatus = st
		nextState.LastError = gErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertNexusSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if nexusSnapshotChanged(prevState, stateOut) {
			u.persistNexusSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "nexus_sync_error")
		}
		return domain.Task{}, &stateOut, fmt.Errorf("nexus get request: %w", gErr)
	}

	nextState.LastNexusHTTPStatus = st

	if st == http.StatusNotFound {
		nextState.LastError = "nexus request not found"
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertNexusSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if nexusSnapshotChanged(prevState, stateOut) {
			u.persistNexusSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "nexus_sync_not_found")
		}
		t.NexusStatus = stateOut.LastNexusStatus
		t.NexusLastCheckedAt = &stateOut.LastCheckedAt
		t.NexusSyncError = stateOut.LastError
		return t, &stateOut, nil
	}
	if normalizedStatus := normalizeNexusStatus(sum.Status); normalizedStatus != "" {
		nextState.LastNexusStatus = normalizedStatus
	}

	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0)

	plan, planErr := u.getExecutionPlan(ctx, t.ID)
	if planErr != nil {
		return domain.Task{}, prevState, planErr
	}
	ev, apply := eventFromNexusRequestStatusWithExecutionPlan(sum.Status, plan != nil)
	if apply {
		appliedEvent = ev
		t, err = u.applyTaskEvent(ctx, t, ev)
		if err != nil {
			return domain.Task{}, prevState, err
		}
	}

	stateOut, upErr := u.repo.UpsertNexusSyncState(ctx, nextState)
	if upErr != nil {
		return domain.Task{}, prevState, upErr
	}
	if nexusSnapshotChanged(prevState, stateOut) || beforeStatus != t.Status {
		u.persistNexusSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
		u.syncTaskMemory(ctx, t.ID, "nexus_sync")
	}
	t.NexusStatus = stateOut.LastNexusStatus
	t.NexusLastCheckedAt = &stateOut.LastCheckedAt
	t.NexusSyncError = stateOut.LastError

	slog.Info("companion task synced from nexus",
		"task_id", t.ID.String(),
		"nexus_request_id", rid.String(),
		"nexus_status", stateOut.LastNexusStatus,
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
			AuthorID:   CompanionRequesterID,
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

func (u *Usecases) Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, nexusclient.SubmitResponse, error) {
	var zeroA domain.TaskAction
	var zeroSub nexusclient.SubmitResponse
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
		ConnectorID:        plan.ConnectorID,
		OrgID:              t.OrgID,
		ActorID:            executionActorID(t),
		ActorType:          executionActorType(t),
		CompanionPrincipal: CompanionRequesterID,
		OnBehalfOf:         executionOnBehalfOf(t),
		ServicePrincipal:   true,
		ProductSurface:     "companion",
		Operation:          plan.Operation,
		Payload:            plan.Payload,
		IdempotencyKey:     idempotencyKey,
		TaskID:             &t.ID,
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
	submitBody := nexusclient.SubmitRequestBody{
		RequesterType:  CompanionRequesterType,
		RequesterID:    CompanionRequesterID,
		RequesterName:  CompanionRequesterName,
		ActionType:     ActionTypePropose,
		TargetSystem:   stringFromBinding(binding, "target_system", in.TargetSystem),
		TargetResource: stringFromBinding(binding, "target_resource", in.TargetResource),
		ActionBinding:  binding,
		Params:         params,
		Reason:         reason,
		Context:        string(ctxStr),
	}

	submitOut, subErr := u.nexus.SubmitRequest(ctx, idem, submitBody)
	if subErr != nil {
		slog.Warn("companion propose nexus submit failed",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"error", subErr,
		)
		_ = u.repo.UpdateActionNexusResult(ctx, action.ID, nil, subErr.Error())
		t2, ge := u.repo.GetTaskByID(ctx, taskID)
		if ge != nil {
			return domain.Task{}, action, zeroSub, ge
		}
		return t2, action, zeroSub, fmt.Errorf("%w: %v", ErrNexusSubmit, subErr)
	}
	reqUUID, perr := uuid.Parse(submitOut.RequestID)
	if perr != nil {
		_ = u.repo.UpdateActionNexusResult(ctx, action.ID, nil, "invalid request_id from nexus")
		return domain.Task{}, action, zeroSub, fmt.Errorf("parse request_id: %w", perr)
	}
	if err := u.repo.UpdateActionNexusResult(ctx, action.ID, &reqUUID, ""); err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	now := time.Now().UTC()
	state, err := u.repo.UpsertNexusSyncState(ctx, domain.TaskNexusSyncState{
		TaskID:              taskID,
		NexusRequestID:      reqUUID,
		LastNexusStatus:     normalizeNexusStatus(submitOut.Status),
		LastNexusHTTPStatus: http.StatusCreated,
		LastCheckedAt:       now,
		LastError:           "",
		ConsecutiveFailures: 0,
		NextCheckAt:         nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0),
	})
	if err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	ev, evErr := eventFromSubmitResponseWithExecutionPlan(submitOut, true)
	if evErr != nil {
		slog.Error("companion propose unexpected nexus status",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"nexus_status", submitOut.Status,
			"error", evErr,
		)
		return domain.Task{}, action, submitOut, evErr
	}
	t, err = u.applyTaskEvent(ctx, t, ev)
	if err != nil {
		return domain.Task{}, action, submitOut, err
	}
	t.NexusStatus = state.LastNexusStatus
	t.NexusLastCheckedAt = &state.LastCheckedAt
	t.NexusSyncError = state.LastError
	action.NexusRequestID = &reqUUID
	slog.Info("companion propose submitted to nexus",
		"task_id", taskID.String(),
		"action_id", action.ID.String(),
		"nexus_request_id", reqUUID.String(),
		"nexus_decision", submitOut.Decision,
		"nexus_status", submitOut.Status,
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

type SetTaskPlanInput struct {
	Objective       string
	Status          string
	Strategy        string
	AssumptionsJSON json.RawMessage
	ConstraintsJSON json.RawMessage
	CheckpointJSON  json.RawMessage
	NextAction      string
	Blocker         string
	CreatedBy       string
	Steps           []SetTaskPlanStepInput
}

type SetTaskPlanStepInput struct {
	ID              uuid.UUID
	StepKey         string
	Title           string
	Status          string
	DependsOnJSON   json.RawMessage
	ToolName        string
	Capability      string
	ExpectedOutcome string
	Postcondition   string
	EvidenceJSON    json.RawMessage
	Observation     string
	Blocker         string
	ErrorMessage    string
	AttemptCount    int
	SortOrder       int
}

type UpdateTaskPlanStepInput struct {
	Status         string
	EvidenceJSON   json.RawMessage
	Observation    string
	Blocker        string
	ErrorMessage   string
	CheckpointJSON json.RawMessage
	NextAction     string
}

type RecordTaskPlanCheckpointInput struct {
	Status         string
	CheckpointJSON json.RawMessage
	NextAction     string
	Blocker        string
}

type PrepareTaskPlanCompensationInput struct {
	Reason string
}

type TaskPlanCompensationOutput struct {
	Plan                domain.TaskPlan
	Step                domain.TaskPlanStep
	Status              string
	Reason              string
	Compensation        map[string]any
	NexusRequestID      string
	NexusStatus         string
	NexusDecision       string
	NexusBindingHash    string
	ApprovalRequired    bool
	ApprovalUnavailable bool
}

type ExecuteTaskPlanCompensationInput struct {
	NexusRequestID string
}

type TaskPlanCompensationExecutionOutput struct {
	Plan             domain.TaskPlan
	Step             domain.TaskPlanStep
	Status           string
	Reason           string
	Compensation     map[string]any
	NexusRequestID   string
	NexusStatus      string
	Execution        connectordomain.ExecutionResult
	Verification     domain.TaskVerificationResult
	ApprovalRequired bool
}

type taskExecutionGraphRepository interface {
	ListTaskExecutionGraph(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskExecutionGraphEvent, error)
}

func (u *Usecases) SetTaskPlan(ctx context.Context, taskID uuid.UUID, in SetTaskPlanInput) (domain.TaskPlan, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	objective := strings.TrimSpace(in.Objective)
	if objective == "" {
		objective = strings.TrimSpace(t.Goal)
	}
	if objective == "" {
		objective = strings.TrimSpace(t.Title)
	}
	if objective == "" {
		return domain.TaskPlan{}, fmt.Errorf("objective is required")
	}
	if len(in.Steps) == 0 {
		return domain.TaskPlan{}, fmt.Errorf("at least one plan step is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = domain.TaskPlanStatusActive
	}
	if !validTaskPlanStatus(status) {
		return domain.TaskPlan{}, fmt.Errorf("invalid plan status")
	}
	plan := domain.TaskPlan{
		TaskID:          taskID,
		OrgID:           t.OrgID,
		Objective:       objective,
		Status:          status,
		Strategy:        strings.TrimSpace(in.Strategy),
		AssumptionsJSON: jsonOrDefault(in.AssumptionsJSON, `[]`),
		ConstraintsJSON: jsonOrDefault(in.ConstraintsJSON, `[]`),
		CheckpointJSON:  jsonOrDefault(in.CheckpointJSON, `{}`),
		NextAction:      strings.TrimSpace(in.NextAction),
		Blocker:         strings.TrimSpace(in.Blocker),
		CreatedBy:       strings.TrimSpace(in.CreatedBy),
		Steps:           make([]domain.TaskPlanStep, 0, len(in.Steps)),
	}
	for i, inputStep := range in.Steps {
		step, err := buildTaskPlanStep(t.OrgID, taskID, i, inputStep)
		if err != nil {
			return domain.TaskPlan{}, err
		}
		plan.Steps = append(plan.Steps, step)
	}
	if plan.NextAction == "" {
		plan.NextAction = nextActionFromSteps(plan.Steps)
	}
	applyPlanCompletion(&plan)
	saved, err := u.repo.UpsertTaskPlan(ctx, plan)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionSetDurablePlan,
		Payload:    taskPlanActionPayload(saved),
	}); insertErr != nil {
		slog.Warn("companion set durable plan action failed", "task_id", taskID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "set_durable_plan")
	return saved, nil
}

func (u *Usecases) ListTaskExecutionGraph(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskExecutionGraphEvent, error) {
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return nil, err
	}
	repo, ok := u.repo.(taskExecutionGraphRepository)
	if !ok {
		return nil, fmt.Errorf("task execution graph repository is not configured")
	}
	return repo.ListTaskExecutionGraph(ctx, taskID, limit)
}

func (u *Usecases) UpdateTaskPlanStep(ctx context.Context, taskID, stepID uuid.UUID, in UpdateTaskPlanStepInput) (domain.TaskPlan, error) {
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskPlan{}, err
	}
	var step *domain.TaskPlanStep
	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			step = &plan.Steps[i]
			break
		}
	}
	if step == nil {
		return domain.TaskPlan{}, ErrNotFound
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		if !validTaskPlanStepStatus(status) {
			return domain.TaskPlan{}, fmt.Errorf("invalid plan step status")
		}
		step.Status = status
	}
	if len(in.EvidenceJSON) > 0 {
		step.EvidenceJSON = jsonOrDefault(in.EvidenceJSON, `{}`)
	}
	if strings.TrimSpace(in.Observation) != "" {
		step.Observation = strings.TrimSpace(in.Observation)
	}
	if strings.TrimSpace(in.Blocker) != "" {
		step.Blocker = strings.TrimSpace(in.Blocker)
	}
	if strings.TrimSpace(in.ErrorMessage) != "" {
		step.ErrorMessage = strings.TrimSpace(in.ErrorMessage)
	}
	if step.Status == domain.TaskPlanStepStatusRunning {
		step.AttemptCount++
	}
	if isTerminalTaskPlanStepStatus(step.Status) {
		now := time.Now().UTC()
		step.CompletedAt = &now
	}
	if _, err := u.repo.UpdateTaskPlanStep(ctx, *step); err != nil {
		return domain.TaskPlan{}, err
	}
	updated, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if len(in.CheckpointJSON) > 0 {
		updated.CheckpointJSON = jsonOrDefault(in.CheckpointJSON, `{}`)
	}
	if strings.TrimSpace(in.NextAction) != "" {
		updated.NextAction = strings.TrimSpace(in.NextAction)
	} else {
		updated.NextAction = nextActionFromSteps(updated.Steps)
	}
	updated.Blocker = firstPlanBlocker(updated.Steps)
	updated.Status = statusFromPlanSteps(updated.Steps, updated.Status)
	applyPlanCompletion(&updated)
	updated, err = u.repo.UpdateTaskPlan(ctx, updated)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionUpdatePlanStep,
		Payload: marshalOrEmpty("task_plan_step_action", map[string]any{
			"step_id":       stepID.String(),
			"status":        step.Status,
			"observation":   step.Observation,
			"blocker":       step.Blocker,
			"error_message": step.ErrorMessage,
		}),
	}); insertErr != nil {
		slog.Warn("companion update plan step action failed", "task_id", taskID.String(), "step_id", stepID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "update_plan_step")
	return updated, nil
}

func (u *Usecases) RecordTaskPlanCheckpoint(ctx context.Context, taskID uuid.UUID, in RecordTaskPlanCheckpointInput) (domain.TaskPlan, error) {
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskPlan{}, err
	}
	if len(in.CheckpointJSON) > 0 {
		plan.CheckpointJSON = jsonOrDefault(in.CheckpointJSON, `{}`)
	}
	if status := strings.TrimSpace(in.Status); status != "" {
		if !validTaskPlanStatus(status) {
			return domain.TaskPlan{}, fmt.Errorf("invalid plan status")
		}
		plan.Status = status
	}
	if strings.TrimSpace(in.NextAction) != "" {
		plan.NextAction = strings.TrimSpace(in.NextAction)
	}
	if strings.TrimSpace(in.Blocker) != "" {
		plan.Blocker = strings.TrimSpace(in.Blocker)
	}
	applyPlanCompletion(&plan)
	updated, err := u.repo.UpdateTaskPlan(ctx, plan)
	if err != nil {
		return domain.TaskPlan{}, err
	}
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionPlanCheckpoint,
		Payload: marshalOrEmpty("task_plan_checkpoint_action", map[string]any{
			"status":      updated.Status,
			"next_action": updated.NextAction,
			"blocker":     updated.Blocker,
			"checkpoint":  json.RawMessage(updated.CheckpointJSON),
		}),
	}); insertErr != nil {
		slog.Warn("companion plan checkpoint action failed", "task_id", taskID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "plan_checkpoint")
	return updated, nil
}

func (u *Usecases) GetTaskPlan(ctx context.Context, taskID uuid.UUID) (domain.TaskPlan, error) {
	return u.repo.GetTaskPlan(ctx, taskID)
}

func (u *Usecases) PrepareTaskPlanCompensation(ctx context.Context, taskID, stepID uuid.UUID, in PrepareTaskPlanCompensationInput) (TaskPlanCompensationOutput, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return TaskPlanCompensationOutput{}, err
	}
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return TaskPlanCompensationOutput{}, err
	}
	step, ok := findTaskPlanStep(plan, stepID)
	if !ok {
		return TaskPlanCompensationOutput{}, ErrNotFound
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return TaskPlanCompensationOutput{}, fmt.Errorf("reason is required")
	}

	compensation, supported := compensationFromTaskPlanStepEvidence(step.EvidenceJSON)
	status := "compensation_unavailable"
	planStatus := domain.TaskPlanStatusBlocked
	blocker := "compensation is not declared for this step"
	nextAction := "review step manually"
	checkpoint := map[string]any{
		"source":            "prepare_task_plan_compensation",
		"status":            status,
		"step_id":           step.ID.String(),
		"step_key":          step.StepKey,
		"reason":            reason,
		"approval_required": true,
		"compensation":      compensation,
	}

	var submitOut nexusclient.SubmitResponse
	if supported {
		if u.executor == nil {
			checkpoint["status"] = "compensation_contract_invalid"
			checkpoint["error"] = "task execution is not configured"
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "configure connector execution before compensation", "compensation execution is not configured")
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "compensation_contract_invalid", Reason: reason, Compensation: compensation, ApprovalRequired: true, ApprovalUnavailable: true}, nil
		}
		spec, specErr := compensationExecutionSpec(t, step, compensation, reason, nil)
		if specErr != nil {
			checkpoint["status"] = "compensation_contract_invalid"
			checkpoint["error"] = specErr.Error()
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "fix compensation contract", "compensation contract invalid: "+specErr.Error())
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "compensation_contract_invalid", Reason: reason, Compensation: compensation, ApprovalRequired: true}, nil
		}
		binding, bindingHash, bindingErr := u.executor.BuildActionBinding(ctx, spec)
		if bindingErr != nil {
			checkpoint["status"] = "compensation_contract_invalid"
			checkpoint["error"] = bindingErr.Error()
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "fix compensation contract", "compensation action binding failed: "+bindingErr.Error())
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "compensation_contract_invalid", Reason: reason, Compensation: compensation, ApprovalRequired: true}, nil
		}
		if u.nexus == nil {
			checkpoint["status"] = "approval_unavailable"
			checkpoint["error"] = "nexus not configured"
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, planStatus, checkpoint, "review compensation manually", "compensation requires approval but Nexus is not configured")
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{
				Plan:                updated,
				Step:                step,
				Status:              "approval_unavailable",
				Reason:              reason,
				Compensation:        compensation,
				ApprovalRequired:    true,
				ApprovalUnavailable: true,
			}, nil
		}
		targetSystem := stringFromBinding(binding, "target_system", "capability")
		targetResource := stringFromBinding(binding, "target_resource", step.ID.String())
		idempotencyKey := stringFromBinding(binding, "idempotency_key", defaultCompensationIdempotencyKey(taskID, stepID))
		params := map[string]any{
			"org_id":               t.OrgID,
			"task_id":              taskID.String(),
			"plan_step_id":         step.ID.String(),
			"step_key":             step.StepKey,
			"reason":               reason,
			"compensation":         compensation,
			"compensation_payload": json.RawMessage(spec.Payload),
			"original_tool":        step.ToolName,
			"original_status":      step.Status,
			"action_binding":       binding,
			"action_binding_hash":  bindingHash,
		}
		if step.EvidenceJSON != nil {
			params["step_evidence"] = json.RawMessage(step.EvidenceJSON)
		}
		submitBody := nexusclient.SubmitRequestBody{
			RequesterType:  "agent",
			RequesterID:    CompanionRequesterID,
			RequesterName:  CompanionRequesterName,
			ActionType:     nexusclient.ActionTypeAgentCapabilityCompensate,
			TargetSystem:   targetSystem,
			TargetResource: targetResource,
			ActionBinding:  binding,
			Params:         params,
			Reason:         "Compensate task plan step: " + reason,
			Context: string(marshalOrEmpty("task_plan_compensation_context", map[string]any{
				"task_title":       t.Title,
				"task_goal":        t.Goal,
				"plan_objective":   plan.Objective,
				"step_title":       step.Title,
				"expected_outcome": step.ExpectedOutcome,
				"postcondition":    step.Postcondition,
			})),
		}
		var subErr error
		submitOut, subErr = u.nexus.SubmitRequest(ctx, idempotencyKey, submitBody)
		if subErr != nil {
			checkpoint["status"] = "approval_submit_failed"
			checkpoint["error"] = subErr.Error()
			updated, updateErr := u.recordCompensationCheckpoint(ctx, taskID, domain.TaskPlanStatusBlocked, checkpoint, "retry compensation approval request", "compensation approval request failed: "+subErr.Error())
			if updateErr != nil {
				return TaskPlanCompensationOutput{}, updateErr
			}
			return TaskPlanCompensationOutput{Plan: updated, Step: step, Status: "approval_submit_failed", Reason: reason, Compensation: compensation, ApprovalRequired: true, ApprovalUnavailable: true}, fmt.Errorf("%w: %v", ErrNexusSubmit, subErr)
		}
		status = "compensation_approval_requested"
		planStatus = domain.TaskPlanStatusEscalated
		blocker = "compensation requires approval: " + reason
		nextAction = "await compensation approval decision"
		checkpoint["status"] = status
		checkpoint["nexus_request_id"] = submitOut.RequestID
		checkpoint["nexus_status"] = submitOut.Status
		checkpoint["nexus_decision"] = submitOut.Decision
		checkpoint["nexus_binding_hash"] = submitOut.BindingHash
		if submitOut.Status == nexusclient.StatusAllowed || submitOut.Status == nexusclient.StatusApproved {
			nextAction = "execute approved compensation under governed capability path"
			blocker = ""
		}
	}

	updated, err := u.recordCompensationCheckpoint(ctx, taskID, planStatus, checkpoint, nextAction, blocker)
	if err != nil {
		return TaskPlanCompensationOutput{}, err
	}
	return TaskPlanCompensationOutput{
		Plan:             updated,
		Step:             step,
		Status:           status,
		Reason:           reason,
		Compensation:     compensation,
		NexusRequestID:   submitOut.RequestID,
		NexusStatus:      submitOut.Status,
		NexusDecision:    submitOut.Decision,
		NexusBindingHash: submitOut.BindingHash,
		ApprovalRequired: true,
	}, nil
}

func (u *Usecases) ExecuteTaskPlanCompensation(ctx context.Context, taskID, stepID uuid.UUID, in ExecuteTaskPlanCompensationInput) (TaskPlanCompensationExecutionOutput, error) {
	var out TaskPlanCompensationExecutionOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}
	if u.nexus == nil {
		return out, fmt.Errorf("nexus is not configured")
	}
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetTaskPlan(ctx, taskID)
	if err != nil {
		return out, err
	}
	step, ok := findTaskPlanStep(plan, stepID)
	if !ok {
		return out, ErrNotFound
	}
	action, reason, compensation, err := u.latestCompensationAction(ctx, taskID, stepID, strings.TrimSpace(in.NexusRequestID))
	if err != nil {
		return out, err
	}
	nexusRequestID, err := compensationNexusRequestID(action, in.NexusRequestID)
	if err != nil {
		return out, err
	}
	sum, statusCode, err := u.nexus.GetRequest(ctx, nexusRequestID.String())
	if err != nil {
		return out, fmt.Errorf("nexus get compensation request: %w", err)
	}
	if statusCode == http.StatusNotFound {
		return out, fmt.Errorf("nexus compensation request not found")
	}
	nexusStatus := normalizeNexusStatus(sum.Status)
	if !isApprovedNexusStatus(nexusStatus) {
		return out, u.nexusBlockedError(nexusRequestID.String(), nexusStatus, "execute_compensation")
	}

	spec, err := compensationExecutionSpec(t, step, compensation, reason, &nexusRequestID)
	if err != nil {
		return out, err
	}
	result, execErr := u.executor.Execute(ctx, spec)
	if execErr != nil {
		result = connectordomain.ExecutionResult{
			ID:             uuid.New(),
			ConnectorID:    spec.ConnectorID,
			OrgID:          spec.OrgID,
			ActorID:        spec.ActorID,
			Operation:      spec.Operation,
			Status:         connectordomain.ExecFailure,
			Payload:        spec.Payload,
			ResultJSON:     json.RawMessage(`{}`),
			ErrorMessage:   execErr.Error(),
			Retryable:      true,
			IdempotencyKey: spec.IdempotencyKey,
			TaskID:         spec.TaskID,
			NexusRequestID: spec.NexusRequestID,
			CreatedAt:      time.Now().UTC(),
		}
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	u.reportExecutionToNexus(ctx, &nexusRequestID, result)
	verification := verifyExecutionResult(result)
	status := "compensation_executed"
	planStatus := domain.TaskPlanStatusCompleted
	nextAction := "compensation executed"
	blocker := ""
	if result.Status != connectordomain.ExecSuccess || verification.Status != domain.VerificationStatusVerified {
		status = "compensation_failed"
		planStatus = domain.TaskPlanStatusFailed
		nextAction = "review failed compensation"
		blocker = firstNonEmptyString(result.ErrorMessage, verification.Summary, "compensation execution failed")
	}

	payload := buildConnectorExecutionPayload(result)
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionExecuteComp,
		Payload:        payload,
		NexusRequestID: &nexusRequestID,
		ErrorMessage:   result.ErrorMessage,
	}); insertErr != nil {
		slog.Warn("companion execute compensation action failed", "task_id", taskID.String(), "error", insertErr)
	}
	artifactKind := TaskArtifactConnectorExecution
	if result.Status != connectordomain.ExecSuccess {
		artifactKind = TaskArtifactExecutionError
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  taskID,
		Kind:    artifactKind,
		URI:     result.ExternalRef,
		Payload: payload,
	}); artifactErr != nil {
		slog.Warn("companion execute compensation artifact failed", "task_id", taskID.String(), "error", artifactErr)
	}

	checkpoint := map[string]any{
		"source":           "execute_task_plan_compensation",
		"status":           status,
		"step_id":          step.ID.String(),
		"step_key":         step.StepKey,
		"reason":           reason,
		"nexus_request_id": nexusRequestID.String(),
		"nexus_status":     nexusStatus,
		"compensation":     compensation,
		"execution":        json.RawMessage(payload),
		"verification":     buildVerificationPayload(result, verification),
		"approval_passed":  true,
	}
	updated, err := u.RecordTaskPlanCheckpoint(ctx, taskID, RecordTaskPlanCheckpointInput{
		Status:         planStatus,
		CheckpointJSON: marshalOrEmpty("task_plan_compensation_execution_checkpoint", checkpoint),
		NextAction:     nextAction,
		Blocker:        blocker,
	})
	if err != nil {
		return out, err
	}
	u.syncTaskMemory(ctx, taskID, "execute_compensation")
	return TaskPlanCompensationExecutionOutput{
		Plan:             updated,
		Step:             step,
		Status:           status,
		Reason:           reason,
		Compensation:     compensation,
		NexusRequestID:   nexusRequestID.String(),
		NexusStatus:      nexusStatus,
		Execution:        result,
		Verification:     verification,
		ApprovalRequired: true,
	}, nil
}

func (u *Usecases) recordCompensationCheckpoint(ctx context.Context, taskID uuid.UUID, status string, checkpoint map[string]any, nextAction, blocker string) (domain.TaskPlan, error) {
	updated, err := u.RecordTaskPlanCheckpoint(ctx, taskID, RecordTaskPlanCheckpointInput{
		Status:         status,
		CheckpointJSON: marshalOrEmpty("task_plan_compensation_checkpoint", checkpoint),
		NextAction:     nextAction,
		Blocker:        blocker,
	})
	if err != nil {
		return domain.TaskPlan{}, err
	}
	nexusRequestID := uuidFromAny(checkpoint["nexus_request_id"])
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionPrepareComp,
		NexusRequestID: nexusRequestID,
		Payload: marshalOrEmpty("task_plan_compensation_action", map[string]any{
			"status":           checkpoint["status"],
			"step_id":          checkpoint["step_id"],
			"reason":           checkpoint["reason"],
			"nexus_request_id": checkpoint["nexus_request_id"],
			"nexus_status":     checkpoint["nexus_status"],
			"compensation":     checkpoint["compensation"],
		}),
	}); insertErr != nil {
		slog.Warn("companion prepare compensation action failed", "task_id", taskID.String(), "error", insertErr)
	}
	u.syncTaskMemory(ctx, taskID, "prepare_compensation")
	return updated, nil
}

func findTaskPlanStep(plan domain.TaskPlan, stepID uuid.UUID) (domain.TaskPlanStep, bool) {
	for _, step := range plan.Steps {
		if step.ID == stepID {
			return step, true
		}
	}
	return domain.TaskPlanStep{}, false
}

func (u *Usecases) latestCompensationAction(ctx context.Context, taskID, stepID uuid.UUID, requestedNexusID string) (domain.TaskAction, string, map[string]any, error) {
	actions, err := u.repo.ListActionsByTaskID(ctx, taskID)
	if err != nil {
		return domain.TaskAction{}, "", nil, err
	}
	requestedNexusID = strings.TrimSpace(requestedNexusID)
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		if action.ActionType != TaskActionPrepareComp {
			continue
		}
		payload := map[string]any{}
		if len(action.Payload) > 0 {
			_ = json.Unmarshal(action.Payload, &payload)
		}
		if strings.TrimSpace(fmt.Sprint(payload["step_id"])) != stepID.String() {
			continue
		}
		actionNexusID := strings.TrimSpace(fmt.Sprint(payload["nexus_request_id"]))
		if action.NexusRequestID != nil {
			actionNexusID = action.NexusRequestID.String()
		}
		if requestedNexusID != "" && actionNexusID != requestedNexusID {
			continue
		}
		compensation, _ := mapAnyFrom(payload["compensation"])
		if len(compensation) == 0 {
			return domain.TaskAction{}, "", nil, fmt.Errorf("prepared compensation action has no compensation payload")
		}
		return action, strings.TrimSpace(fmt.Sprint(payload["reason"])), compensation, nil
	}
	return domain.TaskAction{}, "", nil, ErrNotFound
}

func compensationNexusRequestID(action domain.TaskAction, override string) (uuid.UUID, error) {
	if id, err := uuid.Parse(strings.TrimSpace(override)); err == nil && id != uuid.Nil {
		return id, nil
	}
	if action.NexusRequestID != nil && *action.NexusRequestID != uuid.Nil {
		return *action.NexusRequestID, nil
	}
	var payload map[string]any
	if len(action.Payload) > 0 && json.Unmarshal(action.Payload, &payload) == nil {
		if id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(payload["nexus_request_id"]))); err == nil && id != uuid.Nil {
			return id, nil
		}
	}
	return uuid.Nil, fmt.Errorf("prepared compensation has no nexus_request_id")
}

func compensationFromTaskPlanStepEvidence(raw json.RawMessage) (map[string]any, bool) {
	var evidence map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &evidence) != nil {
		return map[string]any{"supported": false}, false
	}
	if compensation, ok := mapAnyFrom(evidence["compensation"]); ok {
		supported := boolAny(compensation["supported"])
		return compensation, supported
	}
	if metadata, ok := mapAnyFrom(evidence["tool_metadata"]); ok && boolAny(metadata["rollback_supported"]) {
		compensation := map[string]any{
			"supported":      true,
			"capability_id":  strings.TrimSpace(fmt.Sprint(metadata["rollback_capability_id"])),
			"requires_nexus": true,
		}
		return compensation, true
	}
	return map[string]any{"supported": false}, false
}

func compensationExecutionSpec(t domain.Task, step domain.TaskPlanStep, compensation map[string]any, reason string, nexusRequestID *uuid.UUID) (connectordomain.ExecutionSpec, error) {
	originalBinding := originalActionBindingFromTaskPlanStepEvidence(step.EvidenceJSON)
	connectorIDRaw := firstNonEmptyString(
		strings.TrimSpace(fmt.Sprint(compensation["connector_id"])),
		stringFromBinding(originalBinding, "connector_id", ""),
	)
	connectorID, err := uuid.Parse(connectorIDRaw)
	if err != nil || connectorID == uuid.Nil {
		return connectordomain.ExecutionSpec{}, fmt.Errorf("valid compensation connector_id is required")
	}
	operation := firstNonEmptyString(
		strings.TrimSpace(fmt.Sprint(compensation["operation"])),
		strings.TrimSpace(fmt.Sprint(compensation["capability_id"])),
	)
	if operation == "" {
		return connectordomain.ExecutionSpec{}, fmt.Errorf("compensation operation is required")
	}
	payload := compensationExecutionPayload(t, step, compensation, reason, originalBinding)
	return connectordomain.ExecutionSpec{
		ConnectorID:        connectorID,
		OrgID:              t.OrgID,
		ActorID:            CompanionRequesterID,
		ActorType:          "agent",
		CompanionPrincipal: CompanionRequesterID,
		OnBehalfOf:         executionOnBehalfOf(t),
		ServicePrincipal:   true,
		ProductSurface:     firstNonEmptyString(stringFromBinding(originalBinding, "product_surface", ""), "companion"),
		RunID:              t.ID.String(),
		ToolInvocationID:   "compensation:" + step.ID.String(),
		Operation:          operation,
		Payload:            payload,
		IdempotencyKey:     defaultCompensationIdempotencyKey(t.ID, step.ID),
		TaskID:             &t.ID,
		NexusRequestID:     nexusRequestID,
	}, nil
}

func compensationExecutionPayload(t domain.Task, step domain.TaskPlanStep, compensation map[string]any, reason string, originalBinding map[string]any) json.RawMessage {
	payload := map[string]any{
		"org_id":                  t.OrgID,
		"task_id":                 t.ID.String(),
		"plan_step_id":            step.ID.String(),
		"step_key":                step.StepKey,
		"reason":                  reason,
		"compensation":            compensation,
		"original_tool_name":      step.ToolName,
		"original_action_binding": originalBinding,
	}
	if supplied, ok := mapAnyFrom(compensation["payload"]); ok {
		payload = cloneAnyMap(supplied)
		payload["org_id"] = t.OrgID
		payload["task_id"] = t.ID.String()
		payload["plan_step_id"] = step.ID.String()
		if reason != "" {
			payload["reason"] = reason
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func originalActionBindingFromTaskPlanStepEvidence(raw json.RawMessage) map[string]any {
	var evidence map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &evidence) != nil {
		return nil
	}
	if binding, ok := mapAnyFrom(evidence["action_binding"]); ok {
		return cloneAnyMap(binding)
	}
	if toolResult, ok := mapAnyFrom(evidence["tool_result"]); ok {
		if binding, ok := mapAnyFrom(toolResult["action_binding"]); ok {
			return cloneAnyMap(binding)
		}
		if innerEvidence, ok := mapAnyFrom(toolResult["evidence"]); ok {
			if binding, ok := mapAnyFrom(innerEvidence["action_binding"]); ok {
				return cloneAnyMap(binding)
			}
		}
	}
	return nil
}

func mapAnyFrom(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]json.RawMessage:
		out := make(map[string]any, len(typed))
		for key, raw := range typed {
			var decoded any
			if json.Unmarshal(raw, &decoded) == nil {
				out[key] = decoded
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func boolAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultCompensationIdempotencyKey(taskID, stepID uuid.UUID) string {
	return fmt.Sprintf("task-plan-compensation-%s-%s", taskID.String(), stepID.String())
}

func uuidFromAny(value any) *uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(value)))
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func buildTaskPlanStep(orgID string, taskID uuid.UUID, index int, in SetTaskPlanStepInput) (domain.TaskPlanStep, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return domain.TaskPlanStep{}, fmt.Errorf("plan step title is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = domain.TaskPlanStepStatusPending
	}
	if !validTaskPlanStepStatus(status) {
		return domain.TaskPlanStep{}, fmt.Errorf("invalid plan step status")
	}
	stepKey := strings.TrimSpace(in.StepKey)
	if stepKey == "" {
		stepKey = fmt.Sprintf("step-%d", index+1)
	}
	sortOrder := in.SortOrder
	if sortOrder == 0 {
		sortOrder = index + 1
	}
	step := domain.TaskPlanStep{
		ID:              in.ID,
		TaskID:          taskID,
		OrgID:           orgID,
		StepKey:         stepKey,
		Title:           title,
		Status:          status,
		DependsOnJSON:   jsonOrDefault(in.DependsOnJSON, `[]`),
		ToolName:        strings.TrimSpace(in.ToolName),
		Capability:      strings.TrimSpace(in.Capability),
		ExpectedOutcome: strings.TrimSpace(in.ExpectedOutcome),
		Postcondition:   strings.TrimSpace(in.Postcondition),
		EvidenceJSON:    jsonOrDefault(in.EvidenceJSON, `{}`),
		Observation:     strings.TrimSpace(in.Observation),
		Blocker:         strings.TrimSpace(in.Blocker),
		ErrorMessage:    strings.TrimSpace(in.ErrorMessage),
		AttemptCount:    in.AttemptCount,
		SortOrder:       sortOrder,
	}
	if isTerminalTaskPlanStepStatus(step.Status) {
		now := time.Now().UTC()
		step.CompletedAt = &now
	}
	return step, nil
}

func validTaskPlanStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.TaskPlanStatusDraft, domain.TaskPlanStatusActive, domain.TaskPlanStatusBlocked,
		domain.TaskPlanStatusCompleted, domain.TaskPlanStatusFailed, domain.TaskPlanStatusEscalated:
		return true
	default:
		return false
	}
}

func validTaskPlanStepStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.TaskPlanStepStatusPending, domain.TaskPlanStepStatusReady, domain.TaskPlanStepStatusRunning,
		domain.TaskPlanStepStatusBlocked, domain.TaskPlanStepStatusDone,
		domain.TaskPlanStepStatusFailed, domain.TaskPlanStepStatusSkipped:
		return true
	default:
		return false
	}
}

func isTerminalTaskPlanStepStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domain.TaskPlanStepStatusDone, domain.TaskPlanStepStatusFailed, domain.TaskPlanStepStatusSkipped:
		return true
	default:
		return false
	}
}

func jsonOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func nextActionFromSteps(steps []domain.TaskPlanStep) string {
	for _, step := range steps {
		switch step.Status {
		case domain.TaskPlanStepStatusPending, domain.TaskPlanStepStatusReady, domain.TaskPlanStepStatusRunning:
			return step.Title
		case domain.TaskPlanStepStatusBlocked:
			if step.Blocker != "" {
				return "resolve blocker: " + step.Blocker
			}
			return "resolve blocker for " + step.Title
		}
	}
	return "closed"
}

func firstPlanBlocker(steps []domain.TaskPlanStep) string {
	for _, step := range steps {
		if step.Status == domain.TaskPlanStepStatusBlocked && strings.TrimSpace(step.Blocker) != "" {
			return strings.TrimSpace(step.Blocker)
		}
	}
	return ""
}

func statusFromPlanSteps(steps []domain.TaskPlanStep, fallback string) string {
	if len(steps) == 0 {
		return fallback
	}
	allTerminal := true
	hasFailed := false
	hasBlocked := false
	hasRunning := false
	for _, step := range steps {
		switch step.Status {
		case domain.TaskPlanStepStatusFailed:
			hasFailed = true
		case domain.TaskPlanStepStatusBlocked:
			hasBlocked = true
			allTerminal = false
		case domain.TaskPlanStepStatusRunning:
			hasRunning = true
			allTerminal = false
		default:
			if !isTerminalTaskPlanStepStatus(step.Status) {
				allTerminal = false
			}
		}
	}
	switch {
	case hasFailed:
		return domain.TaskPlanStatusFailed
	case hasBlocked:
		return domain.TaskPlanStatusBlocked
	case allTerminal:
		return domain.TaskPlanStatusCompleted
	case hasRunning:
		return domain.TaskPlanStatusActive
	default:
		return domain.TaskPlanStatusActive
	}
}

func applyPlanCompletion(plan *domain.TaskPlan) {
	if plan == nil {
		return
	}
	switch plan.Status {
	case domain.TaskPlanStatusCompleted, domain.TaskPlanStatusFailed, domain.TaskPlanStatusEscalated:
		if plan.CompletedAt == nil {
			now := time.Now().UTC()
			plan.CompletedAt = &now
		}
	default:
		plan.CompletedAt = nil
	}
}

func taskPlanActionPayload(plan domain.TaskPlan) json.RawMessage {
	steps := make([]map[string]any, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, map[string]any{
			"id":               step.ID.String(),
			"step_key":         step.StepKey,
			"title":            step.Title,
			"status":           step.Status,
			"tool_name":        step.ToolName,
			"capability":       step.Capability,
			"expected_outcome": step.ExpectedOutcome,
			"postcondition":    step.Postcondition,
			"sort_order":       step.SortOrder,
			"depends_on":       json.RawMessage(step.DependsOnJSON),
			"attempt_count":    step.AttemptCount,
			"completed_at":     formatOptionalTime(step.CompletedAt),
		})
	}
	return marshalOrEmpty("task_plan_action", map[string]any{
		"objective":   plan.Objective,
		"status":      plan.Status,
		"strategy":    plan.Strategy,
		"next_action": plan.NextAction,
		"blocker":     plan.Blocker,
		"steps":       steps,
	})
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
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
		"nexus_request_id": func() string {
			if result.NexusRequestID != nil {
				return result.NexusRequestID.String()
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

func defaultExecutionIdempotencyKey(taskID uuid.UUID, nexusRequestID *uuid.UUID) string {
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

func executionActorType(t domain.Task) string {
	if executionActorID(t) == CompanionRequesterID {
		return "agent"
	}
	return "human"
}

func executionOnBehalfOf(t domain.Task) string {
	if actor := strings.TrimSpace(t.CreatedBy); actor != "" && actor != CompanionRequesterID {
		return actor
	}
	return ""
}

func (u *Usecases) refreshNexusSnapshot(ctx context.Context, taskID uuid.UUID, origin string) (*domain.TaskNexusSyncState, error) {
	var prevState *domain.TaskNexusSyncState
	currentState, err := u.repo.GetNexusSyncState(ctx, taskID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return nil, err
	}

	nexusRequestID, err := u.latestNexusRequestIDForTask(ctx, taskID, prevState)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskNexusSyncState{
		TaskID:         taskID,
		NexusRequestID: nexusRequestID,
		LastCheckedAt:  now,
		NextCheckAt:    nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastNexusStatus = prevState.LastNexusStatus
		nextState.LastNexusHTTPStatus = prevState.LastNexusHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, statusCode, getErr := u.nexus.GetRequest(ctx, nexusRequestID.String())
	if getErr != nil {
		nextState.LastNexusHTTPStatus = statusCode
		nextState.LastError = getErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upsertErr := u.repo.UpsertNexusSyncState(ctx, nextState)
		if upsertErr != nil {
			return nil, upsertErr
		}
		return &stateOut, fmt.Errorf("nexus get request: %w", getErr)
	}

	nextState.LastNexusHTTPStatus = statusCode
	nextState.LastNexusStatus = normalizeNexusStatus(sum.Status)
	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextNexusSyncAt(now, u.nexusSyncIntervalOrDefault(), 0)

	stateOut, upsertErr := u.repo.UpsertNexusSyncState(ctx, nextState)
	if upsertErr != nil {
		return nil, upsertErr
	}
	if nexusSnapshotChanged(prevState, stateOut) {
		u.persistNexusSyncAction(ctx, taskID, nexusRequestID, origin, prevState, stateOut, "", "", "")
	}
	return &stateOut, nil
}

func (u *Usecases) runTaskExecution(ctx context.Context, t domain.Task, plan domain.TaskExecutionPlan, prevState *domain.TaskExecutionState, startEvent string) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput

	t, err := u.applyTaskEvent(ctx, t, startEvent)
	if err != nil {
		return out, err
	}

	var nexusRequestID *uuid.UUID
	if syncState, syncErr := u.repo.GetNexusSyncState(ctx, t.ID); syncErr == nil && syncState.NexusRequestID != uuid.Nil {
		nexusRequestID = &syncState.NexusRequestID
	}
	idempotencyKey := plan.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = defaultExecutionIdempotencyKey(t.ID, nexusRequestID)
	}

	result, execErr := u.executor.Execute(ctx, connectordomain.ExecutionSpec{
		ConnectorID:        plan.ConnectorID,
		OrgID:              t.OrgID,
		ActorID:            executionActorID(t),
		ActorType:          executionActorType(t),
		CompanionPrincipal: CompanionRequesterID,
		OnBehalfOf:         executionOnBehalfOf(t),
		ServicePrincipal:   true,
		ProductSurface:     "companion",
		Operation:          plan.Operation,
		Payload:            plan.Payload,
		IdempotencyKey:     idempotencyKey,
		TaskID:             &t.ID,
		NexusRequestID:     nexusRequestID,
	})
	if execErr != nil {
		result = connectordomain.ExecutionResult{
			ID:             uuid.New(),
			ConnectorID:    plan.ConnectorID,
			OrgID:          t.OrgID,
			ActorID:        executionActorID(t),
			Operation:      plan.Operation,
			Status:         connectordomain.ExecFailure,
			Payload:        plan.Payload,
			ResultJSON:     json.RawMessage(`{}`),
			ErrorMessage:   execErr.Error(),
			Retryable:      true,
			IdempotencyKey: idempotencyKey,
			TaskID:         &t.ID,
			NexusRequestID: nexusRequestID,
			CreatedAt:      time.Now().UTC(),
		}
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	u.reportExecutionToNexus(ctx, nexusRequestID, result)

	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         t.ID,
		ActionType:     TaskActionExecuteConnector,
		Payload:        buildConnectorExecutionPayload(result),
		NexusRequestID: nexusRequestID,
		ErrorMessage:   result.ErrorMessage,
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
		TaskID:         t.ID,
		ActionType:     TaskActionVerifyExecution,
		Payload:        buildVerificationPayload(result, verification),
		NexusRequestID: nexusRequestID,
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

	t.NexusStatus = normalizeNexusStatus(t.NexusStatus)
	out.Task = t
	out.Plan = plan
	out.Execution = result
	out.ExecutionState = executionState
	u.syncTaskMemory(ctx, t.ID, "execution")
	return out, nil
}

func (u *Usecases) reportExecutionToNexus(ctx context.Context, nexusRequestID *uuid.UUID, result connectordomain.ExecutionResult) {
	if u.nexus == nil || nexusRequestID == nil || *nexusRequestID == uuid.Nil {
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
	status, err := u.nexus.ReportResult(ctx, nexusRequestID.String(), success, resultPayload, result.DurationMS, result.ErrorMessage)
	if err != nil || status >= http.StatusBadRequest {
		slog.Warn("report execution to nexus failed",
			"nexus_request_id", nexusRequestID.String(),
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

	var nexusRequestID string
	if t.Status == domain.TaskStatusWaitingForApproval {
		syncedTask, state, syncErr := u.syncTaskWithNexus(ctx, t, "execute")
		if state != nil {
			syncedTask.NexusStatus = state.LastNexusStatus
			syncedTask.NexusLastCheckedAt = &state.LastCheckedAt
			syncedTask.NexusSyncError = state.LastError
			nexusRequestID = state.NexusRequestID.String()
		}
		if syncErr != nil {
			return out, syncErr
		}
		t = syncedTask
	}

	if !isApprovedNexusStatus(t.NexusStatus) {
		return out, u.nexusBlockedError(nexusRequestID, t.NexusStatus, "execute")
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

	snapshot, snapshotErr := u.refreshNexusSnapshot(ctx, taskID, "retry")
	if snapshotErr != nil {
		return out, snapshotErr
	}
	t.NexusStatus = snapshot.LastNexusStatus
	t.NexusLastCheckedAt = &snapshot.LastCheckedAt
	t.NexusSyncError = snapshot.LastError
	if !isApprovedNexusStatus(snapshot.LastNexusStatus) {
		return out, u.nexusBlockedError(snapshot.NexusRequestID.String(), snapshot.LastNexusStatus, "retry")
	}

	payload := marshalOrEmpty("retry_execution_action", map[string]any{
		"retry_count_before":    state.RetryCount,
		"last_execution_status": state.LastExecutionStatus,
		"last_error":            state.LastError,
	})
	nexusRequestID := snapshot.NexusRequestID
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:         taskID,
		ActionType:     TaskActionRetryExecution,
		Payload:        payload,
		NexusRequestID: &nexusRequestID,
	}); insertErr != nil {
		slog.Warn("companion retry execution action failed", "task_id", taskID.String(), "error", insertErr)
	}

	return u.runTaskExecution(ctx, t, plan, &state, evRetryExecution)
}

// SyncTaskNexus consulta Nexus y aplica transición si el request ya resolvió (tareas en espera).
func (u *Usecases) SyncTaskNexus(ctx context.Context, taskID uuid.UUID) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, state, err := u.syncTaskWithNexus(ctx, t, "manual")
	if state != nil {
		t.NexusStatus = state.LastNexusStatus
		t.NexusLastCheckedAt = &state.LastCheckedAt
		t.NexusSyncError = state.LastError
	}
	if err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

// SyncPendingNexusTasks sincroniza un lote de tareas en waiting_for_approval.
func (u *Usecases) SyncPendingNexusTasks(ctx context.Context, limit int) {
	if limit <= 0 {
		limit = 50
	}
	list, err := u.repo.ListTasksPendingNexusSync(ctx, time.Now().UTC(), limit)
	if err != nil {
		slog.Error("companion sync list waiting tasks", "error", err)
		return
	}
	for _, item := range list {
		if _, _, sErr := u.syncTaskWithNexus(ctx, item, "loop"); sErr != nil {
			slog.Warn("companion sync task failed", "task_id", item.ID.String(), "error", sErr)
		}
	}
}

// RunNexusSyncLoop ejecuta SyncPendingNexusTasks periódicamente hasta que ctx termina.
func (u *Usecases) RunNexusSyncLoop(ctx context.Context, interval time.Duration, batch int) {
	if batch <= 0 {
		return
	}
	worker.RunPeriodic(ctx, interval, "nexus-sync", func(c context.Context) {
		runCtx, cancel := context.WithTimeout(c, 2*time.Minute)
		u.SyncPendingNexusTasks(runCtx, batch)
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

// nexusBlockedError devuelve un error estructurado cuando una operación de
// task se bloquea porque la nexus en Nexus no está aprobada.
func (u *Usecases) nexusBlockedError(nexusRequestID, nexusStatus, reason string) error {
	return &NexusBlockedError{
		NexusRequestID: nexusRequestID,
		NexusStatus:    nexusStatus,
		Reason:         reason,
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
