package orgs

import (
	"context"
	"strings"

	"github.com/devpablocristo/bff-v2/internal/identity"
	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/orgs/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type RepositoryPort interface {
	Ensure(ctx context.Context, input domain.EnsureInput) (domain.Org, error)
	Get(ctx context.Context, id string) (domain.Org, error)
	List(ctx context.Context, input domain.NormalizedListInput) ([]domain.Org, error)
	Archive(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Unarchive(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Trash(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Restore(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Purge(ctx context.Context, input domain.NormalizedLifecycleInput) error
	HasTenants(ctx context.Context, orgID string) (bool, error)
}

type TenancyPort interface {
	ListForPrincipal(ctx context.Context, userID string) ([]tenantdomain.Tenant, error)
	ResolveAccess(ctx context.Context, tenantID, principalID string) (tenantdomain.Tenant, tenantdomain.TenantMember, error)
}

type UseCases struct {
	repo        RepositoryPort
	tenancy     TenancyPort
	orgProvider identity.OrgProviderPort
}

func NewUseCases(repo RepositoryPort, tenancy TenancyPort, orgProvider identity.OrgProviderPort) *UseCases {
	return &UseCases{repo: repo, tenancy: tenancy, orgProvider: orgProvider}
}

func (u *UseCases) List(ctx context.Context, input domain.ListInput) ([]domain.Org, error) {
	normalized, err := domain.NormalizeListInput(input)
	if err != nil {
		return nil, err
	}
	tenants, err := u.tenancy.ListForPrincipal(ctx, normalized.PrincipalID)
	if err != nil {
		return nil, err
	}
	items, err := u.repo.List(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if hasOwnerAccess(ctx, u.tenancy, normalized.PrincipalID, tenants) {
		return items, nil
	}
	return filterOrgsForTenants(items, tenants), nil
}

func (u *UseCases) Create(ctx context.Context, input domain.CreateInput) (domain.Org, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Org{}, err
	}
	if err := u.requireOwner(ctx, normalized.PrincipalID); err != nil {
		return domain.Org{}, err
	}
	if u.orgProvider == nil {
		return domain.Org{}, domainerr.Unavailable("organization provider is not configured")
	}
	providerOrg, err := u.orgProvider.CreateOrg(ctx, normalized.Name)
	if err != nil {
		return domain.Org{}, err
	}
	return u.repo.Ensure(ctx, ensureFromProvider(providerOrg))
}

func (u *UseCases) Update(ctx context.Context, input domain.UpdateInput) (domain.Org, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Org{}, err
	}
	org, err := u.repo.Get(ctx, normalized.OrgID)
	if err != nil {
		return domain.Org{}, err
	}
	if err := u.requireOrgMutator(ctx, normalized.PrincipalID, org.ID); err != nil {
		return domain.Org{}, err
	}
	if strings.TrimSpace(org.ProviderOrgID) == "" {
		return domain.Org{}, domainerr.Conflict("org is missing provider_org_id")
	}
	if u.orgProvider == nil {
		return domain.Org{}, domainerr.Unavailable("organization provider is not configured")
	}
	providerOrg, err := u.orgProvider.UpdateOrg(ctx, org.ProviderOrgID, normalized.Name)
	if err != nil {
		return domain.Org{}, err
	}
	return u.repo.Ensure(ctx, ensureFromProviderWithID(org.ID, providerOrg))
}

func (u *UseCases) Archive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, org, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureOrgHasNoTenants(ctx, org.ID); err != nil {
		return err
	}
	return u.repo.Archive(ctx, normalized)
}

func (u *UseCases) Unarchive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, org, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if org.State() != domain.StateArchived {
		return domainerr.Conflict("org must be archived before unarchive")
	}
	return u.repo.Unarchive(ctx, normalized)
}

func (u *UseCases) Trash(ctx context.Context, input domain.LifecycleInput) error {
	normalized, org, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureOrgHasNoTenants(ctx, org.ID); err != nil {
		return err
	}
	return u.repo.Trash(ctx, normalized)
}

func (u *UseCases) Restore(ctx context.Context, input domain.LifecycleInput) error {
	normalized, org, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if org.State() != domain.StateTrashed {
		return domainerr.Conflict("org must be trashed before restore")
	}
	return u.repo.Restore(ctx, normalized)
}

func (u *UseCases) Purge(ctx context.Context, input domain.LifecycleInput) error {
	normalized, org, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if org.State() != domain.StateTrashed {
		return domainerr.Conflict("org must be trashed before purge")
	}
	if err := u.ensureOrgHasNoTenants(ctx, org.ID); err != nil {
		return err
	}
	if strings.TrimSpace(org.ProviderOrgID) == "" {
		return domainerr.Conflict("org is missing provider_org_id")
	}
	if u.orgProvider == nil {
		return domainerr.Unavailable("organization provider is not configured")
	}
	if err := u.orgProvider.DeleteOrg(ctx, org.ProviderOrgID); err != nil {
		return err
	}
	return u.repo.Purge(ctx, normalized)
}

func (u *UseCases) normalizeLifecycleMutation(ctx context.Context, input domain.LifecycleInput) (domain.NormalizedLifecycleInput, domain.Org, error) {
	normalized, err := domain.NormalizeLifecycleInput(input)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Org{}, err
	}
	org, err := u.repo.Get(ctx, normalized.OrgID)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Org{}, err
	}
	if err := u.requireOrgMutator(ctx, normalized.PrincipalID, org.ID); err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Org{}, err
	}
	return normalized, org, nil
}

func (u *UseCases) requireOwner(ctx context.Context, principalID string) error {
	if u.tenancy == nil {
		return domainerr.Unavailable("tenancy is not configured")
	}
	tenants, err := u.tenancy.ListForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	if hasOwnerAccess(ctx, u.tenancy, principalID, tenants) {
		return nil
	}
	return domainerr.Forbidden("principal must be owner")
}

func (u *UseCases) requireOrgMutator(ctx context.Context, principalID, orgID string) error {
	if u.tenancy == nil {
		return domainerr.Unavailable("tenancy is not configured")
	}
	tenants, err := u.tenancy.ListForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	if hasOwnerAccess(ctx, u.tenancy, principalID, tenants) {
		return nil
	}
	for _, tenant := range tenants {
		if tenant.OrgID != orgID {
			continue
		}
		_, member, err := u.tenancy.ResolveAccess(ctx, tenant.ID.String(), principalID)
		if err != nil {
			return err
		}
		if tenantdomain.CanMutateTenant(member.Role) {
			return nil
		}
	}
	return domainerr.Forbidden("principal cannot mutate orgs")
}

func hasOwnerAccess(ctx context.Context, tenancy TenancyPort, principalID string, tenants []tenantdomain.Tenant) bool {
	for _, tenant := range tenants {
		_, member, err := tenancy.ResolveAccess(ctx, tenant.ID.String(), principalID)
		if err != nil {
			continue
		}
		if tenantdomain.NormalizeRole(member.Role) == tenantdomain.RoleOwner {
			return true
		}
	}
	return false
}

func filterOrgsForTenants(items []domain.Org, tenants []tenantdomain.Tenant) []domain.Org {
	allowed := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		allowed[tenant.OrgID] = struct{}{}
	}
	out := make([]domain.Org, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func (u *UseCases) ensureOrgHasNoTenants(ctx context.Context, orgID string) error {
	inUse, err := u.repo.HasTenants(ctx, orgID)
	if err != nil {
		return err
	}
	if inUse {
		return domainerr.Conflict("org is used by existing tenants")
	}
	return nil
}

func ensureFromProvider(providerOrg identitydomain.ProviderOrg) domain.EnsureInput {
	return ensureFromProviderWithID("", providerOrg)
}

func ensureFromProviderWithID(orgID string, providerOrg identitydomain.ProviderOrg) domain.EnsureInput {
	return domain.EnsureInput{
		OrgID:         orgID,
		Provider:      providerOrg.Provider,
		ProviderOrgID: providerOrg.ProviderOrgID,
		Name:          providerOrg.Name,
		Slug:          providerOrg.Slug,
		Status:        providerOrg.Status,
		SyncedAt:      providerOrg.SyncedAt,
	}
}
