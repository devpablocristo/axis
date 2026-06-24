package agentfleet

import (
	"context"
	"errors"
	"testing"
)

type fakeProfileChecker struct{ known map[string]bool }

func (f fakeProfileChecker) ProfileExists(_ context.Context, profileID string) (bool, error) {
	return f.known[profileID], nil
}

func TestSaveAgentValidatesProfileReference(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo()).WithProfileChecker(fakeProfileChecker{known: map[string]bool{"support.v1": true}})

	if _, err := uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "ghost", ProfileID: "ghost.v9"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation for unknown profile, got %v", err)
	}
	if _, err := uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "ok", ProfileID: "support.v1"}); err != nil {
		t.Fatalf("expected known profile to pass, got %v", err)
	}
	if _, err := uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "legacy", ProfileID: "legacy.unprofiled"}); err != nil {
		t.Fatalf("expected legacy sentinel to pass, got %v", err)
	}
}

func TestSaveAgentSkipsProfileCheckWhenUnwired(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(newFakeRepo())
	if _, err := uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "a", ProfileID: "anything.v1"}); err != nil {
		t.Fatalf("expected pass without checker, got %v", err)
	}
}
