package virployees

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/google/uuid"
)

const (
	OrchestrationModeDisabled = "disabled"
	OrchestrationModeShadow   = "shadow"
	OrchestrationModeActive   = "active"

	AssistStatusPlanning     = "planning"
	AssistStatusConsulting   = "consulting"
	AssistStatusSynthesizing = "synthesizing"
	AssistStatusNeedsHuman   = "needs_human"

	JobKindSpecialistConsult      = "assist.specialist.consult"
	JobKindOrchestrationReconcile = "assist.orchestration.reconcile"
	JobKindOrchestrationSynthesis = "assist.orchestration.synthesize"
)

type AssistCase struct {
	ID                    uuid.UUID  `json:"id"`
	TenantID              string     `json:"tenant_id"`
	ProductSurface        string     `json:"product_surface"`
	AssistType            string     `json:"assist_type"`
	SubjectID             string     `json:"subject_id"`
	EntrypointVirployeeID uuid.UUID  `json:"entrypoint_virployee_id"`
	OwnerVirployeeID      uuid.UUID  `json:"owner_virployee_id"`
	Status                string     `json:"status"`
	Version               int64      `json:"version"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	ClosedAt              *time.Time `json:"closed_at,omitempty"`
}

type OrchestrationPolicy struct {
	ID                          uuid.UUID      `json:"id"`
	TenantID                    string         `json:"tenant_id"`
	ProductSurface              string         `json:"product_surface"`
	AssistType                  string         `json:"assist_type"`
	EntrypointVirployeeID       uuid.UUID      `json:"entrypoint_virployee_id"`
	Mode                        string         `json:"mode"`
	SelectorCapabilityID        uuid.UUID      `json:"selector_capability_id"`
	SynthesisCapabilityID       uuid.UUID      `json:"synthesis_capability_id"`
	OutputSchema                map[string]any `json:"output_schema"`
	MaxSpecialists              int            `json:"max_specialists"`
	MaxDepth                    int            `json:"max_depth"`
	ConsultationTimeoutSeconds  int            `json:"consultation_timeout_seconds"`
	OrchestrationTimeoutSeconds int            `json:"orchestration_timeout_seconds"`
	Version                     int64          `json:"version"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

type SpecialistRoute struct {
	ID                    uuid.UUID `json:"id"`
	TenantID              string    `json:"tenant_id"`
	ProductSurface        string    `json:"product_surface"`
	AssistType            string    `json:"assist_type"`
	EntrypointVirployeeID uuid.UUID `json:"entrypoint_virployee_id"`
	SpecialtyCode         string    `json:"specialty_code"`
	TargetVirployeeID     uuid.UUID `json:"target_virployee_id"`
	CapabilityID          uuid.UUID `json:"capability_id"`
	RequirementMode       string    `json:"requirement_mode"`
	Enabled               bool      `json:"enabled"`
	Version               int64     `json:"version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type OrchestrationPlan struct {
	ID                     uuid.UUID       `json:"id"`
	TenantID               string          `json:"tenant_id"`
	CaseID                 uuid.UUID       `json:"case_id"`
	RootRunID              uuid.UUID       `json:"root_run_id"`
	PolicyID               uuid.UUID       `json:"policy_id"`
	PolicyVersion          int64           `json:"policy_version"`
	OutputSchema           map[string]any  `json:"-"`
	ResponsibleVirployeeID uuid.UUID       `json:"responsible_virployee_id"`
	Decision               string          `json:"decision"`
	Status                 string          `json:"status"`
	Proposal               json.RawMessage `json:"-"`
	PlanHash               string          `json:"plan_hash"`
	Model                  string          `json:"model,omitempty"`
	PromptVersion          string          `json:"prompt_version,omitempty"`
	RequestedCount         int             `json:"requested_count"`
	CompletedCount         int             `json:"completed_count"`
	FailedCount            int             `json:"failed_count"`
	DeadlineAt             time.Time       `json:"deadline_at"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
	CompletedAt            *time.Time      `json:"completed_at,omitempty"`
}

type SpecialistConsultation struct {
	ID                uuid.UUID       `json:"id"`
	TenantID          string          `json:"tenant_id"`
	PlanID            uuid.UUID       `json:"plan_id"`
	RootRunID         uuid.UUID       `json:"root_run_id"`
	CaseID            uuid.UUID       `json:"case_id"`
	SpecialtyCode     string          `json:"specialty_code"`
	TargetVirployeeID uuid.UUID       `json:"target_virployee_id"`
	CapabilityID      uuid.UUID       `json:"capability_id"`
	Requirement       string          `json:"requirement"`
	Status            string          `json:"status"`
	FocusJSON         json.RawMessage `json:"-"`
	FocusHash         string          `json:"focus_hash"`
	Output            json.RawMessage `json:"-"`
	OutputHash        string          `json:"output_hash,omitempty"`
	Model             string          `json:"model,omitempty"`
	PromptVersion     string          `json:"prompt_version,omitempty"`
	ErrorCode         string          `json:"error_code,omitempty"`
	DurationMS        int64           `json:"duration_ms"`
	StartedAt         *time.Time      `json:"started_at,omitempty"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type Handoff struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        string     `json:"tenant_id"`
	CaseID          uuid.UUID  `json:"case_id"`
	SourceRunID     *uuid.UUID `json:"source_run_id,omitempty"`
	FromVirployeeID uuid.UUID  `json:"from_virployee_id"`
	ToVirployeeID   uuid.UUID  `json:"to_virployee_id"`
	ReasonCode      string     `json:"reason_code"`
	Note            string     `json:"-"`
	NoteHash        string     `json:"note_hash,omitempty"`
	Status          string     `json:"status"`
	RequestedBy     string     `json:"requested_by"`
	DecidedBy       string     `json:"decided_by,omitempty"`
	DecisionNote    string     `json:"-"`
	Version         int64      `json:"version"`
	ExpiresAt       time.Time  `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
}

type HumanReview struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       string     `json:"tenant_id"`
	CaseID         uuid.UUID  `json:"case_id"`
	RootRunID      uuid.UUID  `json:"root_run_id"`
	HandoffID      *uuid.UUID `json:"handoff_id,omitempty"`
	ReasonCode     string     `json:"reason_code"`
	Urgency        string     `json:"urgency"`
	Status         string     `json:"status"`
	ReviewerUserID string     `json:"reviewer_user_id,omitempty"`
	Outcome        string     `json:"outcome,omitempty"`
	Note           string     `json:"-"`
	NoteHash       string     `json:"note_hash,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type OrchestrationSummary struct {
	State       string                    `json:"state"`
	Decision    string                    `json:"decision,omitempty"`
	Requested   int                       `json:"requested"`
	Completed   int                       `json:"completed"`
	Failed      int                       `json:"failed"`
	Specialists []SpecialistSummary       `json:"specialists,omitempty"`
	Limitations []OrchestrationLimitation `json:"limitations,omitempty"`
}

type SpecialistSummary struct {
	SpecialtyCode string `json:"specialty_code"`
	Requirement   string `json:"requirement"`
	Status        string `json:"status"`
}

type OrchestrationLimitation struct {
	Code          string `json:"code"`
	SpecialtyCode string `json:"specialty_code,omitempty"`
}

type OrchestrationDecision struct {
	Decision      string                 `json:"decision"`
	DirectOutput  json.RawMessage        `json:"direct_output,omitempty"`
	Consultations []ConsultationProposal `json:"consultations,omitempty"`
	Escalation    *EscalationProposal    `json:"escalation,omitempty"`
}

type ConsultationProposal struct {
	SpecialtyCode string            `json:"specialty_code"`
	Requirement   string            `json:"requirement"`
	Focus         string            `json:"focus"`
	ReasonCodes   []string          `json:"reason_codes"`
	EvidenceRefs  []json.RawMessage `json:"evidence_refs"`
}

type EscalationProposal struct {
	ReasonCode string `json:"reason_code"`
	Urgency    string `json:"urgency"`
}

type SpecialistOpinion struct {
	SpecialtyCode       string              `json:"specialty_code"`
	Opinion             string              `json:"opinion"`
	Findings            []OpinionFinding    `json:"findings"`
	Limitations         []string            `json:"limitations"`
	RecommendationCodes []string            `json:"recommendation_codes"`
	HumanReview         *EscalationProposal `json:"human_review,omitempty"`
}

type OpinionFinding struct {
	Statement    string            `json:"statement"`
	EvidenceRefs []json.RawMessage `json:"evidence_refs"`
}

type CoordinationQueuePort interface {
	EnqueueConsultation(context.Context, SpecialistConsultation, time.Duration) error
	EnqueueReconcile(context.Context, OrchestrationPlan, string) error
	EnqueueSynthesis(context.Context, OrchestrationPlan) error
}

type CoordinationRepositoryPort interface {
	GetAssistCase(context.Context, string, uuid.UUID) (AssistCase, error)
	ListAssistCases(context.Context, string, string, int) ([]AssistCase, error)
	FindOrchestrationPolicy(context.Context, string, string, string, uuid.UUID) (OrchestrationPolicy, error)
	ListOrchestrationPolicies(context.Context, string) ([]OrchestrationPolicy, error)
	UpsertOrchestrationPolicy(context.Context, string, OrchestrationPolicy) (OrchestrationPolicy, error)
	ListSpecialistRoutes(context.Context, string, string, string, uuid.UUID, bool) ([]SpecialistRoute, error)
	UpsertSpecialistRoute(context.Context, string, SpecialistRoute) (SpecialistRoute, error)
	CreateOrchestrationPlan(context.Context, AssistRun, OrchestrationPolicy, OrchestrationDecision, json.RawMessage, string, string, string, []SpecialistConsultation) (OrchestrationPlan, []SpecialistConsultation, error)
	GetOrchestrationPlan(context.Context, string, uuid.UUID) (OrchestrationPlan, error)
	GetOrchestrationPlanByRun(context.Context, string, uuid.UUID) (OrchestrationPlan, error)
	ListRecoverableOrchestrationPlans(context.Context, int) ([]OrchestrationPlan, error)
	ClaimSynthesis(context.Context, string, uuid.UUID) (OrchestrationPlan, bool, error)
	ClaimConsultation(context.Context, string, uuid.UUID) (SpecialistConsultation, bool, error)
	GetConsultation(context.Context, string, uuid.UUID) (SpecialistConsultation, error)
	ListConsultations(context.Context, string, uuid.UUID) ([]SpecialistConsultation, error)
	ReleaseConsultation(context.Context, string, uuid.UUID, string) error
	CompleteConsultation(context.Context, string, uuid.UUID, string, json.RawMessage, string, string, string, string, int64) (SpecialistConsultation, error)
	RefreshPlanCounts(context.Context, string, uuid.UUID) (OrchestrationPlan, error)
	SetPlanStatus(context.Context, string, uuid.UUID, string) error
	TimeoutConsultations(context.Context, string, uuid.UUID) error
	CreateHumanReview(context.Context, string, uuid.UUID, uuid.UUID, string, string) (HumanReview, error)
	GetHumanReview(context.Context, string, uuid.UUID) (HumanReview, error)
	ListHumanReviews(context.Context, string, string) ([]HumanReview, error)
	ClaimHumanReview(context.Context, string, uuid.UUID, string) (HumanReview, error)
	ResolveHumanReview(context.Context, string, uuid.UUID, string, ResolveReviewInput) (HumanReview, error)
	CreateHandoff(context.Context, string, uuid.UUID, string, CreateHandoffInput) (Handoff, error)
	GetHandoff(context.Context, string, uuid.UUID) (Handoff, error)
	ListHandoffs(context.Context, string, string, int) ([]Handoff, error)
	DecideHandoff(context.Context, string, uuid.UUID, string, string, DecideHandoffInput) (Handoff, error)
	CancelHandoff(context.Context, string, uuid.UUID, string, int64) (Handoff, error)
	ExpireHandoffs(context.Context, int) ([]Handoff, error)
	ActiveRunForCase(context.Context, string, uuid.UUID) (AssistRun, error)
}

type ArtifactCorpusReaderPort interface {
	Load(context.Context, artifacts.Scope, string, int) ([]artifacts.ContentPart, error)
}

type CoordinationActor struct {
	ID   string
	Role string
}

type CreateHandoffInput struct {
	CaseID      uuid.UUID
	SourceRunID *uuid.UUID
	ToID        uuid.UUID
	ReasonCode  string
	Note        string
}

type DecideHandoffInput struct {
	Version int64
	Note    string
}

type ResolveReviewInput struct {
	Outcome   string
	Note      string
	HandoffID *uuid.UUID
}

type CoordinationRecoveryResult struct {
	Consultations int `json:"consultations"`
	Reconciles    int `json:"reconciles"`
	Syntheses     int `json:"syntheses"`
}
