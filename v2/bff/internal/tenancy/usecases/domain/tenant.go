package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StatusActive = "active"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	DefaultProductSurface = "axis"
)

type Org struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type Tenant struct {
	ID             uuid.UUID
	OrgID          string
	ProductSurface string
	Name           string
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

type EnsureOrgInput struct {
	OrgID string
	Name  string
}

type CreateTenantInput struct {
	OrgID          string
	ProductSurface string
	Name           string
	OwnerUserID    string
}

type AddMemberInput struct {
	TenantID string
	UserID   string
	Role     string
}

type NormalizedCreateTenantInput struct {
	OrgID          string
	ProductSurface string
	Name           string
	OwnerUserID    string
}

type NormalizedAddMemberInput struct {
	TenantID uuid.UUID
	UserID   string
	Role     string
}

func NormalizeEnsureOrgInput(in EnsureOrgInput) (EnsureOrgInput, error) {
	out := EnsureOrgInput{
		OrgID: strings.TrimSpace(in.OrgID),
		Name:  strings.TrimSpace(in.Name),
	}
	if out.OrgID == "" {
		return EnsureOrgInput{}, domainerr.Validation("org_id is required")
	}
	if out.Name == "" {
		out.Name = out.OrgID
	}
	return out, nil
}

func NormalizeCreateTenantInput(in CreateTenantInput) (NormalizedCreateTenantInput, error) {
	orgID := strings.TrimSpace(in.OrgID)
	if orgID == "" {
		return NormalizedCreateTenantInput{}, domainerr.Validation("org_id is required")
	}
	productSurface := NormalizeProductSurface(in.ProductSurface)
	if productSurface == "" {
		return NormalizedCreateTenantInput{}, domainerr.Validation("product_surface is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = orgID + " / " + productSurface
	}
	return NormalizedCreateTenantInput{
		OrgID:          orgID,
		ProductSurface: productSurface,
		Name:           name,
		OwnerUserID:    strings.TrimSpace(in.OwnerUserID),
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
	return NormalizedAddMemberInput{
		TenantID: tenantID,
		UserID:   userID,
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

func (m TenantMember) IsUsable() bool {
	return m.Status == StatusActive && m.ArchivedAt == nil && m.TrashedAt == nil
}
