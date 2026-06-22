package agentprofiles

import (
	"context"
	"fmt"
	"strings"
)

type Repository interface {
	ListProfiles(ctx context.Context, lifecycle LifecycleView) ([]Profile, error)
	GetProfile(ctx context.Context, profileID string) (Profile, error)
	UpsertProfile(ctx context.Context, profile Profile) (Profile, error)
	ArchiveProfile(ctx context.Context, profileID string) (Profile, error)
	TrashProfile(ctx context.Context, profileID string) (Profile, error)
	RestoreProfile(ctx context.Context, profileID string) (Profile, error)
	PurgeProfile(ctx context.Context, profileID string) error
	ListVersions(ctx context.Context, profileID string, limit int) ([]Version, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListProfiles(ctx context.Context, lifecycle string, includeArchived bool) ([]Profile, error) {
	return u.repo.ListProfiles(ctx, normalizeLifecycleView(lifecycle, includeArchived))
}

func (u *Usecases) GetProfile(ctx context.Context, profileID string) (Profile, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return Profile{}, fmt.Errorf("%w: profile_id is required", ErrValidation)
	}
	return u.repo.GetProfile(ctx, profileID)
}

func (u *Usecases) UpsertProfile(ctx context.Context, profile Profile) (Profile, error) {
	profile = normalizeProfile(profile)
	if err := validateProfile(profile); err != nil {
		return Profile{}, fmt.Errorf("%w: profile_id, family_id, version_label, name, system_prompt and max_autonomy are required", err)
	}
	return u.repo.UpsertProfile(ctx, profile)
}

func (u *Usecases) ArchiveProfile(ctx context.Context, profileID string) (Profile, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return Profile{}, fmt.Errorf("%w: profile_id is required", ErrValidation)
	}
	return u.repo.ArchiveProfile(ctx, profileID)
}

func (u *Usecases) RestoreProfile(ctx context.Context, profileID string) (Profile, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return Profile{}, fmt.Errorf("%w: profile_id is required", ErrValidation)
	}
	return u.repo.RestoreProfile(ctx, profileID)
}

func (u *Usecases) TrashProfile(ctx context.Context, profileID string) (Profile, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return Profile{}, fmt.Errorf("%w: profile_id is required", ErrValidation)
	}
	return u.repo.TrashProfile(ctx, profileID)
}

func (u *Usecases) PurgeProfile(ctx context.Context, profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("%w: profile_id is required", ErrValidation)
	}
	return u.repo.PurgeProfile(ctx, profileID)
}

func (u *Usecases) ListVersions(ctx context.Context, profileID string, limit int) ([]Version, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil, fmt.Errorf("%w: profile_id is required", ErrValidation)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return u.repo.ListVersions(ctx, profileID, limit)
}
