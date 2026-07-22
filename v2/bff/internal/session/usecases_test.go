package session

import (
	"context"
	"testing"
	"time"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	"github.com/google/uuid"
)

func TestResolveEnsuresUserAndDefaultProduct(t *testing.T) {
	identity := &fakeSessionIdentity{}
	products := &fakeSessionOrganizationAccess{}
	uc := NewUseCases(identity, products, Defaults{
		PrincipalID:    "dev-user",
		PrincipalEmail: "dev@example.local",
		OrgID:          "dev-org",
	}, nil, nil)

	out, err := uc.Resolve(context.Background(), sessiondomain.ResolveInput{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.PrincipalID != identity.userID || out.OrgID != products.orgID {
		t.Fatalf("unexpected session identity: %+v", out)
	}
	if identity.ensured.ProviderUserID != "dev-user" {
		t.Fatalf("expected principal ensured, got %+v", identity.ensured)
	}
	if products.defaultOrgID != "dev-org" || products.defaultUserID != identity.userID {
		t.Fatalf("expected default product ensure, got org=%q user=%q", products.defaultOrgID, products.defaultUserID)
	}
	if len(out.Products) != 1 || out.Products[0].ProductSurface != productdomain.DefaultProductSurface {
		t.Fatalf("expected default product in session, got %+v", out.Products)
	}
}

func TestResolveClerkEnsuresProviderOrgMembershipForProduct(t *testing.T) {
	identity := &fakeSessionIdentity{}
	products := &fakeSessionOrganizationAccess{}
	uc := NewUseCases(identity, products, Defaults{}, &fakeTokenVerifier{
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
	if products.defaultOrgID != "org_clerk" || products.defaultUserID != identity.userID {
		t.Fatalf("expected clerk product membership ensure, got org=%q user=%q want user=%q", products.defaultOrgID, products.defaultUserID, identity.userID)
	}
}

func TestResolveClerkDoesNotFallBackToDevWithoutToken(t *testing.T) {
	identity := &fakeSessionIdentity{}
	products := &fakeSessionOrganizationAccess{}
	uc := NewUseCases(identity, products, Defaults{
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

func TestResolveClerkWithoutOrgUsesDefaultProduct(t *testing.T) {
	identity := &fakeSessionIdentity{}
	products := &fakeSessionOrganizationAccess{}
	uc := NewUseCases(identity, products, Defaults{OrgID: "dev-org"}, &fakeTokenVerifier{
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
	if products.defaultOrgID != "dev-org" || products.defaultUserID != identity.userID {
		t.Fatalf("expected default product membership ensure, got org=%q user=%q want user=%q", products.defaultOrgID, products.defaultUserID, identity.userID)
	}
	if len(out.Products) != 1 || out.Products[0].ProductSurface != productdomain.DefaultProductSurface {
		t.Fatalf("expected default product in session, got %+v", out.Products)
	}
}

func TestResolveClerkListsUserOrgMemberships(t *testing.T) {
	identity := &fakeSessionIdentity{}
	products := &fakeSessionOrganizationAccess{}
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
	uc := NewUseCases(identity, products, Defaults{}, &fakeTokenVerifier{
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
	if len(out.Products) != 2 {
		t.Fatalf("expected two Clerk org products, got %+v", out.Products)
	}
	if out.Products[0].OrgName != "Org A" || out.Products[1].OrgName != "Org B" {
		t.Fatalf("expected active Clerk org first, got %+v", out.Products)
	}
	if products.roles["org_a"] != productdomain.RoleAdmin || products.roles["org_b"] != productdomain.RoleMember {
		t.Fatalf("expected roles from Clerk memberships, got %+v", products.roles)
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

type fakeSessionOrganizationAccess struct {
	defaultOrgID  string
	defaultUserID string
	orgID         string
	product       productdomain.Product
	products      []productdomain.Product
	roles         map[string]string
}

func (f *fakeSessionOrganizationAccess) EnsureDefaultProduct(_ context.Context, orgID, _, userID string) (productdomain.Product, error) {
	f.defaultOrgID = orgID
	f.defaultUserID = userID
	now := time.Now().UTC()
	if f.orgID == "" {
		f.orgID = uuid.NewString()
	}
	f.product = productdomain.Product{
		ID:             uuid.New(),
		OrgID:          f.orgID,
		OrgName:        orgID,
		ProductSurface: productdomain.DefaultProductSurface,
		Status:         productdomain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return f.product, nil
}

func (f *fakeSessionOrganizationAccess) EnsureProviderDefaultProduct(_ context.Context, input productdomain.EnsureOrgInput, userID string) (productdomain.Product, error) {
	return f.EnsureDefaultProduct(context.Background(), input.ProviderOrgID, input.Name, userID)
}

func (f *fakeSessionOrganizationAccess) EnsureProviderDefaultProductWithRole(_ context.Context, input productdomain.EnsureOrgInput, userID, role string) (productdomain.Product, error) {
	f.defaultOrgID = input.ProviderOrgID
	f.defaultUserID = userID
	if f.roles == nil {
		f.roles = map[string]string{}
	}
	f.roles[input.ProviderOrgID] = role
	now := time.Now().UTC()
	product := productdomain.Product{
		ID:             uuid.New(),
		OrgID:          uuid.NewString(),
		OrgName:        input.Name,
		ProductSurface: productdomain.DefaultProductSurface,
		Status:         productdomain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	f.product = product
	f.products = append(f.products, product)
	return product, nil
}

func (f *fakeSessionOrganizationAccess) ListForPrincipal(context.Context, string) ([]productdomain.Product, error) {
	if len(f.products) > 0 {
		return f.products, nil
	}
	return []productdomain.Product{f.product}, nil
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
