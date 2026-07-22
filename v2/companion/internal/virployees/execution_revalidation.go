package virployees

import (
	"context"
	"strings"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// verifyCurrentExecutionEligibility closes the time-of-approval/time-of-use
// gap for lifecycle, capability assignment/promotion, and autonomy.
func (u *UseCases) verifyCurrentExecutionEligibility(ctx context.Context, orgID string, virployeeID uuid.UUID, capabilityKey string, action preparedactions.Action) (virployeedomain.Virployee, capabilitydomain.Capability, error) {
	virployee, err := u.repo.Get(ctx, orgID, virployeeID)
	if err != nil {
		return virployeedomain.Virployee{}, capabilitydomain.Capability{}, err
	}
	if virployee.State() != virployeedomain.StateActive {
		return virployeedomain.Virployee{}, capabilitydomain.Capability{}, domainerr.Conflict("virployee is no longer active")
	}
	capabilityKey = strings.TrimSpace(capabilityKey)
	var matched *capabilitydomain.Capability
	for _, capabilityID := range virployee.CapabilityIDs {
		capability, getErr := u.capabilities.Get(ctx, orgID, capabilityID)
		if getErr != nil {
			return virployeedomain.Virployee{}, capabilitydomain.Capability{}, domainerr.Conflict("an assigned capability could not be revalidated")
		}
		if capability.CapabilityKey == capabilityKey {
			copy := capability
			matched = &copy
			break
		}
	}
	if matched == nil {
		return virployeedomain.Virployee{}, capabilitydomain.Capability{}, domainerr.Conflict("approved capability is no longer assigned to the Virployee")
	}
	if matched.State() != capabilitydomain.StateActive || matched.PromotionState != capabilitydomain.PromotionActive {
		return virployeedomain.Virployee{}, capabilitydomain.Capability{}, domainerr.Conflict("approved capability is no longer active and promoted")
	}
	if !virployee.Autonomy.Allows(matched.RequiredAutonomy) {
		return virployeedomain.Virployee{}, capabilitydomain.Capability{}, domainerr.Conflict("Virployee autonomy no longer allows the approved capability")
	}
	required, ok := executionAutonomyForPreparedAction(action.Action)
	if !ok || !virployee.Autonomy.Allows(required) {
		return virployeedomain.Virployee{}, capabilitydomain.Capability{}, domainerr.Conflict("Virployee autonomy no longer allows execution of the prepared action")
	}
	return virployee, *matched, nil
}

func executionAutonomyForPreparedAction(action string) (virployeedomain.AutonomyLevel, bool) {
	switch strings.TrimSpace(action) {
	case preparedactions.ActionCreate, preparedactions.ActionDelete:
		return virployeedomain.AutonomyA3, true
	default:
		return "", false
	}
}
