package domain

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StatusActive  = "active"
	StateActive   = "active"
	StateArchived = "archived"
	StateTrashed  = "trashed"
)

type Org struct {
	ID            string
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      *time.Time
	TenantCount   int
	CreatedAt     time.Time
	UpdatedAt     time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type EnsureInput struct {
	OrgID         string
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      *time.Time
}

type ListInput struct {
	Lifecycle   string
	PrincipalID string
}

type CreateInput struct {
	Name        string
	PrincipalID string
}

type UpdateInput struct {
	OrgID       string
	Name        string
	PrincipalID string
}

type LifecycleInput struct {
	OrgID       string
	PrincipalID string
}

type NormalizedListInput struct {
	Lifecycle   string
	PrincipalID string
}

type NormalizedCreateInput struct {
	Name        string
	PrincipalID string
}

type NormalizedUpdateInput struct {
	OrgID       string
	Name        string
	PrincipalID string
}

type NormalizedLifecycleInput struct {
	OrgID       string
	PrincipalID string
}

func NormalizeListInput(in ListInput) (NormalizedListInput, error) {
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedListInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedListInput{Lifecycle: NormalizeState(in.Lifecycle), PrincipalID: principalID}, nil
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedCreateInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedCreateInput{Name: name, PrincipalID: principalID}, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	orgID, err := NormalizeOrgID(in.OrgID)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedUpdateInput{OrgID: orgID, Name: name, PrincipalID: principalID}, nil
}

func NormalizeLifecycleInput(in LifecycleInput) (NormalizedLifecycleInput, error) {
	orgID, err := NormalizeOrgID(in.OrgID)
	if err != nil {
		return NormalizedLifecycleInput{}, err
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedLifecycleInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedLifecycleInput{OrgID: orgID, PrincipalID: principalID}, nil
}

func NormalizeEnsureInput(in EnsureInput) (EnsureInput, error) {
	orgID := strings.TrimSpace(in.OrgID)
	if orgID != "" {
		normalized, err := NormalizeOrgID(orgID)
		if err != nil {
			return EnsureInput{}, err
		}
		orgID = normalized
	}
	provider := strings.TrimSpace(strings.ToLower(in.Provider))
	if provider == "" {
		provider = "dev"
	}
	providerOrgID := strings.TrimSpace(in.ProviderOrgID)
	if providerOrgID == "" && orgID == "" {
		return EnsureInput{}, domainerr.Validation("provider_org_id is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = firstNonEmpty(providerOrgID, orgID)
	}
	status := strings.TrimSpace(strings.ToLower(in.Status))
	if status == "" {
		status = StatusActive
	}
	return EnsureInput{
		OrgID:         orgID,
		Provider:      provider,
		ProviderOrgID: providerOrgID,
		Name:          name,
		Slug:          strings.TrimSpace(strings.ToLower(in.Slug)),
		Status:        status,
		SyncedAt:      in.SyncedAt,
	}, nil
}

func NormalizeOrgID(raw string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return "", domainerr.Validation("org_id must be a valid UUID")
	}
	return id.String(), nil
}

func NormalizeState(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case StateArchived:
		return StateArchived
	case "trash", StateTrashed:
		return StateTrashed
	default:
		return StateActive
	}
}

func (o Org) State() string {
	if o.TrashedAt != nil {
		return StateTrashed
	}
	if o.ArchivedAt != nil {
		return StateArchived
	}
	return StateActive
}

func (o Org) HasTenants() bool {
	return o.TenantCount > 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
