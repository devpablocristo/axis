package session

import (
	"context"
	"testing"
	"time"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/google/uuid"
)

func TestResolveEnsuresUserAndDefaultTenant(t *testing.T) {
	identity := &fakeSessionIdentity{}
	tenancy := &fakeSessionTenancy{}
	uc := NewUseCases(identity, tenancy, Defaults{
		PrincipalID:    "dev-user",
		PrincipalEmail: "dev@example.local",
		PrincipalName:  "Dev User",
		OrgID:          "dev-org",
	})

	out, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.PrincipalID != "dev-user" || out.OrgID != "dev-org" {
		t.Fatalf("unexpected session identity: %+v", out)
	}
	if identity.ensured.ID != "dev-user" {
		t.Fatalf("expected principal ensured, got %+v", identity.ensured)
	}
	if tenancy.defaultOrgID != "dev-org" || tenancy.defaultUserID != "dev-user" {
		t.Fatalf("expected default tenant ensure, got org=%q user=%q", tenancy.defaultOrgID, tenancy.defaultUserID)
	}
	if len(out.Tenants) != 1 || out.Tenants[0].ProductSurface != tenantdomain.DefaultProductSurface {
		t.Fatalf("expected default tenant in session, got %+v", out.Tenants)
	}
}

type fakeSessionIdentity struct {
	ensured userdomain.EnsureInput
}

func (f *fakeSessionIdentity) Ensure(_ context.Context, input userdomain.EnsureInput) (userdomain.User, error) {
	f.ensured = input
	now := time.Now().UTC()
	return userdomain.User{ID: input.ID, Email: input.Email, Name: input.Name, Status: userdomain.StatusActive, CreatedAt: now, UpdatedAt: now}, nil
}

type fakeSessionTenancy struct {
	defaultOrgID  string
	defaultUserID string
	tenant        tenantdomain.Tenant
}

func (f *fakeSessionTenancy) EnsureDefaultTenant(_ context.Context, orgID, _, userID string) (tenantdomain.Tenant, error) {
	f.defaultOrgID = orgID
	f.defaultUserID = userID
	now := time.Now().UTC()
	f.tenant = tenantdomain.Tenant{
		ID:             uuid.New(),
		OrgID:          orgID,
		ProductSurface: tenantdomain.DefaultProductSurface,
		Name:           orgID + " / " + tenantdomain.DefaultProductSurface,
		Status:         tenantdomain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return f.tenant, nil
}

func (f *fakeSessionTenancy) ListForPrincipal(context.Context, string) ([]tenantdomain.Tenant, error) {
	return []tenantdomain.Tenant{f.tenant}, nil
}
