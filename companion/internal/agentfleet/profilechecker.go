package agentfleet

import "context"

// ProfileChecker is a consumer-side port used to validate that an agent's
// profile_id references an existing agent profile. It is optional: when not
// wired (nil), SaveAgent skips referential validation. This enforces the
// foreign-key relationship at the usecase layer because companion_agents.profile_id
// has no physical FK (legacy rows hold '' / 'legacy.unprofiled').
type ProfileChecker interface {
	ProfileExists(ctx context.Context, profileID string) (bool, error)
}
