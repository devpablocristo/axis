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

type Product struct {
	ID             uuid.UUID
	OrgID          string
	OrgName        string
	ProductSurface string
	ProductName    string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type OrgMember struct {
	OrgID     uuid.UUID
	UserID    string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
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

type CreateProductInput struct {
	OrgID          string
	OrgName        string
	ProductSurface string
	PrincipalID    string
	OwnerUserID    string
}

type UpdateProductInput struct {
	ProductID   string
	OrgName     string
	PrincipalID string
}

type ListInput struct {
	PrincipalID string
	Lifecycle   string
}

type LifecycleInput struct {
	ProductID   string
	PrincipalID string
}

type AddMemberInput struct {
	ProductID string
	UserID    string
	Role      string
}

type NormalizedCreateProductInput struct {
	OrgID          string
	OrgName        string
	ProductSurface string
	PrincipalID    string
	OwnerUserID    string
}

type NormalizedUpdateProductInput struct {
	ProductID   uuid.UUID
	OrgName     string
	PrincipalID string
}

type NormalizedListInput struct {
	PrincipalID string
	Lifecycle   string
}

type NormalizedLifecycleInput struct {
	ProductID   uuid.UUID
	PrincipalID string
}

type NormalizedAddMemberInput struct {
	ProductID uuid.UUID
	UserID    string
	Role      string
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

func NormalizeCreateProductInput(in CreateProductInput) (NormalizedCreateProductInput, error) {
	orgID := strings.TrimSpace(in.OrgID)
	orgName := strings.TrimSpace(in.OrgName)
	if orgID != "" {
		parsedOrgID, err := uuid.Parse(orgID)
		if err != nil || parsedOrgID == uuid.Nil {
			return NormalizedCreateProductInput{}, domainerr.Validation("org_id must be a valid UUID")
		}
		orgID = parsedOrgID.String()
	} else if orgName == "" {
		return NormalizedCreateProductInput{}, domainerr.Validation("org_name is required")
	}
	productSurface := NormalizeProductSurface(in.ProductSurface)
	if productSurface == "" {
		return NormalizedCreateProductInput{}, domainerr.Validation("product_surface is required")
	}
	return NormalizedCreateProductInput{
		OrgID:          orgID,
		OrgName:        orgName,
		ProductSurface: productSurface,
		PrincipalID:    strings.TrimSpace(in.PrincipalID),
		OwnerUserID:    strings.TrimSpace(in.OwnerUserID),
	}, nil
}

func NormalizeUpdateProductInput(in UpdateProductInput) (NormalizedUpdateProductInput, error) {
	productID, err := ParseProductID(in.ProductID)
	if err != nil {
		return NormalizedUpdateProductInput{}, err
	}
	orgName := strings.TrimSpace(in.OrgName)
	if orgName == "" {
		return NormalizedUpdateProductInput{}, domainerr.Validation("org_name is required")
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedUpdateProductInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedUpdateProductInput{
		ProductID:   productID,
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
	productID, err := ParseProductID(in.ProductID)
	if err != nil {
		return NormalizedLifecycleInput{}, err
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedLifecycleInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedLifecycleInput{
		ProductID:   productID,
		PrincipalID: principalID,
	}, nil
}

func NormalizeAddMemberInput(in AddMemberInput) (NormalizedAddMemberInput, error) {
	productID, err := ParseProductID(in.ProductID)
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
		ProductID: productID,
		UserID:    parsedUserID.String(),
		Role:      NormalizeRole(in.Role),
	}, nil
}

func ParseProductID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, domainerr.Validation("product_id is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation("product_id must be a valid UUID")
	}
	return id, nil
}

func ParseOrgID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, domainerr.Validation("org_id is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation("org_id must be a valid UUID")
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

func (t Product) IsUsable() bool {
	return t.Status == StatusActive && t.ArchivedAt == nil && t.TrashedAt == nil
}

func (o Org) IsUsable() bool {
	return o.Status == StatusActive && o.ArchivedAt == nil && o.TrashedAt == nil
}

func (t Product) State() string {
	if t.TrashedAt != nil {
		return StateTrashed
	}
	if t.ArchivedAt != nil {
		return StateArchived
	}
	return StateActive
}

func (m OrgMember) IsUsable() bool {
	return m.Status == StatusActive && m.ArchivedAt == nil && m.TrashedAt == nil
}

func CanMutateProduct(role string) bool {
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
