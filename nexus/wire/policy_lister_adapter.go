package wire

import (
	"context"

	"github.com/devpablocristo/nexus/internal/policies"
	"github.com/devpablocristo/nexus/internal/requests"
)

// policyListerAdapter adapta policies.Usecases al port requests.PolicyLister.
type policyListerAdapter struct {
	uc *policies.Usecases
}

func newPolicyListerAdapter(uc *policies.Usecases) requests.PolicyLister {
	return &policyListerAdapter{uc: uc}
}

func (a *policyListerAdapter) ListActive(ctx context.Context, orgID *string) ([]requests.PolicyForEval, error) {
	list, err := a.uc.ListActive(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]requests.PolicyForEval, len(list))
	for i, p := range list {
		out[i] = requests.PolicyForEval{
			ID:           p.ID,
			Name:         p.Name,
			ActionType:   p.ActionType,
			TargetSystem: p.TargetSystem,
			Expression:   p.Expression,
			Effect:       p.Effect,
			RiskOverride: p.RiskOverride,
			Mode:         string(p.Mode),
		}
	}
	return out, nil
}
