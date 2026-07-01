package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TaskStatus valores persistidos (CHECK en SQL).
const (
	TaskStatusNew                = "new"
	TaskStatusInvestigating      = "investigating"
	TaskStatusProposing          = "proposing"
	TaskStatusWaitingForInput    = "waiting_for_input"
	TaskStatusWaitingForApproval = "waiting_for_approval"
	TaskStatusExecuting          = "executing"
	TaskStatusVerifying          = "verifying"
	TaskStatusDone               = "done"
	TaskStatusFailed             = "failed"
	TaskStatusEscalated          = "escalated"
)

// Task entidad de dominio.
type Task struct {
	ID                 uuid.UUID
	OrgID              string
	Title              string
	Goal               string
	Status             string
	Priority           string
	CreatedBy          string
	AssignedTo         string
	Channel            string
	Summary            string
	ContextJSON        json.RawMessage
	NexusStatus        string
	NexusLastCheckedAt *time.Time
	NexusSyncError     string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ClosedAt           *time.Time
}

// TaskMessage mensaje en el hilo de una tarea.
type TaskMessage struct {
	ID         uuid.UUID
	TaskID     uuid.UUID
	AuthorType string
	AuthorID   string
	Body       string
	Metadata   json.RawMessage
	CreatedAt  time.Time
}

// TaskAction acción sobre una tarea (p. ej. propose → Nexus).
type TaskAction struct {
	ID             uuid.UUID
	TaskID         uuid.UUID
	ActionType     string
	Payload        json.RawMessage
	NexusRequestID *uuid.UUID
	ErrorMessage   string
	CreatedAt      time.Time
}

// TaskArtifact adjunto mínimo.
type TaskArtifact struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	Kind      string
	URI       string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// TaskNexusSyncState snapshot persistido del último estado conocido en Nexus.
type TaskNexusSyncState struct {
	TaskID              uuid.UUID
	NexusRequestID      uuid.UUID
	LastNexusStatus     string
	LastNexusHTTPStatus int
	LastCheckedAt       time.Time
	LastError           string
	ConsecutiveFailures int
	NextCheckAt         time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const (
	TaskPlanStatusDraft     = "draft"
	TaskPlanStatusActive    = "active"
	TaskPlanStatusBlocked   = "blocked"
	TaskPlanStatusCompleted = "completed"
	TaskPlanStatusFailed    = "failed"
	TaskPlanStatusEscalated = "escalated"

	TaskPlanStepStatusPending = "pending"
	TaskPlanStepStatusReady   = "ready"
	TaskPlanStepStatusRunning = "running"
	TaskPlanStepStatusBlocked = "blocked"
	TaskPlanStepStatusDone    = "done"
	TaskPlanStepStatusFailed  = "failed"
	TaskPlanStepStatusSkipped = "skipped"
)

// TaskPlan es el plan cognitivo durable de una task. Modela objetivo, pasos,
// checkpoints y bloqueos; no ejecuta side effects externos por si mismo.
type TaskPlan struct {
	TaskID          uuid.UUID
	OrgID           string
	Objective       string
	Status          string
	Strategy        string
	AssumptionsJSON json.RawMessage
	ConstraintsJSON json.RawMessage
	CheckpointJSON  json.RawMessage
	NextAction      string
	Blocker         string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
	Steps           []TaskPlanStep
}

type TaskPlanStep struct {
	ID              uuid.UUID
	TaskID          uuid.UUID
	OrgID           string
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
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
}

type TaskExecutionGraphEvent struct {
	ID                uuid.UUID
	OrgID             string
	TaskID            uuid.UUID
	StepID            *uuid.UUID
	EventType         string
	Status            string
	AgentID           string
	CapabilityID      string
	CapabilityVersion string
	JobID             *uuid.UUID
	NexusDecisionID   string
	PayloadJSON       json.RawMessage
	CreatedAt         time.Time
}
