package quotas

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AreaInbound    = "inbound"
	AreaLLM        = "llm"
	AreaEmbeddings = "embeddings"
	AreaBytes      = "bytes"
	AreaExecutors  = "executors"
)

var (
	ErrPolicyMissing = errors.New("quota policy is required")
	ErrPolicyInUse   = errors.New("quota policy is required by an active capability")
)

type Key struct {
	TenantID       string `json:"tenant_id"`
	ProductSurface string `json:"product_surface"`
	Area           string `json:"area"`
}

type Policy struct {
	Key
	WindowSeconds int       `json:"window_seconds"`
	RequestLimit  int64     `json:"request_limit"`
	UnitLimit     int64     `json:"unit_limit"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ConsumeRequest struct {
	Key
	IdempotencyKey string
	SubjectType    string
	SubjectID      string
	Units          int64
	Metadata       map[string]any
}

type Decision struct {
	Allowed           bool
	PolicyFound       bool
	RetryAfterSeconds int
	RequestsUsed      int64
	UnitsUsed         int64
}

type Usage struct {
	Key
	IdempotencyKey        string
	SubjectType           string
	SubjectID             string
	Units                 int64
	Model                 string
	EstimatedCostMicroUSD int64
	Metadata              map[string]any
}

type QuotaPort interface {
	Consume(context.Context, ConsumeRequest) (Decision, error)
}

type UsageLedgerPort interface {
	RecordUsage(context.Context, Usage) error
}

type ExceededError struct {
	Key        Key
	RetryAfter int
	Missing    bool
}

func (e *ExceededError) Error() string {
	if e.Missing {
		return "quota policy is required"
	}
	return "quota exceeded"
}

func RetryAfter(err error) (int, bool) {
	var exceeded *ExceededError
	if !errors.As(err, &exceeded) {
		return 0, false
	}
	if exceeded.RetryAfter < 1 {
		return 1, true
	}
	return exceeded.RetryAfter, true
}

func normalizeKey(key Key) (Key, error) {
	key.TenantID = strings.TrimSpace(key.TenantID)
	key.ProductSurface = strings.ToLower(strings.TrimSpace(key.ProductSurface))
	key.Area = strings.ToLower(strings.TrimSpace(key.Area))
	if key.TenantID == "" || key.ProductSurface == "" || key.Area == "" {
		return Key{}, fmt.Errorf("quota key is incomplete")
	}
	return key, nil
}

func validatePolicy(policy Policy) (Policy, error) {
	key, err := normalizeKey(policy.Key)
	if err != nil {
		return Policy{}, err
	}
	policy.Key = key
	if policy.WindowSeconds < 1 || policy.WindowSeconds > 86400 || policy.RequestLimit < 1 || policy.UnitLimit < 1 {
		return Policy{}, fmt.Errorf("quota policy limits are invalid")
	}
	return policy, nil
}
