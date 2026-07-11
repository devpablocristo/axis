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
	RequesterType  string
	RequesterID    string
	ActionType     string
	TargetSystem   string
	TargetResource string
	Params         map[string]any
	Reason         string
	Context        string
	BindingHash    string
}

type NormalizedCheckInput struct {
	RequesterType  string
	RequesterID    string
	ActionType     string
	TargetSystem   string
	TargetResource string
	Params         map[string]any
	Reason         string
	Context        string
	BindingHash    string
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
}

type RecordedCheck struct {
	CheckID        string
	ApprovalID     string
	ApprovalStatus string
}

type ExecutionResultInput struct {
	IdempotencyKey string
	BindingHash    string
	Status         string
	DurationMS     int64
	Result         map[string]any
}

type ExecutionResult struct {
	ID                string
	GovernanceCheckID string
	BindingHash       string
	Status            string
	DurationMS        int64
	Result            map[string]any
}

func NormalizeExecutionResultInput(in ExecutionResultInput) (ExecutionResultInput, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.BindingHash = strings.TrimSpace(in.BindingHash)
	in.Status = strings.TrimSpace(in.Status)
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
		normalized := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || strings.Contains(normalized, "authorization") {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = value
	}
	return out
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
	params := in.Params
	if params == nil {
		params = make(map[string]any)
	}
	return NormalizedCheckInput{
		RequesterType:  requesterType,
		RequesterID:    requesterID,
		ActionType:     actionType,
		TargetSystem:   strings.TrimSpace(in.TargetSystem),
		TargetResource: strings.TrimSpace(in.TargetResource),
		Params:         params,
		Reason:         strings.TrimSpace(in.Reason),
		Context:        strings.TrimSpace(in.Context),
		BindingHash:    strings.TrimSpace(in.BindingHash),
	}, nil
}
