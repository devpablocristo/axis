package domain

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	ProviderDev   = "dev"
	ProviderClerk = "clerk"

	StatusActive  = "active"
	StatusDeleted = "deleted"

	InvitationStatusPending = "pending"
)

type User struct {
	ID             string
	Provider       string
	ProviderUserID string
	Email          string
	Status         string
	SyncedAt       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type EnsureInput struct {
	ID             string
	Provider       string
	ProviderUserID string
	Email          string
	Status         string
	SyncedAt       *time.Time
}

type ProviderUser struct {
	Provider       string
	ProviderUserID string
	Email          string
	Status         string
	SyncedAt       *time.Time
}

type ProviderOrg struct {
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      *time.Time
}

type ProviderOrgMembership struct {
	Org  ProviderOrg
	Role string
	User ProviderUser
}

type ProviderInvitation struct {
	Provider             string
	ProviderInvitationID string
	Email                string
	Role                 string
	Status               string
}

func NormalizeEnsureInput(in EnsureInput) (EnsureInput, error) {
	provider := NormalizeProvider(in.Provider)
	providerUserID := strings.TrimSpace(in.ProviderUserID)
	id := strings.TrimSpace(in.ID)
	if providerUserID == "" && id != "" {
		if _, err := uuid.Parse(id); err != nil {
			providerUserID = id
			id = ""
		}
	}
	out := EnsureInput{
		ID:             id,
		Provider:       provider,
		ProviderUserID: providerUserID,
		Email:          strings.TrimSpace(strings.ToLower(in.Email)),
		Status:         NormalizeStatus(in.Status),
		SyncedAt:       in.SyncedAt,
	}
	if out.ID != "" {
		parsed, err := uuid.Parse(out.ID)
		if err != nil || parsed == uuid.Nil {
			return EnsureInput{}, domainerr.Validation("axis_user_id must be a valid UUID")
		}
		out.ID = parsed.String()
	}
	if out.ProviderUserID == "" {
		return EnsureInput{}, domainerr.Validation("provider_user_id is required")
	}
	if out.Email == "" {
		out.Email = out.ProviderUserID
	}
	return out, nil
}

func NormalizeProvider(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case ProviderClerk:
		return ProviderClerk
	default:
		return ProviderDev
	}
}

func NormalizeStatus(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case StatusDeleted:
		return StatusDeleted
	default:
		return StatusActive
	}
}

func ProviderUserEnsureInput(user ProviderUser) EnsureInput {
	return EnsureInput{
		Provider:       user.Provider,
		ProviderUserID: user.ProviderUserID,
		Email:          user.Email,
		Status:         user.Status,
		SyncedAt:       user.SyncedAt,
	}
}
