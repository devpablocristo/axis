package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusCancelRequested Status = "cancel_requested"
	StatusSucceeded       Status = "succeeded"
	StatusDeadLetter      Status = "dead_letter"
	StatusCancelled       Status = "cancelled"
)

const (
	DefaultMaxAttempts    = 3
	DefaultTimeout        = 5 * time.Minute
	DefaultLease          = 30 * time.Second
	DefaultProductSurface = "companion"
)

type Job struct {
	ID             uuid.UUID       `json:"id"`
	OrgID          string          `json:"org_id"`
	ProductSurface string          `json:"product_surface"`
	Kind           string          `json:"kind"`
	ShardKey       string          `json:"shard_key"`
	DedupeKey      string          `json:"dedupe_key"`
	Payload        json.RawMessage `json:"payload"`
	Status         Status          `json:"status"`
	Priority       int             `json:"priority"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	RunAfter       time.Time       `json:"run_after"`
	LeaseOwner     string          `json:"lease_owner,omitempty"`
	LeaseUntil     *time.Time      `json:"lease_until,omitempty"`
	LockedAt       *time.Time      `json:"locked_at,omitempty"`
	HeartbeatAt    *time.Time      `json:"heartbeat_at,omitempty"`
	DeadlineAt     *time.Time      `json:"deadline_at,omitempty"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	LastErrorCode  string          `json:"last_error_code,omitempty"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

type EnqueueInput struct {
	ID             uuid.UUID
	OrgID          string
	ProductSurface string
	Kind           string
	ShardKey       string
	DedupeKey      string
	Payload        json.RawMessage
	Priority       int
	MaxAttempts    int
	RunAfter       time.Time
	DeadlineAt     *time.Time
	Timeout        time.Duration
	ReplacePayload bool
}

type ClaimOptions struct {
	WorkerID      string
	Kinds         []string
	BatchSize     int
	LeaseDuration time.Duration
	ShardCount    int
	ShardIndex    int
}

type FailInput struct {
	JobID     uuid.UUID
	WorkerID  string
	ErrorCode string
	Retryable bool
	Backoff   time.Duration
	Evidence  json.RawMessage
}

type RecoveryResult struct {
	Requeued   int64
	DeadLetter int64
}

type Repository interface {
	Enqueue(ctx context.Context, in EnqueueInput) (Job, bool, error)
	Claim(ctx context.Context, opts ClaimOptions) ([]Job, error)
	Heartbeat(ctx context.Context, jobID uuid.UUID, workerID string, lease time.Duration) error
	Complete(ctx context.Context, jobID uuid.UUID, workerID string, evidence json.RawMessage) error
	Fail(ctx context.Context, in FailInput) (Job, error)
	Cancel(ctx context.Context, orgID string, jobID uuid.UUID, reasonCode string) error
	Get(ctx context.Context, orgID string, jobID uuid.UUID) (Job, error)
	List(ctx context.Context, orgID, productSurface, status string, limit int) ([]Job, error)
	RecoverExpiredLeases(ctx context.Context, limit int) (RecoveryResult, error)
	ReplayDeadLetter(ctx context.Context, orgID string, jobID uuid.UUID, runAfter time.Time) (Job, error)
}

var ErrJobNotFound = errors.New("job not found")
var ErrJobCancelled = errors.New("job cancellation requested")

type HandlerError struct {
	code      string
	retryable bool
	cause     error
}

func (e *HandlerError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return e.code
}

func (e *HandlerError) Unwrap() error { return e.cause }

func Permanent(code string, cause error) error {
	return &HandlerError{code: NormalizeErrorCode(code), cause: cause}
}

func Retryable(code string, cause error) error {
	return &HandlerError{code: NormalizeErrorCode(code), retryable: true, cause: cause}
}

func ClassifyHandlerError(err error) (code string, retryable bool) {
	if err == nil {
		return "", false
	}
	var handlerErr *HandlerError
	if errors.As(err, &handlerErr) {
		return handlerErr.code, handlerErr.retryable
	}
	return "job_failed", true
}

var errorCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

func NormalizeErrorCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !errorCodePattern.MatchString(value) {
		return "job_failed"
	}
	return value
}

func NormalizeEnqueueInput(in EnqueueInput) (EnqueueInput, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	in.ProductSurface = strings.TrimSpace(strings.ToLower(in.ProductSurface))
	in.Kind = strings.TrimSpace(in.Kind)
	in.ShardKey = strings.TrimSpace(in.ShardKey)
	in.DedupeKey = strings.TrimSpace(in.DedupeKey)
	if in.OrgID == "" {
		return EnqueueInput{}, fmt.Errorf("org_id is required")
	}
	if in.ProductSurface == "" {
		in.ProductSurface = DefaultProductSurface
	}
	if in.Kind == "" {
		return EnqueueInput{}, fmt.Errorf("kind is required")
	}
	if in.DedupeKey == "" {
		return EnqueueInput{}, fmt.Errorf("dedupe_key is required")
	}
	if in.ShardKey == "" {
		in.ShardKey = in.OrgID
	}
	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(in.Payload) {
		return EnqueueInput{}, fmt.Errorf("payload must be valid JSON")
	}
	if in.MaxAttempts <= 0 {
		in.MaxAttempts = DefaultMaxAttempts
	}
	if in.Timeout <= 0 {
		in.Timeout = DefaultTimeout
	}
	if in.RunAfter.IsZero() {
		in.RunAfter = time.Now().UTC()
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	return in, nil
}

func NormalizeClaimOptions(opts ClaimOptions) ClaimOptions {
	opts.WorkerID = strings.TrimSpace(opts.WorkerID)
	if opts.WorkerID == "" {
		opts.WorkerID = "companion-worker"
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = DefaultLease
	}
	if opts.ShardCount <= 0 {
		opts.ShardCount = 0
		opts.ShardIndex = 0
	} else if opts.ShardIndex < 0 || opts.ShardIndex >= opts.ShardCount {
		opts.ShardIndex = 0
	}
	return opts
}
