package agentprofiles

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeRepo struct {
	profiles map[string]Profile
	versions map[string][]Version
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{profiles: map[string]Profile{}, versions: map[string][]Version{}}
}

func (f *fakeRepo) ListProfiles(_ context.Context, lifecycle LifecycleView) ([]Profile, error) {
	out := make([]Profile, 0, len(f.profiles))
	for _, profile := range f.profiles {
		switch lifecycle {
		case LifecycleArchived:
			if profile.ArchivedAt == nil || profile.TrashedAt != nil {
				continue
			}
		case LifecycleTrash:
			if profile.TrashedAt == nil {
				continue
			}
		case LifecycleAll:
		case LifecycleNonTrash:
			if profile.TrashedAt != nil {
				continue
			}
		default:
			if profile.ArchivedAt != nil || profile.TrashedAt != nil {
				continue
			}
		}
		out = append(out, profile)
	}
	return out, nil
}

func (f *fakeRepo) GetProfile(_ context.Context, profileID string) (Profile, error) {
	if profile, ok := f.profiles[profileID]; ok {
		return profile, nil
	}
	for _, profile := range f.profiles {
		if profile.ID.String() == profileID {
			return profile, nil
		}
	}
	return Profile{}, ErrNotFound
}

func (f *fakeRepo) IsArchivedOrTrashed(_ context.Context, profileID string) (bool, bool, error) {
	profile, ok := f.profiles[profileID]
	if !ok {
		return false, false, ErrNotFound
	}
	return profile.ArchivedAt != nil, profile.TrashedAt != nil, nil
}

func (f *fakeRepo) UpsertProfile(_ context.Context, profile Profile) (Profile, error) {
	if profile.ID == uuid.Nil {
		profile.ID = uuid.New()
	}
	f.profiles[profile.ProfileID] = profile
	return profile, nil
}

func (f *fakeRepo) ArchiveProfile(_ context.Context, profileID string) (Profile, error) {
	profile, key, ok := f.getProfileWithKey(profileID)
	if !ok {
		return Profile{}, ErrNotFound
	}
	now := profile.UpdatedAt
	profile.ArchivedAt = &now
	profile.TrashedAt = nil
	f.profiles[key] = profile
	return profile, nil
}

func (f *fakeRepo) TrashProfile(_ context.Context, profileID string) (Profile, error) {
	profile, key, ok := f.getProfileWithKey(profileID)
	if !ok {
		return Profile{}, ErrNotFound
	}
	now := profile.UpdatedAt
	profile.ArchivedAt = nil
	profile.TrashedAt = &now
	f.profiles[key] = profile
	return profile, nil
}

func (f *fakeRepo) RestoreProfile(_ context.Context, profileID string) (Profile, error) {
	profile, key, ok := f.getProfileWithKey(profileID)
	if !ok {
		return Profile{}, ErrNotFound
	}
	profile.ArchivedAt = nil
	profile.TrashedAt = nil
	f.profiles[key] = profile
	return profile, nil
}

func (f *fakeRepo) PurgeProfile(_ context.Context, profileID string) error {
	profile, ok := f.profiles[profileID]
	if !ok || profile.TrashedAt == nil {
		return ErrNotFound
	}
	delete(f.profiles, profileID)
	return nil
}

func (f *fakeRepo) ListVersions(_ context.Context, profileID string, _ int) ([]Version, error) {
	return f.versions[profileID], nil
}

func (f *fakeRepo) getProfileWithKey(profileID string) (Profile, string, bool) {
	if profile, ok := f.profiles[profileID]; ok {
		return profile, profileID, true
	}
	for key, profile := range f.profiles {
		if profile.ID.String() == profileID {
			return profile, key, true
		}
	}
	return Profile{}, "", false
}

func TestUpsertProfileAcceptsCompleteProfile(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	profile, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:    "axis.ops.billing.v1",
		FamilyID:     "axis.ops.billing",
		VersionLabel: "v1",
		Name:         "Billing Agent",
		SystemPrompt: "You handle billing.",
		MaxAutonomy:  "A1",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.FamilyID != "axis.ops.billing" || profile.VersionLabel != "v1" {
		t.Fatalf("expected family/version, got %+v", profile)
	}
}

func TestUpsertProfileRequiresSystemPrompt(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	_, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:    "axis.ops.billing.v1",
		FamilyID:     "axis.ops.billing",
		VersionLabel: "v1",
		Name:         "Billing Agent",
		MaxAutonomy:  "A1",
		Enabled:      true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestUpsertProfileRequiresVersionLabel(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	_, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:    "axis.ops.billing.v1",
		FamilyID:     "axis.ops.billing",
		Name:         "Billing Agent",
		SystemPrompt: "You handle billing.",
		MaxAutonomy:  "A1",
		Enabled:      true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
