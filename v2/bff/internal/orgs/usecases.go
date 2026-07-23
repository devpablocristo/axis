package orgs

import (
	"context"
	"strings"

	"github.com/devpablocristo/bff-v2/internal/identity"
	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/orgs/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
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
	HasProducts(ctx context.Context, orgID string) (bool, error)
}

type OrganizationAccessPort interface {
	ListForPrincipal(ctx context.Context, userID string) ([]productdomain.Product, error)
	ResolveAccess(ctx context.Context, productID, principalID string) (productdomain.Product, productdomain.OrgMember, error)
}

type IdentityPort interface {
	Get(ctx context.Context, id string) (identitydomain.User, error)
}

type UseCases struct {
	repo        RepositoryPort
	products    OrganizationAccessPort
	orgProvider identity.OrgProviderPort
	identity    IdentityPort
}

func NewUseCases(repo RepositoryPort, products OrganizationAccessPort, orgProvider identity.OrgProviderPort, identities ...IdentityPort) *UseCases {
	var identityPort IdentityPort
	if len(identities) > 0 {
		identityPort = identities[0]
	}
	return &UseCases{repo: repo, products: products, orgProvider: orgProvider, identity: identityPort}
}

func (u *UseCases) List(ctx context.Context, input domain.ListInput) ([]domain.Org, error) {
	normalized, err := domain.NormalizeListInput(input)
	if err != nil {
		return nil, err
	}
	if u.identity != nil && u.orgProvider != nil {
		principal, identityErr := u.identity.Get(ctx, normalized.PrincipalID)
		if identityErr != nil {
			return nil, identityErr
		}
		if principal.Provider == identitydomain.ProviderClerk {
			return u.listFromProvider(ctx, normalized, principal.ProviderUserID)
		}
	}
	products, err := u.products.ListForPrincipal(ctx, normalized.PrincipalID)
	if err != nil {
		return nil, err
	}
	items, err := u.repo.List(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if hasOwnerAccess(ctx, u.products, normalized.PrincipalID, products) {
		return items, nil
	}
	return filterOrgsForProducts(items, products), nil
}

func (u *UseCases) listFromProvider(ctx context.Context, input domain.NormalizedListInput, providerUserID string) ([]domain.Org, error) {
	memberships, err := u.orgProvider.ListUserOrgMemberships(ctx, providerUserID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Org, 0, len(memberships))
	for _, membership := range memberships {
		item, ensureErr := u.repo.Ensure(ctx, ensureFromProvider(membership.Org))
		if ensureErr != nil {
			return nil, ensureErr
		}
		if item.State() == input.Lifecycle {
			out = append(out, item)
		}
	}
	return out, nil
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
	if err := u.ensureOrgHasNoProducts(ctx, org.ID); err != nil {
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
	if err := u.ensureOrgHasNoProducts(ctx, org.ID); err != nil {
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
	if err := u.ensureOrgHasNoProducts(ctx, org.ID); err != nil {
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
	if u.products == nil {
		return domainerr.Unavailable("products is not configured")
	}
	products, err := u.products.ListForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	if hasOwnerAccess(ctx, u.products, principalID, products) {
		return nil
	}
	return domainerr.Forbidden("principal must be owner")
}

func (u *UseCases) requireOrgMutator(ctx context.Context, principalID, orgID string) error {
	if u.products == nil {
		return domainerr.Unavailable("products is not configured")
	}
	products, err := u.products.ListForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	if hasOwnerAccess(ctx, u.products, principalID, products) {
		return nil
	}
	for _, product := range products {
		if product.OrgID != orgID {
			continue
		}
		_, member, err := u.products.ResolveAccess(ctx, product.ID.String(), principalID)
		if err != nil {
			return err
		}
		if productdomain.CanMutateProduct(member.Role) {
			return nil
		}
	}
	return domainerr.Forbidden("principal cannot mutate orgs")
}

func hasOwnerAccess(ctx context.Context, productAccess OrganizationAccessPort, principalID string, products []productdomain.Product) bool {
	for _, product := range products {
		_, member, err := productAccess.ResolveAccess(ctx, product.ID.String(), principalID)
		if err != nil {
			continue
		}
		if productdomain.NormalizeRole(member.Role) == productdomain.RoleOwner {
			return true
		}
	}
	return false
}

func filterOrgsForProducts(items []domain.Org, products []productdomain.Product) []domain.Org {
	allowed := make(map[string]struct{}, len(products))
	for _, product := range products {
		allowed[product.OrgID] = struct{}{}
	}
	out := make([]domain.Org, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func (u *UseCases) ensureOrgHasNoProducts(ctx context.Context, orgID string) error {
	inUse, err := u.repo.HasProducts(ctx, orgID)
	if err != nil {
		return err
	}
	if inUse {
		return domainerr.Conflict("org is used by existing products")
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
