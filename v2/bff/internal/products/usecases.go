package products

import (
	"context"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity"
	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	EnsureOrg(ctx context.Context, input domain.EnsureOrgInput) (domain.Org, error)
	OrgByID(ctx context.Context, id string) (domain.Org, error)
	OrgByProvider(ctx context.Context, provider, providerOrgID string) (domain.Org, error)
	CreateProduct(ctx context.Context, input domain.NormalizedCreateProductInput) (domain.Product, error)
	ProductByID(ctx context.Context, id uuid.UUID) (domain.Product, error)
	ListLifecycle(ctx context.Context, lifecycle string) ([]domain.Product, error)
	ListForPrincipal(ctx context.Context, userID string) ([]domain.Product, error)
	ListForPrincipalLifecycle(ctx context.Context, userID, lifecycle string) ([]domain.Product, error)
	List(ctx context.Context, orgID string) ([]domain.Product, error)
	UpsertMember(ctx context.Context, input domain.NormalizedAddMemberInput) (domain.OrgMember, error)
	OrgMembershipForProduct(ctx context.Context, productID uuid.UUID, userID string) (domain.OrgMember, error)
	OrgMembership(ctx context.Context, orgID uuid.UUID, userID string) (domain.OrgMember, error)
	ArchiveProduct(ctx context.Context, id uuid.UUID, at time.Time) error
	UnarchiveProduct(ctx context.Context, id uuid.UUID) error
	TrashProduct(ctx context.Context, id uuid.UUID, at time.Time, purgeAfter *time.Time) error
	RestoreProduct(ctx context.Context, id uuid.UUID) error
	PurgeProduct(ctx context.Context, id uuid.UUID) error
	HasOtherOrgProducts(ctx context.Context, orgID string, excludedProductID uuid.UUID) (bool, error)
	DeleteOrg(ctx context.Context, orgID string) error
	DeactivateUserMemberships(ctx context.Context, userID string) error
	DeactivateOrgUserMemberships(ctx context.Context, orgID, userID string) error
}

type UseCases struct {
	repo        RepositoryPort
	orgProvider identity.OrgProviderPort
}

func NewUseCases(repo RepositoryPort, providers ...identity.OrgProviderPort) *UseCases {
	var orgProvider identity.OrgProviderPort
	if len(providers) > 0 {
		orgProvider = providers[0]
	}
	return &UseCases{repo: repo, orgProvider: orgProvider}
}

func (u *UseCases) EnsureDefaultProduct(ctx context.Context, orgID, orgName, userID string) (domain.Product, error) {
	return u.EnsureProviderDefaultProduct(ctx, domain.EnsureOrgInput{
		Provider:      "dev",
		ProviderOrgID: orgID,
		Name:          orgName,
	}, userID)
}

func (u *UseCases) EnsureProviderDefaultProduct(ctx context.Context, input domain.EnsureOrgInput, userID string) (domain.Product, error) {
	return u.EnsureProviderDefaultProductWithRole(ctx, input, userID, domain.RoleOwner)
}

func (u *UseCases) EnsureProviderDefaultProductWithRole(ctx context.Context, input domain.EnsureOrgInput, userID, role string) (domain.Product, error) {
	orgInput, err := domain.NormalizeEnsureOrgInput(input)
	if err != nil {
		return domain.Product{}, err
	}
	org, err := u.repo.EnsureOrg(ctx, orgInput)
	if err != nil {
		return domain.Product{}, err
	}
	product, err := u.Create(ctx, domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: domain.DefaultProductSurface,
	})
	if err != nil {
		return domain.Product{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return product, nil
	}
	if _, err := u.AddMember(ctx, domain.AddMemberInput{
		ProductID: product.ID.String(),
		UserID:    userID,
		Role:      role,
	}); err != nil {
		return domain.Product{}, err
	}
	return product, nil
}

func (u *UseCases) Create(ctx context.Context, input domain.CreateProductInput) (domain.Product, error) {
	normalized, err := domain.NormalizeCreateProductInput(input)
	if err != nil {
		return domain.Product{}, err
	}
	if normalized.OrgID == "" {
		org, err := u.createProviderOrg(ctx, normalized.OrgName)
		if err != nil {
			return domain.Product{}, err
		}
		normalized.OrgID = org.ID
		normalized.OwnerUserID = firstNonEmpty(normalized.OwnerUserID, normalized.PrincipalID)
	} else {
		if _, err := u.repo.OrgByID(ctx, normalized.OrgID); err != nil {
			return domain.Product{}, err
		}
		if normalized.PrincipalID != "" {
			if err := u.requireOrgMutator(ctx, normalized.OrgID, normalized.PrincipalID); err != nil {
				return domain.Product{}, err
			}
		}
	}
	product, err := u.repo.CreateProduct(ctx, normalized)
	if err != nil {
		return domain.Product{}, err
	}
	if normalized.OwnerUserID != "" {
		_, err = u.AddMember(ctx, domain.AddMemberInput{
			ProductID: product.ID.String(),
			UserID:    normalized.OwnerUserID,
			Role:      domain.RoleOwner,
		})
	}
	return product, err
}

func (u *UseCases) Update(ctx context.Context, input domain.UpdateProductInput) (domain.Product, error) {
	normalized, err := domain.NormalizeUpdateProductInput(input)
	if err != nil {
		return domain.Product{}, err
	}
	product, err := u.repo.ProductByID(ctx, normalized.ProductID)
	if err != nil {
		return domain.Product{}, err
	}
	if err := u.requireOrgMutator(ctx, product.OrgID, normalized.PrincipalID); err != nil {
		return domain.Product{}, err
	}
	org, err := u.repo.OrgByID(ctx, product.OrgID)
	if err != nil {
		return domain.Product{}, err
	}
	if strings.TrimSpace(org.ProviderOrgID) == "" {
		return domain.Product{}, domainerr.Conflict("org is missing provider_org_id")
	}
	if u.orgProvider == nil {
		return domain.Product{}, domainerr.Unavailable("organization provider is not configured")
	}
	providerOrg, err := u.orgProvider.UpdateOrg(ctx, org.ProviderOrgID, normalized.OrgName)
	if err != nil {
		return domain.Product{}, err
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
		return domain.Product{}, err
	}
	return u.repo.ProductByID(ctx, normalized.ProductID)
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

func (u *UseCases) AddMember(ctx context.Context, input domain.AddMemberInput) (domain.OrgMember, error) {
	normalized, err := domain.NormalizeAddMemberInput(input)
	if err != nil {
		return domain.OrgMember{}, err
	}
	return u.repo.UpsertMember(ctx, normalized)
}

func (u *UseCases) List(ctx context.Context, input domain.ListInput) ([]domain.Product, error) {
	normalized, err := domain.NormalizeListInput(input)
	if err != nil {
		return nil, err
	}
	return u.repo.ListForPrincipalLifecycle(ctx, normalized.PrincipalID, normalized.Lifecycle)
}

func (u *UseCases) ListForPrincipal(ctx context.Context, userID string) ([]domain.Product, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, domainerr.Validation("user_id is required")
	}
	return u.repo.ListForPrincipal(ctx, userID)
}

func (u *UseCases) Archive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureCanDeactivate(ctx, product, normalized.PrincipalID); err != nil {
		return err
	}
	return u.repo.ArchiveProduct(ctx, normalized.ProductID, time.Now().UTC())
}

func (u *UseCases) Unarchive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if product.State() != domain.StateArchived {
		return domainerr.Conflict("product must be archived before unarchive")
	}
	return u.repo.UnarchiveProduct(ctx, normalized.ProductID)
}

func (u *UseCases) Trash(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureCanDeactivate(ctx, product, normalized.PrincipalID); err != nil {
		return err
	}
	now := time.Now().UTC()
	purgeAfter := now.Add(30 * 24 * time.Hour)
	return u.repo.TrashProduct(ctx, normalized.ProductID, now, &purgeAfter)
}

func (u *UseCases) Restore(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if product.State() != domain.StateTrashed {
		return domainerr.Conflict("product must be trashed before restore")
	}
	return u.repo.RestoreProduct(ctx, normalized.ProductID)
}

func (u *UseCases) Purge(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if product.State() != domain.StateTrashed {
		return domainerr.Conflict("product must be trashed before purge")
	}
	hasOtherProducts, err := u.repo.HasOtherOrgProducts(ctx, product.OrgID, normalized.ProductID)
	if err != nil {
		return err
	}
	if !hasOtherProducts {
		if err := u.deleteProviderOrg(ctx, product.OrgID); err != nil {
			return err
		}
	}
	if err := u.repo.PurgeProduct(ctx, normalized.ProductID); err != nil {
		return err
	}
	if !hasOtherProducts {
		return u.repo.DeleteOrg(ctx, product.OrgID)
	}
	return nil
}

func (u *UseCases) ResolveAccess(ctx context.Context, productID, principalID string) (domain.Product, domain.OrgMember, error) {
	id, err := domain.ParseProductID(productID)
	if err != nil {
		return domain.Product{}, domain.OrgMember{}, err
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return domain.Product{}, domain.OrgMember{}, domainerr.Validation("principal_id is required")
	}
	product, err := u.repo.ProductByID(ctx, id)
	if err != nil {
		return domain.Product{}, domain.OrgMember{}, err
	}
	if !product.IsUsable() {
		return domain.Product{}, domain.OrgMember{}, domainerr.Forbidden("product is not active")
	}
	member, err := u.repo.OrgMembershipForProduct(ctx, id, principalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domain.Product{}, domain.OrgMember{}, domainerr.Forbidden("principal is not a member of the requested organization")
		}
		return domain.Product{}, domain.OrgMember{}, err
	}
	if !member.IsUsable() {
		return domain.Product{}, domain.OrgMember{}, domainerr.Forbidden("organization membership is not active")
	}
	return product, member, nil
}

// ResolveOrgAccess resolves authority at the organization boundary and then
// selects one of that organization's products. Membership is never stored per
// product.
func (u *UseCases) ResolveOrgAccess(ctx context.Context, orgID, productSurface, principalID string) (domain.Product, domain.OrgMember, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.ToLower(strings.TrimSpace(productSurface))
	principalID = strings.TrimSpace(principalID)
	if orgID == "" || productSurface == "" || principalID == "" {
		return domain.Product{}, domain.OrgMember{}, domainerr.Validation("org_id, product_surface and principal_id are required")
	}
	products, err := u.repo.List(ctx, orgID)
	if err != nil {
		return domain.Product{}, domain.OrgMember{}, err
	}
	for _, product := range products {
		if product.ProductSurface != productSurface || !product.IsUsable() {
			continue
		}
		member, memberErr := u.repo.OrgMembershipForProduct(ctx, product.ID, principalID)
		if memberErr != nil {
			return domain.Product{}, domain.OrgMember{}, memberErr
		}
		if !member.IsUsable() {
			return domain.Product{}, domain.OrgMember{}, domainerr.Forbidden("organization membership is not active")
		}
		return product, member, nil
	}
	return domain.Product{}, domain.OrgMember{}, domainerr.NotFound("product not found for organization")
}

func (u *UseCases) ResolveOrganizationAccess(ctx context.Context, rawOrgID, principalID string) (domain.Org, domain.OrgMember, error) {
	orgID, err := domain.ParseOrgID(rawOrgID)
	if err != nil {
		return domain.Org{}, domain.OrgMember{}, err
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return domain.Org{}, domain.OrgMember{}, domainerr.Validation("principal_id is required")
	}
	org, err := u.repo.OrgByID(ctx, orgID.String())
	if err != nil {
		return domain.Org{}, domain.OrgMember{}, err
	}
	if !org.IsUsable() {
		return domain.Org{}, domain.OrgMember{}, domainerr.Forbidden("organization is not active")
	}
	member, err := u.repo.OrgMembership(ctx, orgID, principalID)
	if err != nil {
		return domain.Org{}, domain.OrgMember{}, err
	}
	if !member.IsUsable() {
		return domain.Org{}, domain.OrgMember{}, domainerr.Forbidden("organization membership is not active")
	}
	return org, member, nil
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

func (u *UseCases) normalizeLifecycleMutation(ctx context.Context, input domain.LifecycleInput) (domain.NormalizedLifecycleInput, domain.Product, error) {
	normalized, err := domain.NormalizeLifecycleInput(input)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Product{}, err
	}
	product, err := u.repo.ProductByID(ctx, normalized.ProductID)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Product{}, err
	}
	if err := u.requireProductMutator(ctx, product.ID, normalized.PrincipalID); err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Product{}, err
	}
	return normalized, product, nil
}

func (u *UseCases) requireProductMutator(ctx context.Context, productID uuid.UUID, principalID string) error {
	member, err := u.repo.OrgMembershipForProduct(ctx, productID, principalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domainerr.Forbidden("principal is not a member of the requested organization")
		}
		return err
	}
	if !member.IsUsable() {
		return domainerr.Forbidden("product membership is not active")
	}
	if !domain.CanMutateProduct(member.Role) {
		return domainerr.Forbidden("principal cannot mutate products")
	}
	return nil
}

func (u *UseCases) requireOrgMutator(ctx context.Context, orgID, principalID string) error {
	parsedOrgID, err := domain.ParseOrgID(orgID)
	if err != nil {
		return err
	}
	member, err := u.repo.OrgMembership(ctx, parsedOrgID, principalID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return domainerr.Forbidden("principal is not a member of the requested organization")
		}
		return err
	}
	if !member.IsUsable() {
		return domainerr.Forbidden("organization membership is not active")
	}
	if domain.CanMutateProduct(member.Role) {
		return nil
	}
	return domainerr.Forbidden("principal cannot mutate products for this organization")
}

func (u *UseCases) ensureCanDeactivate(ctx context.Context, product domain.Product, principalID string) error {
	if product.State() != domain.StateActive {
		return domainerr.Conflict("product must be active")
	}
	activeProducts, err := u.activeProductsForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	if len(activeProducts) <= 1 {
		return domainerr.Conflict("cannot deactivate the last active product for the principal")
	}
	return nil
}

func (u *UseCases) activeProductsForPrincipal(ctx context.Context, principalID string) ([]domain.Product, error) {
	return u.repo.ListForPrincipalLifecycle(ctx, principalID, domain.StateActive)
}
