package wire

import (
	"context"

	"github.com/devpablocristo/nexus/governance/internal/actiontypes"
	"github.com/devpablocristo/nexus/governance/internal/requests"
)

// actionTypeCheckerAdapter adapta actiontypes.Usecases al port requests.ActionTypeChecker
type actionTypeCheckerAdapter struct {
	uc *actiontypes.Usecases
}

func newActionTypeCheckerAdapter(uc *actiontypes.Usecases) requests.ActionTypeChecker {
	return &actionTypeCheckerAdapter{uc: uc}
}

func (a *actionTypeCheckerAdapter) GetByName(ctx context.Context, name string, orgID *string) (requests.ActionTypeInfo, error) {
	at, err := a.uc.GetByNameForOrg(ctx, name, orgID)
	if err != nil {
		return requests.ActionTypeInfo{}, err
	}
	return requests.ActionTypeInfo{
		Name:               at.Name,
		RiskClass:          string(at.RiskClass),
		Schema:             at.Schema,
		RequiresBreakGlass: at.RequiresBreakGlass,
		Enabled:            at.Enabled,
	}, nil
}
