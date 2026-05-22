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
	ID                  uuid.UUID
	OrgID               string
	Title               string
	Goal                string
	Status              string
	Priority            string
	CreatedBy           string
	AssignedTo          string
	Channel             string
	Summary             string
	ContextJSON         json.RawMessage
	GovernanceStatus        string
	GovernanceLastCheckedAt *time.Time
	GovernanceSyncError     string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ClosedAt            *time.Time
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

// TaskAction acción sobre una tarea (p. ej. propose → Governance).
type TaskAction struct {
	ID              uuid.UUID
	TaskID          uuid.UUID
	ActionType      string
	Payload         json.RawMessage
	GovernanceRequestID *uuid.UUID
	ErrorMessage    string
	CreatedAt       time.Time
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

// TaskGovernanceSyncState snapshot persistido del último estado conocido en Governance.
type TaskGovernanceSyncState struct {
	TaskID               uuid.UUID
	GovernanceRequestID      uuid.UUID
	LastGovernanceStatus     string
	LastGovernanceHTTPStatus int
	LastCheckedAt        time.Time
	LastError            string
	ConsecutiveFailures  int
	NextCheckAt          time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// TaskExecutionPlan snapshot persistido del plan de ejecución manual de una tarea.
type TaskExecutionPlan struct {
	TaskID         uuid.UUID
	ConnectorID    uuid.UUID
	Operation      string
	Payload        json.RawMessage
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const (
	VerificationStatusVerified = "verified"
	VerificationStatusFailed   = "failed"
)

// TaskVerificationResult representa el resultado persistido de la verificación posterior a ejecutar.
type TaskVerificationResult struct {
	Status    string
	Summary   string
	CheckedAt time.Time
	Details   json.RawMessage
}

// TaskExecutionState resume el último intento de ejecución de una tarea.
type TaskExecutionState struct {
	TaskID              uuid.UUID
	LastExecutionID     uuid.UUID
	LastExecutionStatus string
	Retryable           bool
	RetryCount          int
	LastError           string
	LastAttemptedAt     time.Time
	VerificationResult  TaskVerificationResult
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
