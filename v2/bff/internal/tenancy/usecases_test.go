package tenancy

import (
	"context"
	"testing"
	"time"

	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCreateTenantIsIdempotentByOrgAndProduct(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()

	first, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          " " + org.ID + " ",
		ProductSurface: " Axis ",
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same tenant id, got %s and %s", first.ID, second.ID)
	}
	if second.ProductSurface != "axis" || second.OrgID != org.ID {
		t.Fatalf("unexpected normalized tenant: %+v", second)
	}
}

func TestResolveAccessRequiresMembership(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	userID := uuid.NewString()
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), userID); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden without membership, got %v", err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   userID,
		Role:     "admin",
	}); err != nil {
		t.Fatal(err)
	}
	resolved, member, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), userID)
	if err != nil {
		t.Fatalf("ResolveAccess: %v", err)
	}
	if resolved.ID != tenant.ID || member.Role != "admin" {
		t.Fatalf("unexpected resolved context: tenant=%+v member=%+v", resolved, member)
	}
}

func TestResolveAccessRejectsArchivedTenant(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	userID := uuid.NewString()
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   userID,
		Role:     "admin",
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	tenant.ArchivedAt = &at
	repo.tenants[tenant.ID] = tenant

	if _, _, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), userID); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for archived tenant, got %v", err)
	}
}

func TestResolveAccessRejectsTrashedMembership(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	userID := uuid.NewString()
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   userID,
		Role:     "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	member.TrashedAt = &at
	repo.members[tenant.ID.String()+"|"+userID] = member

	if _, _, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), userID); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for trashed membership, got %v", err)
	}
}

func TestCreateTenantRequiresOrgMutatorWhenPrincipalProvided(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	memberID := uuid.NewString()
	seeded, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: seeded.ID.String(),
		UserID:   memberID,
		Role:     domain.RoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "ponti",
		PrincipalID:    memberID,
		OwnerUserID:    memberID,
	})
	if !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for member tenant create, got %v", err)
	}
}

func TestAdminCreatesTenantAndBecomesOwner(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	adminID := uuid.NewString()
	seeded, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: seeded.ID.String(),
		UserID:   adminID,
		Role:     domain.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "ponti",
		PrincipalID:    adminID,
		OwnerUserID:    adminID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	member, err := repo.TenantMembership(context.Background(), created.ID, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != domain.RoleOwner {
		t.Fatalf("expected owner membership, got %+v", member)
	}
}

func TestCreateTenantWithOrgNameCreatesProviderOrgAndOwnerMembership(t *testing.T) {
	repo := newFakeTenantRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	ownerID := uuid.NewString()

	created, err := uc.Create(context.Background(), domain.CreateTenantInput{
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
		t.Fatalf("unexpected tenant: %+v", created)
	}
	member, err := repo.TenantMembership(context.Background(), created.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != domain.RoleOwner {
		t.Fatalf("expected owner membership, got %+v", member)
	}
}

func TestUpdateTenantUpdatesProviderOrgNameOnly(t *testing.T) {
	repo := newFakeTenantRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	org := repo.seedOrg()
	adminID := uuid.NewString()
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   adminID,
		Role:     domain.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := uc.Update(context.Background(), domain.UpdateTenantInput{
		TenantID:    tenant.ID.String(),
		OrgName:     "New Org Name",
		PrincipalID: adminID,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if provider.updatedProviderOrgID != org.ProviderOrgID {
		t.Fatalf("expected provider org id %q, got %q", org.ProviderOrgID, provider.updatedProviderOrgID)
	}
	if updated.ProductSurface != tenant.ProductSurface {
		t.Fatalf("product surface should stay immutable, got %+v", updated)
	}
	if updated.OrgName != "New Org Name" {
		t.Fatalf("expected updated org name, got %+v", updated)
	}
}

func TestTenantLifecycleAndLastActiveGuard(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	org := repo.seedOrg()
	adminID := uuid.NewString()
	first, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: first.ID.String(),
		UserID:   adminID,
		Role:     domain.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), domain.LifecycleInput{TenantID: first.ID.String(), PrincipalID: adminID}); !domainerr.IsConflict(err) {
		t.Fatalf("expected last active guard, got %v", err)
	}
	second, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "ponti",
		PrincipalID:    adminID,
		OwnerUserID:    adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	archived, err := uc.List(context.Background(), domain.ListInput{PrincipalID: adminID, Lifecycle: domain.StateArchived})
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].ID != second.ID {
		t.Fatalf("expected archived second tenant, got %+v", archived)
	}
	if err := uc.Unarchive(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	trashed, err := uc.List(context.Background(), domain.ListInput{PrincipalID: adminID, Lifecycle: "trash"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || trashed[0].ID != second.ID {
		t.Fatalf("expected trashed second tenant, got %+v", trashed)
	}
	if err := uc.Restore(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Trash again: %v", err)
	}
	if err := uc.Purge(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: adminID}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := repo.TenantByID(context.Background(), second.ID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected tenant purged, got %v", err)
	}
}

func TestPurgeTrashedTenantUsesTargetTenantMembership(t *testing.T) {
	repo := newFakeTenantRepo()
	provider := &fakeOrgProvider{}
	uc := NewUseCases(repo, provider)
	ownerID := uuid.NewString()
	defaultOrg := repo.seedOrg()
	defaultTenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          defaultOrg.ID,
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: defaultTenant.ID.String(),
		UserID:   ownerID,
		Role:     domain.RoleOwner,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgName:        "Only Product Org",
		ProductSurface: "axis",
		PrincipalID:    ownerID,
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{TenantID: created.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if err := uc.Purge(context.Background(), domain.LifecycleInput{TenantID: created.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Purge should use target tenant membership: %v", err)
	}
	if provider.deletedProviderOrgID != "org_provider_created" {
		t.Fatalf("expected provider org delete, got %q", provider.deletedProviderOrgID)
	}
	if _, err := repo.OrgByID(context.Background(), created.OrgID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected local org mirror deleted, got %v", err)
	}
}

func TestPurgeTenantKeepsProviderOrgWhenAnotherProductTenantExists(t *testing.T) {
	repo := newFakeTenantRepo()
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
	first, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "axis",
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          org.ID,
		ProductSurface: "ponti",
		OwnerUserID:    ownerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Trash(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if err := uc.Purge(context.Background(), domain.LifecycleInput{TenantID: second.ID.String(), PrincipalID: ownerID}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if provider.deletedProviderOrgID != "" {
		t.Fatalf("did not expect provider org delete while another tenant exists, got %q", provider.deletedProviderOrgID)
	}
	if _, err := repo.OrgByID(context.Background(), org.ID); err != nil {
		t.Fatalf("expected org mirror kept: %v", err)
	}
	if _, err := repo.TenantByID(context.Background(), first.ID); err != nil {
		t.Fatalf("expected other tenant kept: %v", err)
	}
}

func TestProductsCatalog(t *testing.T) {
	products, err := NewUseCases(newFakeTenantRepo()).Products(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 5 || products[0].ProductSurface != "axis" {
		t.Fatalf("unexpected product catalog: %+v", products)
	}
}

type fakeTenantRepo struct {
	orgs    map[string]domain.Org
	tenants map[uuid.UUID]domain.Tenant
	members map[string]domain.TenantMember
}

func newFakeTenantRepo() *fakeTenantRepo {
	return &fakeTenantRepo{
		orgs:    map[string]domain.Org{},
		tenants: map[uuid.UUID]domain.Tenant{},
		members: map[string]domain.TenantMember{},
	}
}

func (r *fakeTenantRepo) EnsureOrg(_ context.Context, input domain.EnsureOrgInput) (domain.Org, error) {
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

func (r *fakeTenantRepo) seedOrg() domain.Org {
	org, _ := r.EnsureOrg(context.Background(), domain.EnsureOrgInput{
		OrgID:         uuid.NewString(),
		Provider:      "dev",
		ProviderOrgID: "dev-org",
		Name:          "Dev Org",
	})
	return org
}

func (r *fakeTenantRepo) OrgByID(_ context.Context, id string) (domain.Org, error) {
	org, ok := r.orgs[id]
	if !ok {
		return domain.Org{}, domainerr.NotFound("org not found")
	}
	return org, nil
}

func (r *fakeTenantRepo) OrgByProvider(_ context.Context, provider, providerOrgID string) (domain.Org, error) {
	for _, org := range r.orgs {
		if org.Provider == provider && org.ProviderOrgID == providerOrgID {
			return org, nil
		}
	}
	return domain.Org{}, domainerr.NotFound("org not found")
}

func (r *fakeTenantRepo) DeleteOrg(_ context.Context, id string) error {
	if _, ok := r.orgs[id]; !ok {
		return domainerr.NotFound("org not found")
	}
	delete(r.orgs, id)
	return nil
}

func (r *fakeTenantRepo) CreateTenant(_ context.Context, input domain.NormalizedCreateTenantInput) (domain.Tenant, error) {
	for _, tenant := range r.tenants {
		if tenant.OrgID == input.OrgID && tenant.ProductSurface == input.ProductSurface {
			tenant.UpdatedAt = time.Now().UTC()
			r.tenants[tenant.ID] = tenant
			return tenant, nil
		}
	}
	now := time.Now().UTC()
	tenant := domain.Tenant{
		ID:             uuid.New(),
		OrgID:          input.OrgID,
		OrgName:        r.orgs[input.OrgID].Name,
		ProductSurface: input.ProductSurface,
		Status:         domain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.tenants[tenant.ID] = tenant
	return tenant, nil
}

func (r *fakeTenantRepo) HasOtherOrgTenants(_ context.Context, orgID string, excludedTenantID uuid.UUID) (bool, error) {
	for id, tenant := range r.tenants {
		if id != excludedTenantID && tenant.OrgID == orgID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeTenantRepo) TenantByID(_ context.Context, id uuid.UUID) (domain.Tenant, error) {
	tenant, ok := r.tenants[id]
	if !ok {
		return domain.Tenant{}, domainerr.NotFoundf("tenant", id.String())
	}
	if org, ok := r.orgs[tenant.OrgID]; ok {
		tenant.OrgName = org.Name
	}
	return tenant, nil
}

func (r *fakeTenantRepo) ListForPrincipal(_ context.Context, userID string) ([]domain.Tenant, error) {
	return r.ListForPrincipalLifecycle(context.Background(), userID, domain.StateActive)
}

func (r *fakeTenantRepo) ListForPrincipalLifecycle(_ context.Context, userID, lifecycle string) ([]domain.Tenant, error) {
	out := []domain.Tenant{}
	for _, member := range r.members {
		if member.UserID == userID && member.IsUsable() {
			if tenant, ok := r.tenants[member.TenantID]; ok && tenant.State() == domain.NormalizeState(lifecycle) {
				if org, ok := r.orgs[tenant.OrgID]; ok {
					tenant.OrgName = org.Name
				}
				out = append(out, tenant)
			}
		}
	}
	return out, nil
}

func (r *fakeTenantRepo) List(_ context.Context, orgID string) ([]domain.Tenant, error) {
	out := []domain.Tenant{}
	for _, tenant := range r.tenants {
		if orgID == "" || tenant.OrgID == orgID {
			if org, ok := r.orgs[tenant.OrgID]; ok {
				tenant.OrgName = org.Name
			}
			out = append(out, tenant)
		}
	}
	return out, nil
}

func (r *fakeTenantRepo) UpsertMember(_ context.Context, input domain.NormalizedAddMemberInput) (domain.TenantMember, error) {
	now := time.Now().UTC()
	member := domain.TenantMember{
		TenantID:  input.TenantID,
		UserID:    input.UserID,
		Role:      input.Role,
		Status:    domain.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.members[input.TenantID.String()+"|"+input.UserID] = member
	return member, nil
}

func (r *fakeTenantRepo) ArchiveTenant(_ context.Context, id uuid.UUID, at time.Time) error {
	tenant, ok := r.tenants[id]
	if !ok {
		return domainerr.NotFound("tenant not found")
	}
	if tenant.State() != domain.StateActive {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	tenant.ArchivedAt = &at
	tenant.TrashedAt = nil
	tenant.PurgeAfter = nil
	tenant.UpdatedAt = at
	r.tenants[id] = tenant
	return nil
}

func (r *fakeTenantRepo) UnarchiveTenant(_ context.Context, id uuid.UUID) error {
	tenant, ok := r.tenants[id]
	if !ok {
		return domainerr.NotFound("tenant not found")
	}
	if tenant.State() != domain.StateArchived {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	now := time.Now().UTC()
	tenant.ArchivedAt = nil
	tenant.UpdatedAt = now
	r.tenants[id] = tenant
	return nil
}

func (r *fakeTenantRepo) TrashTenant(_ context.Context, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	tenant, ok := r.tenants[id]
	if !ok {
		return domainerr.NotFound("tenant not found")
	}
	if tenant.State() == domain.StateTrashed {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	tenant.ArchivedAt = nil
	tenant.TrashedAt = &at
	tenant.PurgeAfter = purgeAfter
	tenant.UpdatedAt = at
	r.tenants[id] = tenant
	return nil
}

func (r *fakeTenantRepo) RestoreTenant(_ context.Context, id uuid.UUID) error {
	tenant, ok := r.tenants[id]
	if !ok {
		return domainerr.NotFound("tenant not found")
	}
	if tenant.State() != domain.StateTrashed {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	now := time.Now().UTC()
	tenant.TrashedAt = nil
	tenant.PurgeAfter = nil
	tenant.UpdatedAt = now
	r.tenants[id] = tenant
	return nil
}

func (r *fakeTenantRepo) PurgeTenant(_ context.Context, id uuid.UUID) error {
	tenant, ok := r.tenants[id]
	if !ok {
		return domainerr.NotFound("tenant not found")
	}
	if tenant.State() != domain.StateTrashed {
		return domainerr.Conflict("invalid lifecycle transition")
	}
	delete(r.tenants, id)
	return nil
}

func (r *fakeTenantRepo) TenantMembership(_ context.Context, tenantID uuid.UUID, userID string) (domain.TenantMember, error) {
	member, ok := r.members[tenantID.String()+"|"+userID]
	if !ok {
		return domain.TenantMember{}, domainerr.NotFound("tenant membership not found")
	}
	return member, nil
}

func (r *fakeTenantRepo) DeactivateUserMemberships(context.Context, string) error {
	return nil
}

func (r *fakeTenantRepo) DeactivateOrgUserMemberships(context.Context, string, string) error {
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

func (f *fakeOrgProvider) EnsureOrgMembership(context.Context, string, string, string) error {
	return nil
}

func (f *fakeOrgProvider) DeleteOrgMembership(context.Context, string, string) error {
	return nil
}
