package gateway

import "testing"

func TestUnavailableParticipantProducesNoDownstreamTarget(t *testing.T) {
	usecases, err := NewUseCases(nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := usecases.TargetURL("/api/capabilities", ""); got != "" {
		t.Fatalf("unavailable Companion target must remain empty, got %q", got)
	}
	if got := usecases.NexusTargetURL("/api/approvals", ""); got != "" {
		t.Fatalf("unavailable governance target must remain empty, got %q", got)
	}
}
