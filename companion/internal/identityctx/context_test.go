package identityctx

import (
	"net/http/httptest"
	"testing"

	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

func TestFromRequestUsesBFFInternalJWTClaims(t *testing.T) {
	t.Parallel()

	principal := &authn.Principal{
		OrgID:      "org-a",
		Actor:      "user-a",
		Scopes:     []string{"companion:tasks:read", "companion:tasks:read"},
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_id":          "user-a",
			"actor_type":        "human",
			"product_surface":   "axis-console",
			"service_principal": true,
			"on_behalf_of":      "user-a",
		},
	}
	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = WithPrincipal(req, principal)

	got := FromRequest(req)
	if got.CustomerOrgID != "org-a" {
		t.Fatalf("customer org mismatch: %+v", got)
	}
	if got.HumanUserID != "user-a" || got.OnBehalfOf != "user-a" {
		t.Fatalf("human/on_behalf mismatch: %+v", got)
	}
	if got.ActorType != "human" || !got.ServicePrincipal {
		t.Fatalf("actor metadata mismatch: %+v", got)
	}
	if got.ProductSurface != "axis-console" {
		t.Fatalf("product surface mismatch: %+v", got)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "companion:tasks:read" {
		t.Fatalf("scopes not normalized: %+v", got.Scopes)
	}
}

func TestFromRequestFallsBackToCompatHeaders(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	req.Header.Set("X-Org-ID", "org-compat")
	req.Header.Set("X-User-ID", "user-compat")
	req.Header.Set("X-Product-Surface", "pymes")
	req.Header.Set("X-Auth-Scopes", "companion:tasks:read companion:tasks:read")

	got := FromRequest(req)
	if got.CustomerOrgID != "org-compat" || got.HumanUserID != "user-compat" {
		t.Fatalf("compat identity mismatch: %+v", got)
	}
	if got.ProductSurface != "pymes" {
		t.Fatalf("expected pymes surface, got %q", got.ProductSurface)
	}
	if got.EffectiveActorID() != "user-compat" {
		t.Fatalf("expected user actor, got %q", got.EffectiveActorID())
	}
}

func TestWorkIdentityForOrgUsesRequestedOrgWithCrossOrgScope(t *testing.T) {
	t.Parallel()

	principal := &authn.Principal{
		OrgID:  "org-a",
		Actor:  "axis-admin",
		Scopes: []string{"companion:cross_org"},
		Claims: map[string]any{
			"actor_type":        "service",
			"service_principal": true,
		},
	}
	req := httptest.NewRequest("GET", "/v1/chat?org_id=org-b", nil)
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = WithPrincipal(req, principal)

	got, ok := WorkIdentityForOrg(req, req.URL.Query().Get("org_id"), "companion:cross_org")
	if !ok {
		t.Fatal("expected identity")
	}
	if got.CustomerOrgID != "org-b" {
		t.Fatalf("expected requested org-b, got %+v", got)
	}
}

func TestFromRequestReadsScopeClaimsWhenPrincipalScopesAreEmpty(t *testing.T) {
	t.Parallel()

	principal := &authn.Principal{
		OrgID:      "org-a",
		Actor:      "user-a",
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_type": "human",
			"scp":        []any{"companion:tasks:read", "companion:memory:write companion:memory:write"},
		},
	}
	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = WithPrincipal(req, principal)

	got := FromRequest(req)
	want := []string{"companion:tasks:read", "companion:memory:write"}
	if len(got.Scopes) != len(want) {
		t.Fatalf("scope count mismatch: got %+v want %+v", got.Scopes, want)
	}
	for i := range want {
		if got.Scopes[i] != want[i] {
			t.Fatalf("scope mismatch: got %+v want %+v", got.Scopes, want)
		}
	}
	if !HasScope(req, "companion:memory:write") {
		t.Fatal("expected HasScope to use canonical scope claims")
	}
}

func TestFromRequestReadsActorIDClaim(t *testing.T) {
	t.Parallel()

	principal := &authn.Principal{
		OrgID:      "org-a",
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_id":   "user-from-claim",
			"actor_type": "human",
		},
	}
	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = WithPrincipal(req, principal)

	got := FromRequest(req)
	if got.HumanUserID != "user-from-claim" || got.EffectiveActorID() != "user-from-claim" {
		t.Fatalf("expected actor_id claim to drive human actor, got %+v", got)
	}
}

func TestWorkIdentityRequiresCustomerOrg(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	if _, ok := WorkIdentity(req); ok {
		t.Fatal("expected missing customer org to fail")
	}

	req.Header.Set("X-Org-ID", "org-a")
	got, ok := WorkIdentity(req)
	if !ok || got.CustomerOrgID != "org-a" {
		t.Fatalf("expected org-a work identity, got %+v ok=%v", got, ok)
	}
}

func TestCanActAsRequiresActorOrDelegationOrScope(t *testing.T) {
	t.Parallel()

	id := IdentityContext{
		CustomerOrgID:      "org-a",
		HumanUserID:        "user-a",
		CompanionPrincipal: CompanionPrincipal,
		OnBehalfOf:         "delegate-a",
		Scopes:             []string{"companion:tasks:write"},
	}
	if !id.CanActAs("user-a") || !id.CanActAs("delegate-a") {
		t.Fatalf("expected self/delegated actors to be allowed: %+v", id)
	}
	if id.CanActAs(CompanionPrincipal) {
		t.Fatal("expected human principal to be denied when claiming agent principal")
	}
	serviceID := IdentityContext{
		CustomerOrgID:      "org-a",
		CompanionPrincipal: CompanionPrincipal,
		ServicePrincipal:   true,
	}
	if !serviceID.CanActAs(CompanionPrincipal) {
		t.Fatalf("expected service principal to act as agent: %+v", serviceID)
	}
	if id.CanActAs("user-b") {
		t.Fatal("expected unrelated actor to be denied")
	}
	if !id.CanActAs("user-b", "companion:tasks:write") {
		t.Fatal("expected operator scope to allow alternate actor")
	}
}

func TestEffectiveActorFallsBackToCompanionPrincipal(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	req.Header.Set("X-Service-Principal", "true")

	got := FromRequest(req)
	if got.HumanUserID != "" {
		t.Fatalf("expected no human user, got %+v", got)
	}
	if got.EffectiveActorID() != CompanionPrincipal {
		t.Fatalf("expected companion principal fallback, got %q", got.EffectiveActorID())
	}
}
