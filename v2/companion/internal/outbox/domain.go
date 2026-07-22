package outbox

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const MaxDeliveryAttempts = 10

const (
	AggregateTypeExecutionAttempt      = "execution_attempt"
	AggregateTypeProfessionalAuthority = "professional_authority"
	KindExecutionResult                = "execution_result"
	KindAuditEvent                     = "audit_event"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDelivered  Status = "delivered"
	StatusDead       Status = "dead"
)

type Message struct {
	ID            uuid.UUID
	TenantID      string
	AggregateType string
	AggregateID   uuid.UUID
	Kind          string
	DedupeKey     string
	Payload       json.RawMessage
	Status        Status
	Attempts      int
	MaxAttempts   int
	AvailableAt   time.Time
	LeaseOwner    string
	LeaseUntil    *time.Time
	HeartbeatAt   *time.Time
	LastErrorCode string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeliveredAt   *time.Time
}

type NexusExecutionResult struct {
	VirployeeID        string         `json:"virployee_id"`
	GovernanceCheckID  string         `json:"governance_check_id"`
	IdempotencyKey     string         `json:"idempotency_key"`
	BindingHash        string         `json:"binding_hash"`
	Status             string         `json:"status"`
	DurationMS         int64          `json:"duration_ms"`
	Result             map[string]any `json:"result"`
	AttestationVersion string         `json:"attestation_version"`
	ExecutorVersion    string         `json:"executor_version"`
	Attestation        string         `json:"attestation"`
}

// NexusAuditEvent is deliberately metadata-only. Professional authority
// payloads must never carry policy text, principal data, prompts, documents,
// PHI, secrets, or arbitrary maps.
type NexusAuditEvent struct {
	VirployeeID  string `json:"virployee_id"`
	ActorType    string `json:"actor_type"`
	ActorID      string `json:"actor_id"`
	SubjectType  string `json:"subject_type"`
	SubjectID    string `json:"subject_id"`
	EventType    string `json:"event_type"`
	Summary      string `json:"summary"`
	Revision     int64  `json:"revision"`
	SnapshotHash string `json:"snapshot_hash"`
}

// ProfessionalAuthorityAuditSpec is the allowlist shared by producers and
// senders. Requiring exact static summaries prevents user-controlled policy or
// principal content from entering the durable outbox or Nexus audit ledger.
func ProfessionalAuthorityAuditSpec(eventType string) (subjectType, summary string, ok bool) {
	switch strings.TrimSpace(eventType) {
	case "scope_policy_changed":
		return "scope_policy", "professional scope policy changed", true
	case "professional_policy_pack_created":
		return "professional_policy_pack", "professional policy pack created", true
	case "professional_policy_binding_changed":
		return "professional_policy_binding", "professional policy binding changed", true
	case "delegation_created":
		return "delegation", "professional delegation created", true
	case "delegation_revoked":
		return "delegation", "professional delegation revoked", true
	case "delegation_reviewed":
		return "delegation", "professional delegation reviewed", true
	default:
		return "", "", false
	}
}

func ParseNexusAuditEvent(raw json.RawMessage, aggregateID uuid.UUID) (NexusAuditEvent, error) {
	var payload NexusAuditEvent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return NexusAuditEvent{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return NexusAuditEvent{}, fmt.Errorf("multiple JSON values are not allowed")
		}
		return NexusAuditEvent{}, err
	}
	expectedSubjectType, expectedSummary, ok := ProfessionalAuthorityAuditSpec(payload.EventType)
	subjectID, subjectErr := uuid.Parse(payload.SubjectID)
	virployeeValid := payload.VirployeeID == "service:professional-authority"
	if _, err := uuid.Parse(payload.VirployeeID); err == nil {
		virployeeValid = true
	}
	if !ok || payload.SubjectType != expectedSubjectType || payload.Summary != expectedSummary ||
		payload.ActorType != "human" || !safeMetadataID(payload.ActorID) ||
		subjectErr != nil || subjectID != aggregateID || !virployeeValid ||
		payload.Revision <= 0 || !validSHA256(payload.SnapshotHash) {
		return NexusAuditEvent{}, fmt.Errorf("professional authority audit metadata is invalid")
	}
	return payload, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) || value != strings.TrimSpace(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func safeMetadataID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 256 && !strings.ContainsAny(value, "\r\n")
}

type EnqueueInput struct {
	ID            uuid.UUID
	TenantID      string
	AggregateType string
	AggregateID   uuid.UUID
	Kind          string
	DedupeKey     string
	Payload       json.RawMessage
}

type ClaimOptions struct {
	WorkerID string
	Batch    int
	Lease    time.Duration
}

type RecoveryResult struct {
	Pending int64
	Dead    int64
}

type RepositoryPort interface {
	EnqueueTx(context.Context, pgx.Tx, EnqueueInput) (Message, bool, error)
	Claim(context.Context, ClaimOptions) ([]Message, error)
	Heartbeat(context.Context, uuid.UUID, string, time.Duration) error
	MarkDelivered(context.Context, uuid.UUID, string) error
	MarkFailed(context.Context, uuid.UUID, string, string, bool, time.Duration) (Message, error)
	RecoverExpiredLeases(context.Context, int) (RecoveryResult, error)
	Replay(context.Context, string, uuid.UUID, time.Time) (Message, error)
	Get(context.Context, string, uuid.UUID) (Message, error)
}

var ErrMessageNotFound = errors.New("outbox message not found")

type deliveryError struct {
	code      string
	retryable bool
	cause     error
}

func (e *deliveryError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return e.code
}

func (e *deliveryError) Unwrap() error { return e.cause }

func Permanent(code string, cause error) error {
	return &deliveryError{code: normalizeErrorCode(code), cause: cause}
}

func Retryable(code string, cause error) error {
	return &deliveryError{code: normalizeErrorCode(code), retryable: true, cause: cause}
}

func classifyError(err error) (string, bool) {
	var deliveryErr *deliveryError
	if errors.As(err, &deliveryErr) {
		return deliveryErr.code, deliveryErr.retryable
	}
	return "delivery_failed", true
}

var errorCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

func normalizeErrorCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !errorCodePattern.MatchString(value) {
		return "delivery_failed"
	}
	return value
}
