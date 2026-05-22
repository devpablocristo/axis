package domain

import (
	"time"

	"github.com/google/uuid"
)

type RequesterType string

const (
	RequesterTypeAgent   RequesterType = "agent"
	RequesterTypeService RequesterType = "service"
	RequesterTypeHuman   RequesterType = "human"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionRequireApproval Decision = "require_approval"
)

type RequestStatus string

const (
	StatusPending         RequestStatus = "pending"
	StatusEvaluated       RequestStatus = "evaluated"
	StatusAllowed         RequestStatus = "allowed"
	StatusDenied          RequestStatus = "denied"
	StatusPendingApproval RequestStatus = "pending_approval"
	StatusApproved        RequestStatus = "approved"
	StatusRejected        RequestStatus = "rejected"
	StatusExpired         RequestStatus = "expired"
	StatusExecuted        RequestStatus = "executed"
	StatusFailed          RequestStatus = "failed"
	StatusCancelled       RequestStatus = "cancelled"
)

// Attestation es la prueba verificable de qué ejecutó el sistema target.
type Attestation struct {
	ID           uuid.UUID
	RequestID    uuid.UUID
	Status       string         // success, failure, partial
	ProviderRefs map[string]any // refs externas del ejecutor (tx_id, deploy_id, etc.)
	Signature    string         // firma del attester (JWS o hash)
	Attester     string         // identidad del attester (pep:treasury_gateway, etc.)
	Metadata     map[string]any // contexto adicional
	CreatedAt    time.Time
	// Verified indica si la firma fue criptográficamente verificada por el
	// runtime al persistirse. Cuando GOVERNANCE_ATTESTATION_VERIFIER=none (o
	// no está cableado) se persiste false con VerificationError explicando el
	// motivo, así el caller puede distinguir attestations no verificadas de
	// verificadas válidamente.
	Verified          bool
	VerificationError string
}

type Request struct {
	ID              uuid.UUID
	OrgID           *string // tenant-owned requests require org_id outside dev/test fixtures.
	IdempotencyKey  *string
	RequesterType   RequesterType
	RequesterID     string
	RequesterName   string
	ActionType      string
	TargetSystem    string
	TargetResource  string
	ActionBinding   map[string]any
	BindingHash     string
	Params          map[string]any
	Reason          string
	Context         string
	RiskLevel       RiskLevel
	Decision        Decision
	DecisionReason  string
	PolicyID        *uuid.UUID
	Status          RequestStatus
	ApprovalID      *uuid.UUID
	ExecutionResult map[string]any
	ErrorMessage    string
	AISummary       string
	AIDegraded      bool
	EvaluatedAt     *time.Time
	DecidedAt       *time.Time
	ExecutedAt      *time.Time
	ExpiresAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
