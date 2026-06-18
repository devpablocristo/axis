package agentprofiles

import (
	"context"
	"errors"
	"testing"
)

type fakeRepo struct {
	profiles map[string]Profile
	versions map[string][]Version
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{profiles: map[string]Profile{}, versions: map[string][]Version{}}
}

func (f *fakeRepo) ListProfiles(_ context.Context, includeArchived bool) ([]Profile, error) {
	out := make([]Profile, 0, len(f.profiles))
	for _, profile := range f.profiles {
		if !includeArchived && profile.ArchivedAt != nil {
			continue
		}
		out = append(out, profile)
	}
	return out, nil
}

func (f *fakeRepo) GetProfile(_ context.Context, profileID string) (Profile, error) {
	profile, ok := f.profiles[profileID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return profile, nil
}

func (f *fakeRepo) UpsertProfile(_ context.Context, profile Profile) (Profile, error) {
	f.profiles[profile.ProfileID] = profile
	return profile, nil
}

func (f *fakeRepo) ArchiveProfile(_ context.Context, profileID string) (Profile, error) {
	profile, ok := f.profiles[profileID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	profile.Enabled = false
	f.profiles[profileID] = profile
	return profile, nil
}

func (f *fakeRepo) RestoreProfile(_ context.Context, profileID string) (Profile, error) {
	profile, ok := f.profiles[profileID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	profile.Enabled = true
	f.profiles[profileID] = profile
	return profile, nil
}

func (f *fakeRepo) ListVersions(_ context.Context, profileID string, _ int) ([]Version, error) {
	return f.versions[profileID], nil
}

func TestUpsertProfileDefaultsFamilyAndVersion(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	profile, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:    "axis.ops.billing.v1",
		Name:         "Billing Agent",
		SystemPrompt: "You handle billing.",
		MaxAutonomy:  "A1",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.FamilyID != "axis.ops.billing" || profile.VersionLabel != "v1" {
		t.Fatalf("expected derived family/version, got %+v", profile)
	}
}

func TestUpsertProfileRequiresSystemPrompt(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	_, err := uc.UpsertProfile(context.Background(), Profile{
		ProfileID:   "axis.ops.billing.v1",
		Name:        "Billing Agent",
		MaxAutonomy: "A1",
		Enabled:     true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
