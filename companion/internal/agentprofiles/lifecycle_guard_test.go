package agentprofiles

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUpsertProfileRejectsArchived(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	now := time.Now().UTC()
	repo.profiles["axis.ops.billing.v1"] = Profile{ProfileID: "axis.ops.billing.v1", ArchivedAt: &now}
	uc := NewUsecases(repo)

	_, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:    "axis.ops.billing.v1",
		FamilyID:     "axis.ops.billing",
		VersionLabel: "v2",
		Name:         "Billing Agent",
		SystemPrompt: "Updated.",
		MaxAutonomy:  "A1",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict upserting an archived profile, got %v", err)
	}
}

func TestUpsertProfileAllowsActive(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	if _, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:    "axis.ops.billing.v1",
		FamilyID:     "axis.ops.billing",
		VersionLabel: "v1",
		Name:         "Billing Agent",
		SystemPrompt: "Handle billing.",
		MaxAutonomy:  "A1",
	}); err != nil {
		t.Fatalf("expected active upsert to pass, got %v", err)
	}
}
