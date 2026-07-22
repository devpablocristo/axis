package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionRequireApproval Decision = "require_approval"
)

type Status string

const (
	StatusAllowed         Status = "allowed"
	StatusDenied          Status = "denied"
	StatusPendingApproval Status = "pending_approval"
)

type CheckInput struct {
	RequesterType        string
	RequesterID          string
	ProductSurface       string
	SupervisorUserID     string
	ActionType           string
	TargetSystem         string
	TargetResource       string
	ResourceType         string
	MembershipRole       string
	FunctionalRoles      []string
	FunctionalScopes     []string
	Reason               string
	BindingHash          string
	AuthorityBindingHash string
	ScopeRevision        int64
	PolicyRevisionHash   string
	DelegationRequired   bool
	DelegationID         string
	DelegationRevision   int64
}

type NormalizedCheckInput struct {
	RequesterType        string
	RequesterID          string
	ProductSurface       string
	SupervisorUserID     string
	ActionType           string
	TargetSystem         string
	TargetResource       string
	ResourceType         string
	MembershipRole       string
	FunctionalRoles      []string
	FunctionalScopes     []string
	Reason               string
	BindingHash          string
	AuthorityBindingHash string
	ScopeRevision        int64
	PolicyRevisionHash   string
	DelegationRequired   bool
	DelegationID         string
	DelegationRevision   int64
}

type CheckResult struct {
	CheckID              string
	Decision             Decision
	RiskLevel            string
	Status               Status
	DecisionReason       string
	WouldRequireApproval bool
	Mode                 string
	BindingHash          string
	ApprovalID           string
	ApprovalStatus       string
	PolicySnapshotHash   string
	PolicyInputHash      string
	PolicyMatches        []map[string]any
	RoleSnapshot         map[string]any
}

type RecordedCheck struct {
	CheckID        string
	ApprovalID     string
	ApprovalStatus string
}

type RevalidationInput struct {
	BindingHash          string
	PolicySnapshotHash   string
	AuthorityBindingHash string
	ScopeRevision        int64
	PolicyRevisionHash   string
	DelegationID         string
	DelegationRevision   int64
}

type RevalidationResult struct {
	Valid              bool
	Reason             string
	PolicySnapshotHash string
}

type RevalidationRecord struct {
	Input              NormalizedCheckInput
	Decision           Decision
	RiskLevel          string
	PolicySnapshotHash string
}

type ExecutionResultInput struct {
	IdempotencyKey     string
	BindingHash        string
	Status             string
	DurationMS         int64
	Result             map[string]any
	AttestationVersion string
	ExecutorVersion    string
	Attestation        string
}

type ExecutionResult struct {
	ID                 string
	GovernanceCheckID  string
	RequesterID        string
	BindingHash        string
	Status             string
	DurationMS         int64
	Result             map[string]any
	Created            bool
	IdempotencyKey     string
	AttestationVersion string
	ExecutorVersion    string
	Attestation        string
	ResultHash         string
}

func NormalizeExecutionResultInput(in ExecutionResultInput) (ExecutionResultInput, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.BindingHash = strings.TrimSpace(in.BindingHash)
	in.Status = strings.TrimSpace(in.Status)
	in.AttestationVersion = strings.TrimSpace(in.AttestationVersion)
	in.ExecutorVersion = strings.TrimSpace(in.ExecutorVersion)
	in.Attestation = strings.TrimSpace(in.Attestation)
	if in.IdempotencyKey == "" {
		return ExecutionResultInput{}, domainerr.Validation("Idempotency-Key is required")
	}
	if in.BindingHash == "" {
		return ExecutionResultInput{}, domainerr.Validation("binding_hash is required")
	}
	if in.Status != "succeeded" && in.Status != "failed" {
		return ExecutionResultInput{}, domainerr.Validation("status must be succeeded or failed")
	}
	if in.DurationMS < 0 {
		return ExecutionResultInput{}, domainerr.Validation("duration_ms cannot be negative")
	}
	if in.AttestationVersion == "" || in.ExecutorVersion == "" || in.Attestation == "" {
		return ExecutionResultInput{}, domainerr.Validation("executor attestation is required")
	}
	if in.Result == nil {
		in.Result = map[string]any{}
	}
	raw, err := json.Marshal(in.Result)
	if err != nil || len(raw) > 16*1024 {
		return ExecutionResultInput{}, fmt.Errorf("result payload is invalid or too large")
	}
	in.Result = sanitizeResult(in.Result)
	return in, nil
}

func sanitizeResult(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if sensitiveResultKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = sanitizeResultValue(value)
	}
	return out
}

func sanitizeResultValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeResult(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeResultValue(item))
		}
		return out
	default:
		return value
	}
}

func sensitiveResultKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	for _, sensitive := range []string{"token", "secret", "password", "authorization", "api_key", "cookie", "credential"} {
		if normalized == sensitive || strings.Contains(normalized, sensitive) {
			return true
		}
	}
	return false
}

func NormalizeCheckInput(in CheckInput) (NormalizedCheckInput, error) {
	requesterType := strings.TrimSpace(in.RequesterType)
	requesterID := strings.TrimSpace(in.RequesterID)
	actionType := strings.TrimSpace(in.ActionType)
	if requesterType == "" {
		requesterType = "agent"
	}
	if requesterID == "" {
		return NormalizedCheckInput{}, domainerr.Validation("requester_id is required")
	}
	if actionType == "" {
		return NormalizedCheckInput{}, domainerr.Validation("action_type is required")
	}
	bindingHash := strings.TrimSpace(in.BindingHash)
	authorityBindingHash := strings.TrimSpace(in.AuthorityBindingHash)
	policyRevisionHash := strings.TrimSpace(in.PolicyRevisionHash)
	delegationID := strings.TrimSpace(in.DelegationID)
	if authorityBindingHash != "" {
		if bindingHash == "" {
			return NormalizedCheckInput{}, domainerr.Validation("binding_hash is required with professional authority")
		}
		if policyRevisionHash == "" {
			return NormalizedCheckInput{}, domainerr.Validation("policy_revision_hash is required with professional authority")
		}
		if in.ScopeRevision < 0 || in.DelegationRevision < 0 {
			return NormalizedCheckInput{}, domainerr.Validation("professional authority revisions cannot be negative")
		}
		if in.DelegationRequired && (delegationID == "" || in.DelegationRevision <= 0) {
			return NormalizedCheckInput{}, domainerr.Validation("a current delegation binding is required")
		}
		if delegationID != "" && in.DelegationRevision <= 0 {
			return NormalizedCheckInput{}, domainerr.Validation("delegation_revision is required with delegation_id")
		}
	} else if in.DelegationRequired || delegationID != "" || in.DelegationRevision != 0 {
		return NormalizedCheckInput{}, domainerr.Validation("authority_binding_hash is required with delegation metadata")
	}
	return NormalizedCheckInput{
		RequesterType:        requesterType,
		RequesterID:          requesterID,
		ProductSurface:       strings.ToLower(strings.TrimSpace(in.ProductSurface)),
		SupervisorUserID:     strings.TrimSpace(in.SupervisorUserID),
		ActionType:           actionType,
		TargetSystem:         strings.TrimSpace(in.TargetSystem),
		TargetResource:       strings.TrimSpace(in.TargetResource),
		ResourceType:         strings.ToLower(strings.TrimSpace(in.ResourceType)),
		MembershipRole:       strings.ToLower(strings.TrimSpace(in.MembershipRole)),
		FunctionalRoles:      normalizedStrings(in.FunctionalRoles),
		FunctionalScopes:     normalizedStrings(in.FunctionalScopes),
		Reason:               strings.TrimSpace(in.Reason),
		BindingHash:          bindingHash,
		AuthorityBindingHash: authorityBindingHash,
		ScopeRevision:        in.ScopeRevision,
		PolicyRevisionHash:   policyRevisionHash,
		DelegationRequired:   in.DelegationRequired,
		DelegationID:         delegationID,
		DelegationRevision:   in.DelegationRevision,
	}, nil
}

func normalizedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
