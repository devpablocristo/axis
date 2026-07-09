package domain

import (
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
	ApprovalID     string
	ApprovalStatus string
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
