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
		OrgID:          "dev-org",
	}, nil, nil)

	out, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.PrincipalID != identity.userID || out.OrgID != tenancy.orgID {
		t.Fatalf("unexpected session identity: %+v", out)
	}
	if identity.ensured.ProviderUserID != "dev-user" {
		t.Fatalf("expected principal ensured, got %+v", identity.ensured)
	}
	if tenancy.defaultOrgID != "dev-org" || tenancy.defaultUserID != identity.userID {
		t.Fatalf("expected default tenant ensure, got org=%q user=%q", tenancy.defaultOrgID, tenancy.defaultUserID)
	}
	if len(out.Tenants) != 1 || out.Tenants[0].ProductSurface != tenantdomain.DefaultProductSurface {
		t.Fatalf("expected default tenant in session, got %+v", out.Tenants)
	}
}

func TestResolveClerkEnsuresProviderTenantMembership(t *testing.T) {
	identity := &fakeSessionIdentity{}
	tenancy := &fakeSessionTenancy{}
	uc := NewUseCases(identity, tenancy, Defaults{}, &fakeTokenVerifier{
		claims: map[string]any{
			"sub":      "user_clerk",
			"email":    "clerk@example.com",
			"org_id":   "org_clerk",
			"org_name": "Clerk Org",
			"org_slug": "clerk-org",
		},
	}, nil)

	out, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{
		Authorization: "Bearer token",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.AuthMethod != "clerk" || out.User.Email != "clerk@example.com" {
		t.Fatalf("unexpected clerk session: %+v", out)
	}
	if tenancy.defaultOrgID != "org_clerk" || tenancy.defaultUserID != identity.userID {
		t.Fatalf("expected clerk tenant membership ensure, got org=%q user=%q want user=%q", tenancy.defaultOrgID, tenancy.defaultUserID, identity.userID)
	}
}

func TestResolveClerkDoesNotFallBackToDevWithoutToken(t *testing.T) {
	identity := &fakeSessionIdentity{}
	tenancy := &fakeSessionTenancy{}
	uc := NewUseCases(identity, tenancy, Defaults{
		PrincipalID:    "dev-user",
		PrincipalEmail: "dev@example.local",
		OrgID:          "dev-org",
	}, &fakeTokenVerifier{}, nil)

	if _, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{}); err == nil {
		t.Fatal("expected Clerk mode to reject a missing session token")
	}
	if identity.ensured.ProviderUserID != "" {
		t.Fatalf("Clerk mode must not ensure a dev identity, got %+v", identity.ensured)
	}
}

func TestResolveClerkWithoutOrgUsesDefaultTenant(t *testing.T) {
	identity := &fakeSessionIdentity{}
	tenancy := &fakeSessionTenancy{}
	uc := NewUseCases(identity, tenancy, Defaults{OrgID: "dev-org"}, &fakeTokenVerifier{
		claims: map[string]any{
			"sub":   "user_clerk",
			"email": "clerk@example.com",
		},
	}, nil)

	out, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{
		Authorization: "Bearer token",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.AuthMethod != "clerk" || out.User.Email != "clerk@example.com" {
		t.Fatalf("unexpected clerk session: %+v", out)
	}
	if tenancy.defaultOrgID != "dev-org" || tenancy.defaultUserID != identity.userID {
		t.Fatalf("expected default tenant membership ensure, got org=%q user=%q want user=%q", tenancy.defaultOrgID, tenancy.defaultUserID, identity.userID)
	}
	if len(out.Tenants) != 1 || out.Tenants[0].ProductSurface != tenantdomain.DefaultProductSurface {
		t.Fatalf("expected default tenant in session, got %+v", out.Tenants)
	}
}

func TestResolveClerkListsUserOrgMemberships(t *testing.T) {
	identity := &fakeSessionIdentity{}
	tenancy := &fakeSessionTenancy{}
	orgs := &fakeSessionOrgProvider{
		memberships: []userdomain.ProviderOrgMembership{
			{
				Org: userdomain.ProviderOrg{
					Provider:      userdomain.ProviderClerk,
					ProviderOrgID: "org_b",
					Name:          "Org B",
					Status:        userdomain.StatusActive,
				},
				Role: "org:member",
			},
			{
				Org: userdomain.ProviderOrg{
					Provider:      userdomain.ProviderClerk,
					ProviderOrgID: "org_a",
					Name:          "Org A",
					Status:        userdomain.StatusActive,
				},
				Role: "org:admin",
			},
		},
	}
	uc := NewUseCases(identity, tenancy, Defaults{}, &fakeTokenVerifier{
		claims: map[string]any{
			"sub":    "user_clerk",
			"email":  "clerk@example.com",
			"org_id": "org_a",
		},
	}, orgs)

	out, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{
		Authorization: "Bearer token",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := orgs.providerUserID; got != "user_clerk" {
		t.Fatalf("expected provider user lookup, got %q", got)
	}
	if len(out.Tenants) != 2 {
		t.Fatalf("expected two Clerk org tenants, got %+v", out.Tenants)
	}
	if out.Tenants[0].OrgName != "Org A" || out.Tenants[1].OrgName != "Org B" {
		t.Fatalf("expected active Clerk org first, got %+v", out.Tenants)
	}
	if tenancy.roles["org_a"] != tenantdomain.RoleAdmin || tenancy.roles["org_b"] != tenantdomain.RoleMember {
		t.Fatalf("expected roles from Clerk memberships, got %+v", tenancy.roles)
	}
}

type fakeSessionIdentity struct {
	ensured userdomain.EnsureInput
	userID  string
}

func (f *fakeSessionIdentity) Ensure(_ context.Context, input userdomain.EnsureInput) (userdomain.User, error) {
	f.ensured = input
	now := time.Now().UTC()
	if f.userID == "" {
		f.userID = uuid.NewString()
	}
	return userdomain.User{ID: f.userID, Provider: input.Provider, ProviderUserID: input.ProviderUserID, Email: input.Email, Status: userdomain.StatusActive, CreatedAt: now, UpdatedAt: now}, nil
}

type fakeSessionTenancy struct {
	defaultOrgID  string
	defaultUserID string
	orgID         string
	tenant        tenantdomain.Tenant
	tenants       []tenantdomain.Tenant
	roles         map[string]string
}

func (f *fakeSessionTenancy) EnsureDefaultTenant(_ context.Context, orgID, _, userID string) (tenantdomain.Tenant, error) {
	f.defaultOrgID = orgID
	f.defaultUserID = userID
	now := time.Now().UTC()
	if f.orgID == "" {
		f.orgID = uuid.NewString()
	}
	f.tenant = tenantdomain.Tenant{
		ID:             uuid.New(),
		OrgID:          f.orgID,
		OrgName:        orgID,
		ProductSurface: tenantdomain.DefaultProductSurface,
		Status:         tenantdomain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return f.tenant, nil
}

func (f *fakeSessionTenancy) EnsureProviderDefaultTenant(_ context.Context, input tenantdomain.EnsureOrgInput, userID string) (tenantdomain.Tenant, error) {
	return f.EnsureDefaultTenant(context.Background(), input.ProviderOrgID, input.Name, userID)
}

func (f *fakeSessionTenancy) EnsureProviderDefaultTenantWithRole(_ context.Context, input tenantdomain.EnsureOrgInput, userID, role string) (tenantdomain.Tenant, error) {
	f.defaultOrgID = input.ProviderOrgID
	f.defaultUserID = userID
	if f.roles == nil {
		f.roles = map[string]string{}
	}
	f.roles[input.ProviderOrgID] = role
	now := time.Now().UTC()
	tenant := tenantdomain.Tenant{
		ID:             uuid.New(),
		OrgID:          uuid.NewString(),
		OrgName:        input.Name,
		ProductSurface: tenantdomain.DefaultProductSurface,
		Status:         tenantdomain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	f.tenant = tenant
	f.tenants = append(f.tenants, tenant)
	return tenant, nil
}

func (f *fakeSessionTenancy) ListForPrincipal(context.Context, string) ([]tenantdomain.Tenant, error) {
	if len(f.tenants) > 0 {
		return f.tenants, nil
	}
	return []tenantdomain.Tenant{f.tenant}, nil
}

type fakeSessionOrgProvider struct {
	providerUserID string
	memberships    []userdomain.ProviderOrgMembership
}

func (f *fakeSessionOrgProvider) ListUserOrgMemberships(_ context.Context, providerUserID string) ([]userdomain.ProviderOrgMembership, error) {
	f.providerUserID = providerUserID
	return f.memberships, nil
}

type fakeTokenVerifier struct {
	claims map[string]any
}

func (f *fakeTokenVerifier) VerifyToken(context.Context, string) (map[string]any, error) {
	return f.claims, nil
}
