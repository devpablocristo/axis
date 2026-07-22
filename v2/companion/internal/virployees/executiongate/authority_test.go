package executiongate

import "testing"

func TestApplyAuthorityFailsClosed(t *testing.T) {
	base := Result{Gate: Gate{Decision: DecisionPass}}
	blocked := ApplyAuthority(base, AuthorityCheckResult{Allowed: false, Reason: "delegation expired"})
	if blocked.Gate.Decision != DecisionBlocked {
		t.Fatalf("denied authority must block: %+v", blocked.Gate)
	}
	if len(blocked.Gate.Checks) != 1 || blocked.Gate.Checks[0].Key != "professional_authority" {
		t.Fatalf("expected professional authority check: %+v", blocked.Gate.Checks)
	}
	unavailable := ApplyAuthorityUnavailable(base)
	if unavailable.Gate.Decision != DecisionBlocked {
		t.Fatalf("unavailable authority must block: %+v", unavailable.Gate)
	}
}
