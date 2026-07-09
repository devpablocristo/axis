package governance

import (
	"context"
	"fmt"

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

type UseCases struct {
	actionTypes ActionTypeReaderPort
	checks      CheckRecorderPort
}

func NewUseCases(actionTypes ActionTypeReaderPort, checks ...CheckRecorderPort) *UseCases {
	var recorder CheckRecorderPort
	if len(checks) > 0 {
		recorder = checks[0]
	}
	return &UseCases{actionTypes: actionTypes, checks: recorder}
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
			if _, err := u.checks.RecordCheck(ctx, tenantID, normalized, result); err != nil {
				return domain.CheckResult{}, err
			}
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
	}
	return result, nil
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
	result.DecisionReason = fmt.Sprintf("No policy matched; default for risk %s", result.RiskLevel)
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
