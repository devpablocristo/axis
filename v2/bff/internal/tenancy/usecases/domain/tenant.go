package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StatusActive  = "active"
	StateActive   = "active"
	StateArchived = "archived"
	StateTrashed  = "trashed"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	DefaultProductSurface = "axis"
)

type Org struct {
	ID            string
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type Tenant struct {
	ID             uuid.UUID
	OrgID          string
	OrgName        string
	ProductSurface string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type TenantMember struct {
	TenantID  uuid.UUID
	UserID    string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type Product struct {
	ProductSurface string
	Name           string
}

type EnsureOrgInput struct {
	OrgID         string
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      *time.Time
}

type CreateTenantInput struct {
	OrgID          string
	OrgName        string
	ProductSurface string
	PrincipalID    string
	OwnerUserID    string
}

type UpdateTenantInput struct {
	TenantID    string
	OrgName     string
	PrincipalID string
}

type ListInput struct {
	PrincipalID string
	Lifecycle   string
}

type LifecycleInput struct {
	TenantID    string
	PrincipalID string
}

type AddMemberInput struct {
	TenantID string
	UserID   string
	Role     string
}

type NormalizedCreateTenantInput struct {
	OrgID          string
	OrgName        string
	ProductSurface string
	PrincipalID    string
	OwnerUserID    string
}

type NormalizedUpdateTenantInput struct {
	TenantID    uuid.UUID
	OrgName     string
	PrincipalID string
}

type NormalizedListInput struct {
	PrincipalID string
	Lifecycle   string
}

type NormalizedLifecycleInput struct {
	TenantID    uuid.UUID
	PrincipalID string
}

type NormalizedAddMemberInput struct {
	TenantID uuid.UUID
	UserID   string
	Role     string
}

var ProductCatalog = []Product{
	{ProductSurface: "axis", Name: "Axis"},
	{ProductSurface: "companion", Name: "Companion"},
	{ProductSurface: "medmory", Name: "Medmory"},
	{ProductSurface: "ponti", Name: "Ponti"},
	{ProductSurface: "pymes", Name: "Pymes"},
}

func NormalizeEnsureOrgInput(in EnsureOrgInput) (EnsureOrgInput, error) {
	orgID := strings.TrimSpace(in.OrgID)
	if orgID != "" {
		parsed, err := uuid.Parse(orgID)
		if err != nil || parsed == uuid.Nil {
			return EnsureOrgInput{}, domainerr.Validation("axis_org_id must be a valid UUID")
		}
		orgID = parsed.String()
	}
	provider := strings.TrimSpace(strings.ToLower(in.Provider))
	if provider == "" {
		provider = "dev"
	}
	providerOrgID := strings.TrimSpace(in.ProviderOrgID)
	if providerOrgID == "" && orgID == "" {
		return EnsureOrgInput{}, domainerr.Validation("provider_org_id is required")
	}
	out := EnsureOrgInput{
		OrgID:         orgID,
		Provider:      provider,
		ProviderOrgID: providerOrgID,
		Name:          strings.TrimSpace(in.Name),
		Slug:          NormalizeProductSurface(in.Slug),
		Status:        normalizeStatus(in.Status),
		SyncedAt:      in.SyncedAt,
	}
	if out.Name == "" {
		out.Name = firstNonEmpty(out.ProviderOrgID, out.OrgID)
	}
	if out.Slug == "" {
		out.Slug = NormalizeProductSurface(out.Name)
	}
	return out, nil
}

func NormalizeCreateTenantInput(in CreateTenantInput) (NormalizedCreateTenantInput, error) {
	orgID := strings.TrimSpace(in.OrgID)
	orgName := strings.TrimSpace(in.OrgName)
	if orgID != "" {
		parsedOrgID, err := uuid.Parse(orgID)
		if err != nil || parsedOrgID == uuid.Nil {
			return NormalizedCreateTenantInput{}, domainerr.Validation("org_id must be a valid UUID")
		}
		orgID = parsedOrgID.String()
	} else if orgName == "" {
		return NormalizedCreateTenantInput{}, domainerr.Validation("org_name is required")
	}
	productSurface := NormalizeProductSurface(in.ProductSurface)
	if productSurface == "" {
		return NormalizedCreateTenantInput{}, domainerr.Validation("product_surface is required")
	}
	return NormalizedCreateTenantInput{
		OrgID:          orgID,
		OrgName:        orgName,
		ProductSurface: productSurface,
		PrincipalID:    strings.TrimSpace(in.PrincipalID),
		OwnerUserID:    strings.TrimSpace(in.OwnerUserID),
	}, nil
}

func NormalizeUpdateTenantInput(in UpdateTenantInput) (NormalizedUpdateTenantInput, error) {
	tenantID, err := ParseTenantID(in.TenantID)
	if err != nil {
		return NormalizedUpdateTenantInput{}, err
	}
	orgName := strings.TrimSpace(in.OrgName)
	if orgName == "" {
		return NormalizedUpdateTenantInput{}, domainerr.Validation("org_name is required")
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedUpdateTenantInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedUpdateTenantInput{
		TenantID:    tenantID,
		OrgName:     orgName,
		PrincipalID: principalID,
	}, nil
}

func NormalizeListInput(in ListInput) (NormalizedListInput, error) {
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedListInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedListInput{
		PrincipalID: principalID,
		Lifecycle:   NormalizeState(in.Lifecycle),
	}, nil
}

func NormalizeLifecycleInput(in LifecycleInput) (NormalizedLifecycleInput, error) {
	tenantID, err := ParseTenantID(in.TenantID)
	if err != nil {
		return NormalizedLifecycleInput{}, err
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedLifecycleInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedLifecycleInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
	}, nil
}

func NormalizeAddMemberInput(in AddMemberInput) (NormalizedAddMemberInput, error) {
	tenantID, err := ParseTenantID(in.TenantID)
	if err != nil {
		return NormalizedAddMemberInput{}, err
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		return NormalizedAddMemberInput{}, domainerr.Validation("user_id is required")
	}
	parsedUserID, err := uuid.Parse(userID)
	if err != nil || parsedUserID == uuid.Nil {
		return NormalizedAddMemberInput{}, domainerr.Validation("user_id must be a valid UUID")
	}
	return NormalizedAddMemberInput{
		TenantID: tenantID,
		UserID:   parsedUserID.String(),
		Role:     NormalizeRole(in.Role),
	}, nil
}

func ParseTenantID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, domainerr.Validation("tenant_id is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation("tenant_id must be a valid UUID")
	}
	return id, nil
}

func NormalizeRole(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case RoleOwner, RoleAdmin, RoleMember:
		return strings.TrimSpace(strings.ToLower(raw))
	default:
		return RoleMember
	}
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

func NormalizeProductSurface(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	out := make([]rune, 0, len(raw))
	lastDash := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out = append(out, r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && len(out) > 0 {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

func (t Tenant) IsUsable() bool {
	return t.Status == StatusActive && t.ArchivedAt == nil && t.TrashedAt == nil
}

func (t Tenant) State() string {
	if t.TrashedAt != nil {
		return StateTrashed
	}
	if t.ArchivedAt != nil {
		return StateArchived
	}
	return StateActive
}

func (m TenantMember) IsUsable() bool {
	return m.Status == StatusActive && m.ArchivedAt == nil && m.TrashedAt == nil
}

func CanMutateTenant(role string) bool {
	switch NormalizeRole(role) {
	case RoleOwner, RoleAdmin:
		return true
	default:
		return false
	}
}

func normalizeStatus(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case StatusActive, "":
		return StatusActive
	default:
		return strings.TrimSpace(strings.ToLower(raw))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
