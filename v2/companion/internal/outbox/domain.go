package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const MaxDeliveryAttempts = 10

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
