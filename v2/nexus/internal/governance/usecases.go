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

type UseCases struct {
	actionTypes ActionTypeReaderPort
}

func NewUseCases(actionTypes ActionTypeReaderPort) *UseCases {
	return &UseCases{actionTypes: actionTypes}
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
		return domain.CheckResult{}, domainerr.Validation("action_type is disabled")
	}
	return decisionForRisk(actionType.RiskClass), nil
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
