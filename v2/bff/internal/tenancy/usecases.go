package tenancy

import (
	"context"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity"
	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	EnsureOrg(ctx context.Context, input domain.EnsureOrgInput) (domain.Org, error)
	OrgByID(ctx context.Context, id string) (domain.Org, error)
	OrgByProvider(ctx context.Context, provider, providerOrgID string) (domain.Org, error)
	CreateTenant(ctx context.Context, input domain.NormalizedCreateTenantInput) (domain.Tenant, error)
	TenantByID(ctx context.Context, id uuid.UUID) (domain.Tenant, error)
	ListLifecycle(ctx context.Context, lifecycle string) ([]domain.Tenant, error)
	ListForPrincipal(ctx context.Context, userID string) ([]domain.Tenant, error)
	ListForPrincipalLifecycle(ctx context.Context, userID, lifecycle string) ([]domain.Tenant, error)
	List(ctx context.Context, orgID string) ([]domain.Tenant, error)
	UpsertMember(ctx context.Context, input domain.NormalizedAddMemberInput) (domain.TenantMember, error)
	TenantMembership(ctx context.Context, tenantID uuid.UUID, userID string) (domain.TenantMember, error)
	PrincipalHasOwnerRole(ctx context.Context, userID string) (bool, error)
	ArchiveTenant(ctx context.Context, id uuid.UUID, at time.Time) error
	UnarchiveTenant(ctx context.Context, id uuid.UUID) error
	TrashTenant(ctx context.Context, id uuid.UUID, at time.Time, purgeAfter *time.Time) error
	RestoreTenant(ctx context.Context, id uuid.UUID) error
	PurgeTenant(ctx context.Context, id uuid.UUID) error
	HasOtherOrgTenants(ctx context.Context, orgID string, excludedTenantID uuid.UUID) (bool, error)
	DeleteOrg(ctx context.Context, orgID string) error
	DeactivateUserMemberships(ctx context.Context, userID string) error
	DeactivateOrgUserMemberships(ctx context.Context, orgID, userID string) error
}

type ProductResolverPort interface {
	ActiveProductExists(ctx context.Context, productSurface string) (bool, error)
}

type UseCases struct {
	repo            RepositoryPort
	orgProvider     identity.OrgProviderPort
	productResolver ProductResolverPort
}

func NewUseCases(repo RepositoryPort, providers ...identity.OrgProviderPort) *UseCases {
	var orgProvider identity.OrgProviderPort
	if len(providers) > 0 {
		orgProvider = providers[0]
	}
	return &UseCases{repo: repo, orgProvider: orgProvider}
}

func NewUseCasesWithProductResolver(repo RepositoryPort, productResolver ProductResolverPort, providers ...identity.OrgProviderPort) *UseCases {
	uc := NewUseCases(repo, providers...)
	uc.productResolver = productResolver
	return uc
}

func (u *UseCases) EnsureDefaultTenant(ctx context.Context, orgID, orgName, userID string) (domain.Tenant, error) {
	return u.EnsureProviderDefaultTenant(ctx, domain.EnsureOrgInput{
		Provider:      "dev",
		ProviderOrgID: orgID,
		Name:          orgName,
	}, userID)
}

func (u *UseCases) EnsureProviderDefaultTenant(ctx context.Context, input domain.EnsureOrgInput, userID string) (domain.Tenant, error) {
	return u.EnsureProviderDefaultTenantWithRole(ctx, input, userID, domain.RoleOwner)
}

func (u *UseCases) EnsureProviderDefaultTenantWithRole(ctx context.Context, input domain.EnsureOrgInput, userID, role string) (domain.Tenant, error) {
	orgInput, err := domain.NormalizeEnsureOrgInput(input)
	if err != nil {
		return domain.Tenant{}, err
	}
	org, err := u.repo.EnsureOrg(ctx, orgInput)
	if err != nil {
		return domain.Tenant{}, err
	}
	tenant, err := u.Create(ctx, domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: domain.DefaultProductSurface,
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
		Role:     role,
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
	if err := u.requireActiveProduct(ctx, normalized.ProductSurface); err != nil {
		return domain.Tenant{}, err
	}
	if normalized.OrgID == "" {
		org, err := u.createProviderOrg(ctx, normalized.OrgName)
		if err != nil {
			return domain.Tenant{}, err
		}
		normalized.OrgID = org.ID
		normalized.OwnerUserID = firstNonEmpty(normalized.OwnerUserID, normalized.PrincipalID)
	} else {
		if _, err := u.repo.OrgByID(ctx, normalized.OrgID); err != nil {
			return domain.Tenant{}, err
		}
		if normalized.PrincipalID != "" {
			if err := u.requireOrgMutator(ctx, normalized.OrgID, normalized.PrincipalID); err != nil {
				return domain.Tenant{}, err
			}
		}
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

func (u *UseCases) Update(ctx context.Context, input domain.UpdateTenantInput) (domain.Tenant, error) {
	normalized, err := domain.NormalizeUpdateTenantInput(input)
	if err != nil {
		return domain.Tenant{}, err
	}
	tenant, err := u.repo.TenantByID(ctx, normalized.TenantID)
	if err != nil {
		return domain.Tenant{}, err
	}
	if err := u.requireOrgMutator(ctx, tenant.OrgID, normalized.PrincipalID); err != nil {
		return domain.Tenant{}, err
	}
	org, err := u.repo.OrgByID(ctx, tenant.OrgID)
	if err != nil {
		return domain.Tenant{}, err
	}
	if strings.TrimSpace(org.ProviderOrgID) == "" {
		return domain.Tenant{}, domainerr.Conflict("org is missing provider_org_id")
	}
	if u.orgProvider == nil {
		return domain.Tenant{}, domainerr.Unavailable("organization provider is not configured")
	}
	providerOrg, err := u.orgProvider.UpdateOrg(ctx, org.ProviderOrgID, normalized.OrgName)
	if err != nil {
		return domain.Tenant{}, err
	}
	if _, err := u.repo.EnsureOrg(ctx, domain.EnsureOrgInput{
		OrgID:         org.ID,
		Provider:      providerOrg.Provider,
		ProviderOrgID: providerOrg.ProviderOrgID,
		Name:          providerOrg.Name,
		Slug:          providerOrg.Slug,
		Status:        providerOrg.Status,
		SyncedAt:      providerOrg.SyncedAt,
	}); err != nil {
		return domain.Tenant{}, err
	}
	return u.repo.TenantByID(ctx, normalized.TenantID)
}

func (u *UseCases) OrgByID(ctx context.Context, id string) (domain.Org, error) {
	return u.repo.OrgByID(ctx, id)
}

func (u *UseCases) EnsureOrg(ctx context.Context, input domain.EnsureOrgInput) (domain.Org, error) {
	normalized, err := domain.NormalizeEnsureOrgInput(input)
	if err != nil {
		return domain.Org{}, err
	}
	return u.repo.EnsureOrg(ctx, normalized)
}

func (u *UseCases) OrgByProvider(ctx context.Context, provider, providerOrgID string) (domain.Org, error) {
	return u.repo.OrgByProvider(ctx, provider, providerOrgID)
}

func (u *UseCases) DeactivateUserMemberships(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return domainerr.Validation("user_id is required")
	}
	return u.repo.DeactivateUserMemberships(ctx, userID)
}

func (u *UseCases) DeactivateOrgUserMemberships(ctx context.Context, orgID, userID string) error {
	orgID = strings.TrimSpace(orgID)
	userID = strings.TrimSpace(userID)
	if orgID == "" || userID == "" {
		return domainerr.Validation("org_id and user_id are required")
	}
	return u.repo.DeactivateOrgUserMemberships(ctx, orgID, userID)
}

func (u *UseCases) AddMember(ctx context.Context, input domain.AddMemberInput) (domain.TenantMember, error) {
	normalized, err := domain.NormalizeAddMemberInput(input)
	if err != nil {
		return domain.TenantMember{}, err
	}
	return u.repo.UpsertMember(ctx, normalized)
}

func (u *UseCases) List(ctx context.Context, input domain.ListInput) ([]domain.Tenant, error) {
	normalized, err := domain.NormalizeListInput(input)
	if err != nil {
		return nil, err
	}
	if isOwner, err := u.principalHasOwnerAccess(ctx, normalized.PrincipalID); err != nil {
		return nil, err
	} else if isOwner {
		return u.repo.ListLifecycle(ctx, normalized.Lifecycle)
	}
	return u.repo.ListForPrincipalLifecycle(ctx, normalized.PrincipalID, normalized.Lifecycle)
}

func (u *UseCases) ListForPrincipal(ctx context.Context, userID string) ([]domain.Tenant, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, domainerr.Validation("user_id is required")
	}
	if isOwner, err := u.principalHasOwnerAccess(ctx, userID); err != nil {
		return nil, err
	} else if isOwner {
		return u.repo.ListLifecycle(ctx, domain.StateActive)
	}
	return u.repo.ListForPrincipal(ctx, userID)
}

func (u *UseCases) Archive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, tenant, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureCanDeactivate(ctx, tenant, normalized.PrincipalID); err != nil {
		return err
	}
	return u.repo.ArchiveTenant(ctx, normalized.TenantID, time.Now().UTC())
}

func (u *UseCases) Unarchive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, tenant, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if tenant.State() != domain.StateArchived {
		return domainerr.Conflict("tenant must be archived before unarchive")
	}
	return u.repo.UnarchiveTenant(ctx, normalized.TenantID)
}

func (u *UseCases) Trash(ctx context.Context, input domain.LifecycleInput) error {
	normalized, tenant, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureCanDeactivate(ctx, tenant, normalized.PrincipalID); err != nil {
		return err
	}
	now := time.Now().UTC()
	purgeAfter := now.Add(30 * 24 * time.Hour)
	return u.repo.TrashTenant(ctx, normalized.TenantID, now, &purgeAfter)
}

func (u *UseCases) Restore(ctx context.Context, input domain.LifecycleInput) error {
	normalized, tenant, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if tenant.State() != domain.StateTrashed {
		return domainerr.Conflict("tenant must be trashed before restore")
	}
	return u.repo.RestoreTenant(ctx, normalized.TenantID)
}

func (u *UseCases) Purge(ctx context.Context, input domain.LifecycleInput) error {
	normalized, tenant, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if tenant.State() != domain.StateTrashed {
		return domainerr.Conflict("tenant must be trashed before purge")
	}
	hasOtherTenants, err := u.repo.HasOtherOrgTenants(ctx, tenant.OrgID, normalized.TenantID)
	if err != nil {
		return err
	}
	if !hasOtherTenants {
		if err := u.deleteProviderOrg(ctx, tenant.OrgID); err != nil {
			return err
		}
	}
	if err := u.repo.PurgeTenant(ctx, normalized.TenantID); err != nil {
		return err
	}
	if !hasOtherTenants {
		return u.repo.DeleteOrg(ctx, tenant.OrgID)
	}
	return nil
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
			if isOwner, ownerErr := u.principalHasOwnerAccess(ctx, principalID); ownerErr != nil {
				return domain.Tenant{}, domain.TenantMember{}, ownerErr
			} else if isOwner {
				return tenant, virtualOwnerMember(id, principalID), nil
			}
			return domain.Tenant{}, domain.TenantMember{}, domainerr.Forbidden("principal is not a member of the requested tenant")
		}
		return domain.Tenant{}, domain.TenantMember{}, err
	}
	if !member.IsUsable() {
		return domain.Tenant{}, domain.TenantMember{}, domainerr.Forbidden("tenant membership is not active")
	}
	return tenant, member, nil
}

func (u *UseCases) createProviderOrg(ctx context.Context, orgName string) (domain.Org, error) {
	if u.orgProvider == nil {
		return domain.Org{}, domainerr.Unavailable("organization provider is not configured")
	}
	providerOrg, err := u.orgProvider.CreateOrg(ctx, orgName)
	if err != nil {
		return domain.Org{}, err
	}
	return u.repo.EnsureOrg(ctx, ensureOrgFromProvider(providerOrg))
}

func (u *UseCases) deleteProviderOrg(ctx context.Context, orgID string) error {
	org, err := u.repo.OrgByID(ctx, orgID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(org.ProviderOrgID) == "" {
		return domainerr.Conflict("org is missing provider_org_id")
	}
	if u.orgProvider == nil {
		return domainerr.Unavailable("organization provider is not configured")
	}
	return u.orgProvider.DeleteOrg(ctx, org.ProviderOrgID)
}

func ensureOrgFromProvider(providerOrg identitydomain.ProviderOrg) domain.EnsureOrgInput {
	return domain.EnsureOrgInput{
		Provider:      providerOrg.Provider,
		ProviderOrgID: providerOrg.ProviderOrgID,
		Name:          providerOrg.Name,
		Slug:          providerOrg.Slug,
		Status:        providerOrg.Status,
		SyncedAt:      providerOrg.SyncedAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (u *UseCases) requireActiveProduct(ctx context.Context, productSurface string) error {
	if u.productResolver == nil {
		return nil
	}
	exists, err := u.productResolver.ActiveProductExists(ctx, productSurface)
	if err != nil {
		return err
	}
	if !exists {
		return domainerr.Validation("product_surface must reference an active product")
	}
	return nil
}

func (u *UseCases) normalizeLifecycleMutation(ctx context.Context, input domain.LifecycleInput) (domain.NormalizedLifecycleInput, domain.Tenant, error) {
	normalized, err := domain.NormalizeLifecycleInput(input)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Tenant{}, err
	}
	tenant, err := u.repo.TenantByID(ctx, normalized.TenantID)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Tenant{}, err
	}
	if err := u.requireTenantMutator(ctx, tenant.ID, normalized.PrincipalID); err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Tenant{}, err
	}
	return normalized, tenant, nil
}

func (u *UseCases) requireTenantMutator(ctx context.Context, tenantID uuid.UUID, principalID string) error {
	member, err := u.repo.TenantMembership(ctx, tenantID, principalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			if isOwner, ownerErr := u.principalHasOwnerAccess(ctx, principalID); ownerErr != nil {
				return ownerErr
			} else if isOwner {
				return nil
			}
			return domainerr.Forbidden("principal is not a member of the requested tenant")
		}
		return err
	}
	if !member.IsUsable() {
		return domainerr.Forbidden("tenant membership is not active")
	}
	if !domain.CanMutateTenant(member.Role) {
		return domainerr.Forbidden("principal cannot mutate tenants")
	}
	return nil
}

func (u *UseCases) requireOrgMutator(ctx context.Context, orgID, principalID string) error {
	if isOwner, err := u.principalHasOwnerAccess(ctx, principalID); err != nil {
		return err
	} else if isOwner {
		return nil
	}
	tenants, err := u.repo.ListForPrincipalLifecycle(ctx, principalID, domain.StateActive)
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		if tenant.OrgID != orgID {
			continue
		}
		member, err := u.repo.TenantMembership(ctx, tenant.ID, principalID)
		if err != nil {
			return err
		}
		if domain.CanMutateTenant(member.Role) {
			return nil
		}
	}
	return domainerr.Forbidden("principal cannot mutate tenants for this org")
}

func (u *UseCases) PrincipalHasOwnerAccess(ctx context.Context, userID string) (bool, error) {
	return u.principalHasOwnerAccess(ctx, userID)
}

func (u *UseCases) principalHasOwnerAccess(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, domainerr.Validation("user_id is required")
	}
	return u.repo.PrincipalHasOwnerRole(ctx, userID)
}

func virtualOwnerMember(tenantID uuid.UUID, principalID string) domain.TenantMember {
	return domain.TenantMember{
		TenantID: tenantID,
		UserID:   principalID,
		Role:     domain.RoleOwner,
		Status:   domain.StatusActive,
	}
}

func (u *UseCases) ensureCanDeactivate(ctx context.Context, tenant domain.Tenant, principalID string) error {
	if tenant.State() != domain.StateActive {
		return domainerr.Conflict("tenant must be active")
	}
	activeTenants, err := u.activeTenantsForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	if len(activeTenants) <= 1 {
		return domainerr.Conflict("cannot deactivate the last active tenant for the principal")
	}
	return nil
}

func (u *UseCases) activeTenantsForPrincipal(ctx context.Context, principalID string) ([]domain.Tenant, error) {
	if isOwner, err := u.principalHasOwnerAccess(ctx, principalID); err != nil {
		return nil, err
	} else if isOwner {
		return u.repo.ListLifecycle(ctx, domain.StateActive)
	}
	return u.repo.ListForPrincipalLifecycle(ctx, principalID, domain.StateActive)
}
