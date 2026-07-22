package operations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type FleetStatus string

const (
	FleetReady    FleetStatus = "ready"
	FleetDegraded FleetStatus = "degraded"
	FleetBlocked  FleetStatus = "blocked"
	FleetInactive FleetStatus = "inactive"
	FleetUnknown  FleetStatus = "unknown"
)

type FleetMember struct {
	VirployeeID         uuid.UUID   `json:"virployee_id"`
	Name                string      `json:"name"`
	Status              FleetStatus `json:"status"`
	JobRoleID           uuid.UUID   `json:"job_role_id"`
	JobRoleName         string      `json:"job_role_name"`
	ProfileTemplateID   uuid.UUID   `json:"profile_template_id"`
	Autonomy            string      `json:"autonomy"`
	GroundingMode       string      `json:"grounding_mode"`
	CapabilityCount     int         `json:"capability_count"`
	InvalidCapabilities int         `json:"invalid_capabilities"`
	KnowledgeBaseCount  int         `json:"knowledge_base_count"`
	PoolCount           int         `json:"pool_count"`
	MaxActiveSubjects   int         `json:"max_active_subjects"`
	ActiveSubjects      int         `json:"active_subjects"`
	PendingJobs         int         `json:"pending_jobs"`
	RecentErrors        int         `json:"recent_errors"`
	LastRunAt           *time.Time  `json:"last_run_at,omitempty"`
	LastSuccessAt       *time.Time  `json:"last_success_at,omitempty"`
	AuthorityState      string      `json:"authority_state"`
}

type Overview struct {
	Service            string         `json:"service"`
	Status             string         `json:"status"`
	Fleet              map[string]int `json:"fleet"`
	Jobs               map[string]int `json:"jobs"`
	Outbox             map[string]int `json:"outbox"`
	OpenFindings       map[string]int `json:"open_findings"`
	OldestQueuedJobAge int64          `json:"oldest_queued_job_age_seconds"`
	OldestOutboxAge    int64          `json:"oldest_outbox_age_seconds"`
	GeneratedAt        time.Time      `json:"generated_at"`
}

type ReconciliationMode string

const (
	ModeDetect     ReconciliationMode = "detect"
	ModeSafeRepair ReconciliationMode = "safe_repair"
)

type CreateReconciliationInput struct {
	Mode           string `json:"mode"`
	TriggerType    string `json:"-"`
	IdempotencyKey string `json:"-"`
}

type ReconciliationRun struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       string     `json:"tenant_id"`
	ProductSurface string     `json:"product_surface"`
	Mode           string     `json:"mode"`
	TriggerType    string     `json:"trigger"`
	Status         string     `json:"status"`
	ActorID        string     `json:"actor_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	FindingsCount  int        `json:"findings_count"`
	RepairedCount  int        `json:"repaired_count"`
	ReportHash     string     `json:"report_hash"`
	ErrorCode      string     `json:"error_code,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Findings       []Finding  `json:"findings,omitempty"`
}

type Finding struct {
	ID           uuid.UUID       `json:"id"`
	RunID        uuid.UUID       `json:"run_id"`
	TenantID     string          `json:"tenant_id"`
	FindingType  string          `json:"finding_type"`
	Severity     string          `json:"severity"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Fingerprint  string          `json:"fingerprint"`
	ExpectedHash string          `json:"expected_hash,omitempty"`
	ObservedHash string          `json:"observed_hash,omitempty"`
	RepairClass  string          `json:"repair_class"`
	Repaired     bool            `json:"repaired"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
}

type OperationalJob struct {
	Service          string          `json:"service"`
	ID               uuid.UUID       `json:"id"`
	ProductSurface   string          `json:"product_surface"`
	Kind             string          `json:"kind"`
	DedupeKey        string          `json:"-"`
	DedupeKeyHash    string          `json:"dedupe_key_hash"`
	Status           string          `json:"status"`
	EffectClass      string          `json:"effect_class"`
	ReplayPolicy     string          `json:"replay_policy"`
	Attempts         int             `json:"attempts"`
	MaxAttempts      int             `json:"max_attempts"`
	RunAfter         time.Time       `json:"run_after"`
	LeaseUntil       *time.Time      `json:"lease_until,omitempty"`
	DeadlineAt       *time.Time      `json:"deadline_at,omitempty"`
	LastErrorCode    string          `json:"last_error_code,omitempty"`
	CancellationCode string          `json:"cancellation_code,omitempty"`
	Evidence         json.RawMessage `json:"evidence"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
}

type OutboxMessage struct {
	ID            uuid.UUID  `json:"id"`
	AggregateType string     `json:"aggregate_type"`
	AggregateID   uuid.UUID  `json:"aggregate_id"`
	Kind          string     `json:"kind"`
	DedupeKey     string     `json:"-"`
	DedupeKeyHash string     `json:"dedupe_key_hash"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	AvailableAt   time.Time  `json:"available_at"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	LastErrorCode string     `json:"last_error_code,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
}

type WorkerControl struct {
	ID                     uuid.UUID  `json:"id"`
	TenantID               string     `json:"tenant_id"`
	ProductSurface         string     `json:"product_surface"`
	JobKind                string     `json:"job_kind"`
	State                  string     `json:"state"`
	Version                int64      `json:"version"`
	FailureCount           int        `json:"failure_count"`
	FailureWindowStartedAt *time.Time `json:"failure_window_started_at,omitempty"`
	OpenedUntil            *time.Time `json:"opened_until,omitempty"`
	ReasonCode             string     `json:"reason_code"`
	ChangedBy              string     `json:"changed_by"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type PutWorkerControlInput struct {
	JobKind         string `json:"job_kind"`
	State           string `json:"state"`
	ReasonCode      string `json:"reason_code"`
	ExpectedVersion int64  `json:"expected_version"`
}

type AuthorizationCheck struct{ TenantID, ProductSurface, ActorID, ActorRole, Permission, ActionType, ResourceType, ResourceID string }
type AuthorizationResult struct {
	Allowed      bool
	Reason       string
	SnapshotHash string
}

func normalizeReconciliation(in CreateReconciliationInput) (CreateReconciliationInput, error) {
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))
	if in.Mode == "" {
		in.Mode = string(ModeDetect)
	}
	if in.Mode != string(ModeDetect) && in.Mode != string(ModeSafeRepair) {
		return CreateReconciliationInput{}, domainerr.Validation("mode must be detect or safe_repair")
	}
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.IdempotencyKey == "" {
		return CreateReconciliationInput{}, domainerr.Validation("Idempotency-Key is required")
	}
	if in.TriggerType == "" {
		in.TriggerType = "manual"
	}
	if in.TriggerType != "manual" && in.TriggerType != "scheduled" {
		return CreateReconciliationInput{}, domainerr.Validation("trigger is invalid")
	}
	return in, nil
}
func normalizeControl(in PutWorkerControlInput) (PutWorkerControlInput, error) {
	in.JobKind = strings.TrimSpace(in.JobKind)
	in.State = strings.ToLower(strings.TrimSpace(in.State))
	in.ReasonCode = strings.ToLower(strings.TrimSpace(in.ReasonCode))
	if in.JobKind == "" || (in.State != "paused" && in.State != "closed") {
		return PutWorkerControlInput{}, domainerr.Validation("operators may set worker state to paused or closed")
	}
	if !safeCode.MatchString(in.ReasonCode) {
		return PutWorkerControlInput{}, domainerr.Validation("reason_code is invalid")
	}
	return in, nil
}
func fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}
func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var safeCode = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
