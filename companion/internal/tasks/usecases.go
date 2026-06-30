package tasks

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

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
	Workspace      json.RawMessage // contexto operativo de pantalla; gana sobre handoff.workspace
	Messages       []domain.TaskMessage
	TaskID         *uuid.UUID // opcional: vincula el trace a una task
	ProductSurface string     // opcional: "companion" (default) | "ponti" | "pymes" — afecta routing
	TenantID       string     // requerido si EmployeeID resuelve un Virtual Employee
	EmployeeID     string     // opcional: Virtual Employee persistente que toma ownership de la ejecución
	AgentID        string     // opcional legacy: Agent tecnico persistente
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
	EmployeeID string
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
