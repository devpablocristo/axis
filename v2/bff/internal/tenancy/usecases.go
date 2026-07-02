package tenancy

import (
	"context"
	"strings"

	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	EnsureOrg(ctx context.Context, input domain.EnsureOrgInput) (domain.Org, error)
	CreateTenant(ctx context.Context, input domain.NormalizedCreateTenantInput) (domain.Tenant, error)
	TenantByID(ctx context.Context, id uuid.UUID) (domain.Tenant, error)
	ListForPrincipal(ctx context.Context, userID string) ([]domain.Tenant, error)
	List(ctx context.Context, orgID string) ([]domain.Tenant, error)
	UpsertMember(ctx context.Context, input domain.NormalizedAddMemberInput) (domain.TenantMember, error)
	TenantMembership(ctx context.Context, tenantID uuid.UUID, userID string) (domain.TenantMember, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) EnsureDefaultTenant(ctx context.Context, orgID, orgName, userID string) (domain.Tenant, error) {
	org, err := domain.NormalizeEnsureOrgInput(domain.EnsureOrgInput{OrgID: orgID, Name: orgName})
	if err != nil {
		return domain.Tenant{}, err
	}
	if _, err := u.repo.EnsureOrg(ctx, org); err != nil {
		return domain.Tenant{}, err
	}
	tenant, err := u.Create(ctx, domain.CreateTenantInput{
		OrgID:          org.OrgID,
		ProductSurface: domain.DefaultProductSurface,
		Name:           org.Name + " / " + domain.DefaultProductSurface,
	})
	if err != nil {
		return domain.Tenant{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return tenant, nil
	}
	if _, err := u.AddMember(ctx, domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   userID,
		Role:     domain.RoleOwner,
	}); err != nil {
		return domain.Tenant{}, err
	}
	return tenant, nil
}

func (u *UseCases) Create(ctx context.Context, input domain.CreateTenantInput) (domain.Tenant, error) {
	normalized, err := domain.NormalizeCreateTenantInput(input)
	if err != nil {
		return domain.Tenant{}, err
	}
	if _, err := u.repo.EnsureOrg(ctx, domain.EnsureOrgInput{OrgID: normalized.OrgID, Name: normalized.OrgID}); err != nil {
		return domain.Tenant{}, err
	}
	tenant, err := u.repo.CreateTenant(ctx, normalized)
	if err != nil {
		return domain.Tenant{}, err
	}
	if normalized.OwnerUserID != "" {
		_, err = u.AddMember(ctx, domain.AddMemberInput{
			TenantID: tenant.ID.String(),
			UserID:   normalized.OwnerUserID,
			Role:     domain.RoleOwner,
		})
	}
	return tenant, err
}

func (u *UseCases) AddMember(ctx context.Context, input domain.AddMemberInput) (domain.TenantMember, error) {
	normalized, err := domain.NormalizeAddMemberInput(input)
	if err != nil {
		return domain.TenantMember{}, err
	}
	return u.repo.UpsertMember(ctx, normalized)
}

func (u *UseCases) ListForPrincipal(ctx context.Context, userID string) ([]domain.Tenant, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, domainerr.Validation("user_id is required")
	}
	return u.repo.ListForPrincipal(ctx, userID)
}

func (u *UseCases) ResolveAccess(ctx context.Context, tenantID, principalID string) (domain.Tenant, domain.TenantMember, error) {
	id, err := domain.ParseTenantID(tenantID)
	if err != nil {
		return domain.Tenant{}, domain.TenantMember{}, err
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return domain.Tenant{}, domain.TenantMember{}, domainerr.Validation("principal_id is required")
	}
	tenant, err := u.repo.TenantByID(ctx, id)
	if err != nil {
		return domain.Tenant{}, domain.TenantMember{}, err
	}
	if !tenant.IsUsable() {
		return domain.Tenant{}, domain.TenantMember{}, domainerr.Forbidden("tenant is not active")
	}
	member, err := u.repo.TenantMembership(ctx, id, principalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domain.Tenant{}, domain.TenantMember{}, domainerr.Forbidden("principal is not a member of the requested tenant")
		}
		return domain.Tenant{}, domain.TenantMember{}, err
	}
	if !member.IsUsable() {
		return domain.Tenant{}, domain.TenantMember{}, domainerr.Forbidden("tenant membership is not active")
	}
	return tenant, member, nil
}
