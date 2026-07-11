package governance

import (
	"context"

	actiondomain "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type ActionTypeReaderPort interface {
	GetByKey(ctx context.Context, tenantID string, key string) (actiondomain.ActionType, error)
}

type CheckRecorderPort interface {
	RecordCheck(ctx context.Context, tenantID string, input domain.NormalizedCheckInput, result domain.CheckResult) (domain.RecordedCheck, error)
}

type ExecutionResultRecorderPort interface {
	RecordExecutionResult(ctx context.Context, tenantID, checkID string, input domain.ExecutionResultInput) (domain.ExecutionResult, error)
}

type UseCases struct {
	actionTypes ActionTypeReaderPort
	checks      CheckRecorderPort
	results     ExecutionResultRecorderPort
}

func NewUseCases(actionTypes ActionTypeReaderPort, checks ...CheckRecorderPort) *UseCases {
	var recorder CheckRecorderPort
	if len(checks) > 0 {
		recorder = checks[0]
	}
	var results ExecutionResultRecorderPort
	if typed, ok := recorder.(ExecutionResultRecorderPort); ok {
		results = typed
	}
	return &UseCases{actionTypes: actionTypes, checks: recorder, results: results}
}

func (u *UseCases) Check(ctx context.Context, tenantID string, input domain.CheckInput) (domain.CheckResult, error) {
	normalized, err := domain.NormalizeCheckInput(input)
	if err != nil {
		return domain.CheckResult{}, err
	}
	actionType, err := u.actionTypes.GetByKey(ctx, tenantID, normalized.ActionType)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domain.CheckResult{}, domainerr.Validation("unknown action_type")
		}
		return domain.CheckResult{}, err
	}
	if !actionType.Enabled {
		result := denyForDisabledActionType(actionType.RiskClass)
		result.BindingHash = normalized.BindingHash
		if u.checks != nil {
			recorded, err := u.checks.RecordCheck(ctx, tenantID, normalized, result)
			if err != nil {
				return domain.CheckResult{}, err
			}
			result.CheckID = recorded.CheckID
		}
		return result, nil
	}
	result := decisionForRisk(actionType.RiskClass)
	result.BindingHash = normalized.BindingHash
	if u.checks != nil {
		recorded, err := u.checks.RecordCheck(ctx, tenantID, normalized, result)
		if err != nil {
			return domain.CheckResult{}, err
		}
		result.ApprovalID = recorded.ApprovalID
		result.ApprovalStatus = recorded.ApprovalStatus
		result.CheckID = recorded.CheckID
	}
	return result, nil
}

func (u *UseCases) ReportExecutionResult(ctx context.Context, tenantID, checkID string, input domain.ExecutionResultInput) (domain.ExecutionResult, error) {
	if u.results == nil {
		return domain.ExecutionResult{}, domainerr.Conflict("governance result recorder is not configured")
	}
	normalized, err := domain.NormalizeExecutionResultInput(input)
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	return u.results.RecordExecutionResult(ctx, tenantID, checkID, normalized)
}

func decisionForRisk(risk actiondomain.RiskClass) domain.CheckResult {
	result := domain.CheckResult{
		RiskLevel: string(risk),
		Mode:      "simulation",
	}
	switch risk {
	case actiondomain.RiskClassHigh:
		result.Decision = domain.DecisionRequireApproval
		result.Status = domain.StatusPendingApproval
		result.WouldRequireApproval = true
	default:
		result.Decision = domain.DecisionAllow
		result.Status = domain.StatusAllowed
		result.WouldRequireApproval = false
	}
	result.DecisionReason = "No policy matched; default for risk " + result.RiskLevel
	return result
}

func denyForDisabledActionType(risk actiondomain.RiskClass) domain.CheckResult {
	return domain.CheckResult{
		Decision:             domain.DecisionDeny,
		RiskLevel:            string(risk),
		Status:               domain.StatusDenied,
		DecisionReason:       "Action type is disabled",
		WouldRequireApproval: false,
		Mode:                 "simulation",
	}
}
