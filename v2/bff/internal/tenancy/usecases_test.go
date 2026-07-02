package tenancy

import (
	"context"
	"testing"
	"time"

	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCreateTenantIsIdempotentByOrgAndProduct(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)

	first, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          " org-a ",
		ProductSurface: " Axis ",
		Name:           "Org A / Axis",
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          "org-a",
		ProductSurface: "axis",
		Name:           "Updated name",
	})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same tenant id, got %s and %s", first.ID, second.ID)
	}
	if second.ProductSurface != "axis" || second.OrgID != "org-a" {
		t.Fatalf("unexpected normalized tenant: %+v", second)
	}
}

func TestResolveAccessRequiresMembership(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          "org-a",
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), "user-a"); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden without membership, got %v", err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   "user-a",
		Role:     "admin",
	}); err != nil {
		t.Fatal(err)
	}
	resolved, member, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), "user-a")
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
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          "org-a",
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   "user-a",
		Role:     "admin",
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	tenant.ArchivedAt = &at
	repo.tenants[tenant.ID] = tenant

	if _, _, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), "user-a"); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for archived tenant, got %v", err)
	}
}

func TestResolveAccessRejectsTrashedMembership(t *testing.T) {
	repo := newFakeTenantRepo()
	uc := NewUseCases(repo)
	tenant, err := uc.Create(context.Background(), domain.CreateTenantInput{
		OrgID:          "org-a",
		ProductSurface: "axis",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := uc.AddMember(context.Background(), domain.AddMemberInput{
		TenantID: tenant.ID.String(),
		UserID:   "user-a",
		Role:     "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	member.TrashedAt = &at
	repo.members[tenant.ID.String()+"|user-a"] = member

	if _, _, err := uc.ResolveAccess(context.Background(), tenant.ID.String(), "user-a"); !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for trashed membership, got %v", err)
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
	org := domain.Org{ID: input.OrgID, Name: input.Name, Status: domain.StatusActive, CreatedAt: now, UpdatedAt: now}
	r.orgs[org.ID] = org
	return org, nil
}

func (r *fakeTenantRepo) CreateTenant(_ context.Context, input domain.NormalizedCreateTenantInput) (domain.Tenant, error) {
	for _, tenant := range r.tenants {
		if tenant.OrgID == input.OrgID && tenant.ProductSurface == input.ProductSurface {
			tenant.Name = input.Name
			tenant.UpdatedAt = time.Now().UTC()
			r.tenants[tenant.ID] = tenant
			return tenant, nil
		}
	}
	now := time.Now().UTC()
	tenant := domain.Tenant{
		ID:             uuid.New(),
		OrgID:          input.OrgID,
		ProductSurface: input.ProductSurface,
		Name:           input.Name,
		Status:         domain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.tenants[tenant.ID] = tenant
	return tenant, nil
}

func (r *fakeTenantRepo) TenantByID(_ context.Context, id uuid.UUID) (domain.Tenant, error) {
	tenant, ok := r.tenants[id]
	if !ok {
		return domain.Tenant{}, domainerr.NotFoundf("tenant", id.String())
	}
	return tenant, nil
}

func (r *fakeTenantRepo) ListForPrincipal(_ context.Context, userID string) ([]domain.Tenant, error) {
	out := []domain.Tenant{}
	for _, member := range r.members {
		if member.UserID == userID && member.IsUsable() {
			if tenant, ok := r.tenants[member.TenantID]; ok && tenant.IsUsable() {
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

func (r *fakeTenantRepo) TenantMembership(_ context.Context, tenantID uuid.UUID, userID string) (domain.TenantMember, error) {
	member, ok := r.members[tenantID.String()+"|"+userID]
	if !ok {
		return domain.TenantMember{}, domainerr.NotFound("tenant membership not found")
	}
	return member, nil
}
