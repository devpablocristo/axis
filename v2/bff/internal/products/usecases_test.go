package products

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCreateProductDerivesSlug(t *testing.T) {
	repo := newFakeProductsRepo()
	uc := NewUseCases(repo, newFakeProductsTenancy(tenantdomain.RoleAdmin))

	product, err := uc.Create(context.Background(), domain.CreateInput{
		Name:        "Ponti CRM",
		PrincipalID: "principal",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if product.ProductSurface != "ponti-crm" || product.Name != "Ponti CRM" {
		t.Fatalf("unexpected product %+v", product)
	}
}

func TestCreateProductRejectsDuplicateSlug(t *testing.T) {
	repo := newFakeProductsRepo()
	uc := NewUseCases(repo, newFakeProductsTenancy(tenantdomain.RoleOwner))

	_, err := uc.Create(context.Background(), domain.CreateInput{
		Name:           "Axis",
		ProductSurface: "axis",
		PrincipalID:    "principal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Create(context.Background(), domain.CreateInput{
		Name:           "Axis Again",
		ProductSurface: "axis",
		PrincipalID:    "principal",
	}); !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestProductLifecycleBlocksWhenProductIsUsedByTenant(t *testing.T) {
	repo := newFakeProductsRepo()
	uc := NewUseCases(repo, newFakeProductsTenancy(tenantdomain.RoleAdmin))
	product, err := uc.Create(context.Background(), domain.CreateInput{
		Name:        "Ponti",
		PrincipalID: "principal",
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.inUse[product.ProductSurface] = true

	if err := uc.Archive(context.Background(), domain.LifecycleInput{
		ProductID:   product.ID.String(),
		PrincipalID: "principal",
	}); !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestProductMutationRequiresTenantMutator(t *testing.T) {
	repo := newFakeProductsRepo()
	uc := NewUseCases(repo, newFakeProductsTenancy(tenantdomain.RoleMember))

	_, err := uc.Create(context.Background(), domain.CreateInput{
		Name:        "Ponti",
		PrincipalID: "principal",
	})
	if !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

type fakeProductsRepo struct {
	products map[uuid.UUID]domain.Product
	inUse    map[string]bool
}

func newFakeProductsRepo() *fakeProductsRepo {
	return &fakeProductsRepo{
		products: map[uuid.UUID]domain.Product{},
		inUse:    map[string]bool{},
	}
}

func (r *fakeProductsRepo) Get(_ context.Context, id uuid.UUID) (domain.Product, error) {
	product, ok := r.products[id]
	if !ok {
		return domain.Product{}, domainerr.NotFound("product not found")
	}
	return product, nil
}

func (r *fakeProductsRepo) List(_ context.Context, input domain.NormalizedListInput) ([]domain.Product, error) {
	out := []domain.Product{}
	for _, product := range r.products {
		if product.State() == input.Lifecycle {
			out = append(out, product)
		}
	}
	return out, nil
}

func (r *fakeProductsRepo) Create(_ context.Context, input domain.NormalizedCreateInput) (domain.Product, error) {
	for _, product := range r.products {
		if product.ProductSurface == input.ProductSurface {
			return domain.Product{}, domainerr.Conflict("product_surface already exists")
		}
	}
	now := time.Now().UTC()
	product := domain.Product{
		ID:             uuid.New(),
		ProductSurface: input.ProductSurface,
		Name:           input.Name,
		Status:         domain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.products[product.ID] = product
	return product, nil
}

func (r *fakeProductsRepo) Update(_ context.Context, input domain.NormalizedUpdateInput) (domain.Product, error) {
	product, err := r.Get(context.Background(), input.ProductID)
	if err != nil {
		return domain.Product{}, err
	}
	product.Name = input.Name
	product.UpdatedAt = time.Now().UTC()
	r.products[product.ID] = product
	return product, nil
}

func (r *fakeProductsRepo) Archive(_ context.Context, input domain.NormalizedLifecycleInput) error {
	product, err := r.Get(context.Background(), input.ProductID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	product.ArchivedAt = &now
	product.TrashedAt = nil
	r.products[product.ID] = product
	return nil
}

func (r *fakeProductsRepo) Unarchive(_ context.Context, input domain.NormalizedLifecycleInput) error {
	product, err := r.Get(context.Background(), input.ProductID)
	if err != nil {
		return err
	}
	product.ArchivedAt = nil
	r.products[product.ID] = product
	return nil
}

func (r *fakeProductsRepo) Trash(_ context.Context, input domain.NormalizedLifecycleInput) error {
	product, err := r.Get(context.Background(), input.ProductID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	purgeAfter := now.Add(30 * 24 * time.Hour)
	product.ArchivedAt = nil
	product.TrashedAt = &now
	product.PurgeAfter = &purgeAfter
	r.products[product.ID] = product
	return nil
}

func (r *fakeProductsRepo) Restore(_ context.Context, input domain.NormalizedLifecycleInput) error {
	product, err := r.Get(context.Background(), input.ProductID)
	if err != nil {
		return err
	}
	product.TrashedAt = nil
	product.PurgeAfter = nil
	r.products[product.ID] = product
	return nil
}

func (r *fakeProductsRepo) Purge(_ context.Context, input domain.NormalizedLifecycleInput) error {
	delete(r.products, input.ProductID)
	return nil
}

func (r *fakeProductsRepo) IsProductInUse(_ context.Context, productSurface string) (bool, error) {
	return r.inUse[productSurface], nil
}

type fakeProductsTenancy struct {
	role string
}

func newFakeProductsTenancy(role string) *fakeProductsTenancy {
	return &fakeProductsTenancy{role: role}
}

func (f *fakeProductsTenancy) ListForPrincipal(_ context.Context, _ string) ([]tenantdomain.Tenant, error) {
	return []tenantdomain.Tenant{{ID: uuid.New(), Status: tenantdomain.StatusActive}}, nil
}

func (f *fakeProductsTenancy) ResolveAccess(_ context.Context, tenantID, principalID string) (tenantdomain.Tenant, tenantdomain.TenantMember, error) {
	id, _ := uuid.Parse(tenantID)
	return tenantdomain.Tenant{ID: id, Status: tenantdomain.StatusActive}, tenantdomain.TenantMember{
		TenantID: id,
		UserID:   principalID,
		Role:     f.role,
		Status:   tenantdomain.StatusActive,
	}, nil
}
