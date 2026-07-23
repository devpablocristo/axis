package products

import (
	"context"
	"testing"
	"time"

	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCreateProductIsIdempotentByOrgAndProduct(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()

	first, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          " " + org.ID + " ",
		ProductSurface: " Axis ",
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same product id, got %s and %s", first.ID, second.ID)
	}
	if second.ProductSurface != "axis" || second.OrgID != org.ID {
		t.Fatalf("unexpected normalized product: %+v", second)
	}
}

func TestResolveAccessRequiresMembership(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	userID := uuid.NewString()
	product, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := uc.ResolveAccess(context.Background(), product.ID.String(), userID); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden without membership, got %v", err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: product.ID.String(),
		UserID:    userID,
		Role:      "admin",
	}); err != nil {
		t.Fatal(err)
	}
	resolved, member, err := uc.ResolveAccess(context.Background(), product.ID.String(), userID)
	if err != nil {
		t.Fatalf("ResolveAccess: %v", err)
	}
	if resolved.ID != product.ID || member.Role != "admin" {
		t.Fatalf("unexpected resolved context: product=%+v member=%+v", resolved, member)
	}
}

func TestOwnerAccessIsScopedToTheirOrganization(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	ownerID := uuid.NewString()
	firstOrg := repo.seedOrg()
	secondOrg := repo.seedOrg()
	first, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          firstOrg.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          secondOrg.ID,
		ProductSurface: "productb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: first.ID.String(),
		UserID:    ownerID,
		Role:      "owner",
	}); err != nil {
		t.Fatal(err)
	}
	products, err := uc.ListForPrincipal(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("ListForPrincipal: %v", err)
	}
	if len(products) != 1 || products[0].ID != first.ID {
		t.Fatalf("owner should only see products in their organization, got %+v", products)
	}
	if _, _, err := uc.ResolveAccess(context.Background(), second.ID.String(), ownerID); !domainerr.IsForbidden(err) {
		t.Fatalf("owner must not access another organization's product, got %v", err)
	}
}

func TestOwnerCannotDeactivateAnotherOrganizationsProduct(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	ownerID := uuid.NewString()
	firstOrg := repo.seedOrg()
	secondOrg := repo.seedOrg()
	first, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          firstOrg.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          secondOrg.ID,
		ProductSurface: "productb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: first.ID.String(),
		UserID:    ownerID,
		Role:      "owner",
	}); err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), domain.LifecycleInput{
		ProductID:   second.ID.String(),
		PrincipalID: ownerID,
	}); !domainerr.IsForbidden(err) {
		t.Fatalf("owner must not archive another organization's product, got %v", err)
	}
}

func TestResolveAccessRejectsArchivedProduct(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	userID := uuid.NewString()
	product, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: product.ID.String(),
		UserID:    userID,
		Role:      "admin",
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	product.ArchivedAt = &at
	repo.products[product.ID] = product

	if _, _, err := uc.ResolveAccess(context.Background(), product.ID.String(), userID); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for archived product, got %v", err)
	}
}

func TestResolveAccessRejectsTrashedMembership(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	userID := uuid.NewString()
	product, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: product.ID.String(),
		UserID:    userID,
		Role:      "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	member.TrashedAt = &at
	repo.members[org.ID+"|"+userID] = member

	if _, _, err := uc.ResolveAccess(context.Background(), product.ID.String(), userID); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for trashed membership, got %v", err)
	}
}

func TestCreateProductRequiresOrgMutatorWhenPrincipalProvided(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	memberID := uuid.NewString()
	seeded, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: seeded.ID.String(),
		UserID:    memberID,
		Role:      domain.RoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "productb",
		PrincipalID:    memberID,
		OwnerUserID:    memberID,
	})
	if !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for member product create, got %v", err)
	}
}

func TestAdminCreatesProductAndBecomesOwner(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	adminID := uuid.NewString()
	seeded, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: seeded.ID.String(),
		UserID:    adminID,
		Role:      domain.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "productb",
		PrincipalID:    adminID,
		OwnerUserID:    adminID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	member, err := repo.OrgMembershipForProduct(context.Background(), created.ID, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != domain.RoleOwner {
		t.Fatalf("expected owner membership, got %+v", member)
	}
}

func TestCreateProductWithOrgNameCreatesProviderOrgAndOwnerMembership(t *testing.T) {
	repo := newFakeProductRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	ownerID := uuid.NewString()

	created, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgName:        "Cristo Tech",
		ProductSurface: "axis",
		PrincipalID:    ownerID,
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if provider.createdName != "Cristo Tech" {
		t.Fatalf("expected provider org creation, got %q", provider.createdName)
	}
	if created.OrgName != "Cristo Tech" || created.ProductSurface != "axis" {
		t.Fatalf("unexpected product: %+v", created)
	}
	member, err := repo.OrgMembershipForProduct(context.Background(), created.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != domain.RoleOwner {
		t.Fatalf("expected owner membership, got %+v", member)
	}
}

func TestUpdateProductUpdatesProviderOrgNameOnly(t *testing.T) {
	repo := newFakeProductRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	org := repo.seedOrg()
	adminID := uuid.NewString()
	product, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: product.ID.String(),
		UserID:    adminID,
		Role:      domain.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := uc.Update(context.Background(), domain.UpdateProductInput{
		ProductID:   product.ID.String(),
		OrgName:     "New Org Name",
		PrincipalID: adminID,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if provider.updatedProviderOrgID != org.ProviderOrgID {
		t.Fatalf("expected provider org id %q, got %q", org.ProviderOrgID, provider.updatedProviderOrgID)
	}
	if updated.ProductSurface != product.ProductSurface {
		t.Fatalf("product surface should stay immutable, got %+v", updated)
	}
	if updated.OrgName != "New Org Name" {
		t.Fatalf("expected updated org name, got %+v", updated)
	}
}

func TestProductLifecycleAndLastActiveGuard(t *testing.T) {
	repo := newFakeProductRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	adminID := uuid.NewString()
	first, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: first.ID.String(),
		UserID:    adminID,
		Role:      domain.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), domain.LifecycleInput{ProductID: first.ID.String(), PrincipalID: adminID}); !domainerr.IsConflict(err) {
		t.Fatalf("expected last active guard, got %v", err)
	}
	second, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "productb",
		PrincipalID:    adminID,
		OwnerUserID:    adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	archived, err := uc.List(context.Background(), domain.ListInput{PrincipalID: adminID, Lifecycle: domain.StateArchived})
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].ID != second.ID {
		t.Fatalf("expected archived second product, got %+v", archived)
	}
	if err := uc.Unarchive(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	trashed, err := uc.List(context.Background(), domain.ListInput{PrincipalID: adminID, Lifecycle: "trash"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || trashed[0].ID != second.ID {
		t.Fatalf("expected trashed second product, got %+v", trashed)
	}
	if err := uc.Restore(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Trash again: %v", err)
	}
	if err := uc.Purge(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := repo.ProductByID(context.Background(), second.ID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected product purged, got %v", err)
	}
}

func TestPurgeTrashedProductUsesTargetOrgMembershipForProduct(t *testing.T) {
	repo := newFakeProductRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	ownerID := uuid.NewString()
	defaultOrg := repo.seedOrg()
	defaultProduct, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          defaultOrg.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		ProductID: defaultProduct.ID.String(),
		UserID:    ownerID,
		Role:      domain.RoleOwner,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgName:        "Only Product Org",
		ProductSurface: "axis",
		PrincipalID:    ownerID,
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{ProductID: created.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if err := uc.Purge(context.Background(), domain.LifecycleInput{ProductID: created.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Purge should use target product membership: %v", err)
	}
	if provider.deletedProviderOrgID != "org_provider_created" {
		t.Fatalf("expected provider org delete, got %q", provider.deletedProviderOrgID)
	}
	if _, err := repo.OrgByID(context.Background(), created.OrgID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected local org mirror deleted, got %v", err)
	}
}

func TestPurgeProductKeepsProviderOrgWhenAnotherProductProductExists(t *testing.T) {
	repo := newFakeProductRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	ownerID := uuid.NewString()
	org, err := uc.EnsureOrg(context.Background(), domain.EnsureOrgInput{
		Provider:      "clerk",
		ProviderOrgID: "org_SHARED",
		Name:          "Shared Org",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Create(context.Background(), domain.CreateProductInput{
		OrgID:          org.ID,
		ProductSurface: "productb",
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if err := uc.Purge(context.Background(), domain.LifecycleInput{ProductID: second.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if provider.deletedProviderOrgID != "" {
		t.Fatalf("did not expect provider org delete while another product exists, got %q", provider.deletedProviderOrgID)
	}
	if _, err := repo.OrgByID(context.Background(), org.ID); err != nil {
		t.Fatalf("expected org mirror kept: %v", err)
	}
	if _, err := repo.ProductByID(context.Background(), first.ID); err != nil {
		t.Fatalf("expected other product kept: %v", err)
	}
}

type fakeProductRepo struct {
	orgs     map[string]domain.Org
	products map[uuid.UUID]domain.Product
	members  map[string]domain.OrgMember
}

func newFakeProductRepo() *fakeProductRepo {
	return &fakeProductRepo{
		orgs:     map[string]domain.Org{},
		products: map[uuid.UUID]domain.Product{},
		members:  map[string]domain.OrgMember{},
	}
}

func (r *fakeProductRepo) EnsureOrg(_ context.Context, input domain.EnsureOrgInput) (domain.Org, error) {
	now := time.Now().UTC()
	id := input.OrgID
	if id == "" {
		id = uuid.NewString()
	}
	org := domain.Org{
		ID:            id,
		Provider:      input.Provider,
		ProviderOrgID: input.ProviderOrgID,
		Name:          input.Name,
		Slug:          input.Slug,
		Status:        domain.StatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
		SyncedAt:      input.SyncedAt,
	}
	if existing, ok := r.orgs[org.ID]; ok {
		org.CreatedAt = existing.CreatedAt
	}
	r.orgs[org.ID] = org
	return org, nil
}

func (r *fakeProductRepo) seedOrg() domain.Org {
	org, _ := r.EnsureOrg(context.Background(), domain.EnsureOrgInput{
		OrgID:         uuid.NewString(),
		Provider:      "dev",
		ProviderOrgID: "dev-org",
		Name:          "Dev Org",
	})
	return org
}

func (r *fakeProductRepo) OrgByID(_ context.Context, id string) (domain.Org, error) {
	org, ok := r.orgs[id]
	if !ok {
		return domain.Org{}, domainerr.NotFound("org not found")
	}
	return org, nil
}

func (r *fakeProductRepo) OrgByProvider(_ context.Context, provider, providerOrgID string) (domain.Org, error) {
	for _, org := range r.orgs {
		if org.Provider == provider && org.ProviderOrgID == providerOrgID {
			return org, nil
		}
	}
	return domain.Org{}, domainerr.NotFound("org not found")
}

func (r *fakeProductRepo) DeleteOrg(_ context.Context, id string) error {
	if _, ok := r.orgs[id]; !ok {
		return domainerr.NotFound("org not found")
	}
	delete(r.orgs, id)
	return nil
}

func (r *fakeProductRepo) CreateProduct(_ context.Context, input domain.NormalizedCreateProductInput) (domain.Product, error) {
	for _, product := range r.products {
		if product.OrgID == input.OrgID && product.ProductSurface == input.ProductSurface {
			product.UpdatedAt = time.Now().UTC()
			r.products[product.ID] = product
			return product, nil
		}
	}
	now := time.Now().UTC()
	product := domain.Product{
		ID:             uuid.New(),
		OrgID:          input.OrgID,
		OrgName:        r.orgs[input.OrgID].Name,
		ProductSurface: input.ProductSurface,
		Status:         domain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.products[product.ID] = product
	return product, nil
}

func (r *fakeProductRepo) HasOtherOrgProducts(_ context.Context, orgID string, excludedProductID uuid.UUID) (bool, error) {
	for id, product := range r.products {
		if id != excludedProductID && product.OrgID == orgID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeProductRepo) ProductByID(_ context.Context, id uuid.UUID) (domain.Product, error) {
	product, ok := r.products[id]
	if !ok {
		return domain.Product{}, domainerr.NotFoundf("product", id.String())
	}
	if org, ok := r.orgs[product.OrgID]; ok {
		product.OrgName = org.Name
	}
	return product, nil
}

func (r *fakeProductRepo) ListForPrincipal(_ context.Context, userID string) ([]domain.Product, error) {
	return r.ListForPrincipalLifecycle(context.Background(), userID, domain.StateActive)
}

func (r *fakeProductRepo) ListLifecycle(_ context.Context, lifecycle string) ([]domain.Product, error) {
	out := []domain.Product{}
	for _, product := range r.products {
		if product.State() != domain.NormalizeState(lifecycle) {
			continue
		}
		if org, ok := r.orgs[product.OrgID]; ok {
			product.OrgName = org.Name
		}
		out = append(out, product)
	}
	return out, nil
}

func (r *fakeProductRepo) ListForPrincipalLifecycle(_ context.Context, userID, lifecycle string) ([]domain.Product, error) {
	out := []domain.Product{}
	for _, member := range r.members {
		if member.UserID == userID && member.IsUsable() {
			for _, product := range r.products {
				if product.OrgID != member.OrgID.String() || product.State() != domain.NormalizeState(lifecycle) {
					continue
				}
				if org, ok := r.orgs[product.OrgID]; ok {
					product.OrgName = org.Name
				}
				out = append(out, product)
			}
		}
	}
	return out, nil
}

func (r *fakeProductRepo) List(_ context.Context, orgID string) ([]domain.Product, error) {
	out := []domain.Product{}
	for _, product := range r.products {
		if orgID == "" || product.OrgID == orgID {
			if org, ok := r.orgs[product.OrgID]; ok {
				product.OrgName = org.Name
			}
			out = append(out, product)
		}
	}
	return out, nil
}

func (r *fakeProductRepo) UpsertMember(_ context.Context, input domain.NormalizedAddMemberInput) (domain.OrgMember, error) {
	now := time.Now().UTC()
	product := r.products[input.ProductID]
	orgID, _ := uuid.Parse(product.OrgID)
	member := domain.OrgMember{
		OrgID:     orgID,
		UserID:    input.UserID,
		Role:      input.Role,
		Status:    domain.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.members[orgID.String()+"|"+input.UserID] = member
	return member, nil
}

func (r *fakeProductRepo) ArchiveProduct(_ context.Context, id uuid.UUID, at time.Time) error {
	product, ok := r.products[id]
	if !ok {
		return domainerr.NotFound("product not found")
	}
	if product.State() != domain.StateActive {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	product.ArchivedAt = &at
	product.TrashedAt = nil
	product.PurgeAfter = nil
	product.UpdatedAt = at
	r.products[id] = product
	return nil
}

func (r *fakeProductRepo) UnarchiveProduct(_ context.Context, id uuid.UUID) error {
	product, ok := r.products[id]
	if !ok {
		return domainerr.NotFound("product not found")
	}
	if product.State() != domain.StateArchived {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	now := time.Now().UTC()
	product.ArchivedAt = nil
	product.UpdatedAt = now
	r.products[id] = product
	return nil
}

func (r *fakeProductRepo) TrashProduct(_ context.Context, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	product, ok := r.products[id]
	if !ok {
		return domainerr.NotFound("product not found")
	}
	if product.State() == domain.StateTrashed {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	product.ArchivedAt = nil
	product.TrashedAt = &at
	product.PurgeAfter = purgeAfter
	product.UpdatedAt = at
	r.products[id] = product
	return nil
}

func (r *fakeProductRepo) RestoreProduct(_ context.Context, id uuid.UUID) error {
	product, ok := r.products[id]
	if !ok {
		return domainerr.NotFound("product not found")
	}
	if product.State() != domain.StateTrashed {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	now := time.Now().UTC()
	product.TrashedAt = nil
	product.PurgeAfter = nil
	product.UpdatedAt = now
	r.products[id] = product
	return nil
}

func (r *fakeProductRepo) PurgeProduct(_ context.Context, id uuid.UUID) error {
	product, ok := r.products[id]
	if !ok {
		return domainerr.NotFound("product not found")
	}
	if product.State() != domain.StateTrashed {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	delete(r.products, id)
	return nil
}

func (r *fakeProductRepo) OrgMembershipForProduct(_ context.Context, productID uuid.UUID, userID string) (domain.OrgMember, error) {
	product, productOK := r.products[productID]
	if !productOK {
		return domain.OrgMember{}, domainerr.NotFound("product not found")
	}
	member, ok := r.members[product.OrgID+"|"+userID]
	if !ok {
		return domain.OrgMember{}, domainerr.NotFound("product membership not found")
	}
	return member, nil
}

func (r *fakeProductRepo) OrgMembership(_ context.Context, orgID uuid.UUID, userID string) (domain.OrgMember, error) {
	for _, member := range r.members {
		if member.OrgID == orgID && member.UserID == userID {
			return member, nil
		}
	}
	return domain.OrgMember{}, domainerr.NotFound("organization membership not found")
}

func (r *fakeProductRepo) PrincipalHasOwnerRole(_ context.Context, userID string) (bool, error) {
	for _, member := range r.members {
		if member.UserID == userID && member.Role == domain.RoleOwner && member.IsUsable() {
			for _, product := range r.products {
				if product.OrgID == member.OrgID.String() && product.IsUsable() {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (r *fakeProductRepo) DeactivateUserMemberships(context.Context, string) error {
	return nil
}

func (r *fakeProductRepo) DeactivateOrgUserMemberships(context.Context, string, string) error {
	return nil
}

type fakeOrgProvider struct {
	createdName          string
	updatedProviderOrgID string
	updatedName          string
	deletedProviderOrgID string
}

func (f *fakeOrgProvider) CreateOrg(_ context.Context, name string) (identitydomain.ProviderOrg, error) {
	f.createdName = name
	now := time.Now().UTC()
	return identitydomain.ProviderOrg{
		Provider:      identitydomain.ProviderClerk,
		ProviderOrgID: "org_provider_created",
		Name:          name,
		Slug:          "created",
		Status:        identitydomain.StatusActive,
		SyncedAt:      &now,
	}, nil
}

func (f *fakeOrgProvider) UpdateOrg(_ context.Context, providerOrgID, name string) (identitydomain.ProviderOrg, error) {
	f.updatedProviderOrgID = providerOrgID
	f.updatedName = name
	now := time.Now().UTC()
	return identitydomain.ProviderOrg{
		Provider:      identitydomain.ProviderClerk,
		ProviderOrgID: providerOrgID,
		Name:          name,
		Slug:          "updated",
		Status:        identitydomain.StatusActive,
		SyncedAt:      &now,
	}, nil
}

func (f *fakeOrgProvider) DeleteOrg(_ context.Context, providerOrgID string) error {
	f.deletedProviderOrgID = providerOrgID
	return nil
}

func (f *fakeOrgProvider) ListUserOrgMemberships(context.Context, string) ([]identitydomain.ProviderOrgMembership, error) {
	return nil, nil
}

func (f *fakeOrgProvider) ListOrganizationMemberships(context.Context, string) ([]identitydomain.ProviderOrgMembership, error) {
	return nil, nil
}

func (f *fakeOrgProvider) EnsureOrgMembership(context.Context, string, string, string) error {
	return nil
}

func (f *fakeOrgProvider) DeleteOrgMembership(context.Context, string, string) error {
	return nil
}
