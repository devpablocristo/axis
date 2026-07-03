package products

import (
	"context"

	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Product, error)
	List(ctx context.Context, input domain.NormalizedListInput) ([]domain.Product, error)
	Create(ctx context.Context, input domain.NormalizedCreateInput) (domain.Product, error)
	Update(ctx context.Context, input domain.NormalizedUpdateInput) (domain.Product, error)
	Archive(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Unarchive(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Trash(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Restore(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Purge(ctx context.Context, input domain.NormalizedLifecycleInput) error
	IsProductInUse(ctx context.Context, productSurface string) (bool, error)
}

type TenancyPort interface {
	ListForPrincipal(ctx context.Context, userID string) ([]tenantdomain.Tenant, error)
	ResolveAccess(ctx context.Context, tenantID, principalID string) (tenantdomain.Tenant, tenantdomain.TenantMember, error)
}

type UseCases struct {
	repo    RepositoryPort
	tenancy TenancyPort
}

func NewUseCases(repo RepositoryPort, tenancy TenancyPort) *UseCases {
	return &UseCases{repo: repo, tenancy: tenancy}
}

func (u *UseCases) List(ctx context.Context, input domain.ListInput) ([]domain.Product, error) {
	return u.repo.List(ctx, domain.NormalizeListInput(input))
}

func (u *UseCases) Create(ctx context.Context, input domain.CreateInput) (domain.Product, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Product{}, err
	}
	if err := u.requireMutator(ctx, normalized.PrincipalID); err != nil {
		return domain.Product{}, err
	}
	return u.repo.Create(ctx, normalized)
}

func (u *UseCases) Update(ctx context.Context, input domain.UpdateInput) (domain.Product, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Product{}, err
	}
	if err := u.requireMutator(ctx, normalized.PrincipalID); err != nil {
		return domain.Product{}, err
	}
	return u.repo.Update(ctx, normalized)
}

func (u *UseCases) Archive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureProductIsUnused(ctx, product); err != nil {
		return err
	}
	return u.repo.Archive(ctx, normalized)
}

func (u *UseCases) Unarchive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if product.State() != domain.StateArchived {
		return domainerr.Conflict("product must be archived before unarchive")
	}
	return u.repo.Unarchive(ctx, normalized)
}

func (u *UseCases) Trash(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if err := u.ensureProductIsUnused(ctx, product); err != nil {
		return err
	}
	return u.repo.Trash(ctx, normalized)
}

func (u *UseCases) Restore(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if product.State() != domain.StateTrashed {
		return domainerr.Conflict("product must be trashed before restore")
	}
	return u.repo.Restore(ctx, normalized)
}

func (u *UseCases) Purge(ctx context.Context, input domain.LifecycleInput) error {
	normalized, product, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	if product.State() != domain.StateTrashed {
		return domainerr.Conflict("product must be trashed before purge")
	}
	if err := u.ensureProductIsUnused(ctx, product); err != nil {
		return err
	}
	return u.repo.Purge(ctx, normalized)
}

func (u *UseCases) normalizeLifecycleMutation(ctx context.Context, input domain.LifecycleInput) (domain.NormalizedLifecycleInput, domain.Product, error) {
	normalized, err := domain.NormalizeLifecycleInput(input)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Product{}, err
	}
	if err := u.requireMutator(ctx, normalized.PrincipalID); err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Product{}, err
	}
	product, err := u.repo.Get(ctx, normalized.ProductID)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, domain.Product{}, err
	}
	return normalized, product, nil
}

func (u *UseCases) requireMutator(ctx context.Context, principalID string) error {
	if u.tenancy == nil {
		return domainerr.Unavailable("tenancy is not configured")
	}
	tenants, err := u.tenancy.ListForPrincipal(ctx, principalID)
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		_, member, err := u.tenancy.ResolveAccess(ctx, tenant.ID.String(), principalID)
		if err != nil {
			return err
		}
		if tenantdomain.CanMutateTenant(member.Role) {
			return nil
		}
	}
	return domainerr.Forbidden("principal cannot mutate products")
}

func (u *UseCases) ensureProductIsUnused(ctx context.Context, product domain.Product) error {
	inUse, err := u.repo.IsProductInUse(ctx, product.ProductSurface)
	if err != nil {
		return err
	}
	if inUse {
		return domainerr.Conflict("product is used by existing tenants")
	}
	return nil
}
