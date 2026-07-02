package main

import (
	"context"
	"errors"
	"testing"

	authn "github.com/devpablocristo/platform/authn/go"
)

// TestClerkWebhookOrgCreatedProvisionsAxisTenant: un org creado por webhook de
// Clerk nace con su tenant 'axis' (sin owner — el webhook no tiene actor).
func TestClerkWebhookOrgCreatedProvisionsAxisTenant(t *testing.T) {
	store := newMemoryIAMStore()
	a := &clerkIdentityAdapter{store: store}

	if err := a.HandleWebhook(context.Background(), "organization.created", map[string]any{"id": "org_x", "name": "X"}); err != nil {
		t.Fatal(err)
	}
	tenants, err := store.ListTenants(context.Background(), "org_x")
	if err != nil {
		t.Fatal(err)
	}
	for _, tn := range tenants {
		if tn.ProductSurface == defaultOrgProductSurface {
			return
		}
	}
	t.Fatalf("org should have its %q tenant, got %#v", defaultOrgProductSurface, tenants)
}

// TestClerkSyncPrincipalProvisionsAxisTenant: al loguear, el user resuelve el
// tenant 'axis' de su org (lo agrega como tenant-member con su org_role).
func TestClerkSyncPrincipalProvisionsAxisTenant(t *testing.T) {
	store := newMemoryIAMStore()
	a := &clerkIdentityAdapter{store: store}
	p := authn.Principal{Actor: "user_x", OrgID: "org_y", Claims: map[string]any{"email": "x@y.com", "org_role": "member"}}

	if err := a.SyncPrincipal(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	tenants, err := store.ResolveTenantsForUser(context.Background(), "user_x")
	if err != nil {
		t.Fatal(err)
	}
	for _, tn := range tenants {
		if tn.OrgID == "org_y" && tn.ProductSurface == defaultOrgProductSurface {
			return
		}
	}
	t.Fatalf("logged-in user should resolve org's %q tenant, got %#v", defaultOrgProductSurface, tenants)
}

// TestIsOwnerOrg locks in the role-model decision: the global owner role is
// granted only to accounts of AXIS_OWNER_ORG, matched by Axis id directly or via
// provider_org_id (the Clerk org id that arrives in the JWT claim).
func TestIsOwnerOrg(t *testing.T) {
	store := newMemoryIAMStore()
	if _, err := store.CreateOrg(context.Background(), IAMOrg{ID: "cristo.tech", ProviderOrgID: "org_clerk123", Name: "Cristo", Status: "active"}, ""); err != nil {
		t.Fatal(err)
	}
	a := &clerkIdentityAdapter{store: store, ownerOrg: "cristo.tech"}

	cases := []struct {
		claimOrg string
		want     bool
	}{
		{"cristo.tech", true},  // direct Axis id match
		{"org_clerk123", true}, // Clerk org id → resolved via provider_org_id
		{"org_other", false},   // a different org
		{"", false},            // no org
	}
	for _, c := range cases {
		if got := a.isOwnerOrg(context.Background(), c.claimOrg); got != c.want {
			t.Fatalf("isOwnerOrg(%q) = %v, want %v", c.claimOrg, got, c.want)
		}
	}

	// Owner org not configured → never an owner.
	none := &clerkIdentityAdapter{store: store, ownerOrg: ""}
	if none.isOwnerOrg(context.Background(), "cristo.tech") {
		t.Fatal("empty ownerOrg must never qualify")
	}
}

// updateMemberFailingIAMStore exercises the local IAM fallback path of
// updateIAMUser: UpdateMember reports the member is missing (errNotFound), so
// the code must fall back to UpsertMember. Here UpsertMember fails, and the
// returned error must surface to the caller.
type updateMemberFailingIAMStore struct {
	IAMStore
	upsertErr        error
	upsertMemberSeen bool
}

func (s *updateMemberFailingIAMStore) UpdateUser(_ context.Context, userID string, _ IAMUser) (IAMUser, error) {
	return IAMUser{ID: userID}, nil
}

func (s *updateMemberFailingIAMStore) UpdateMember(_ context.Context, _ string, _ string, _ IAMMember) (IAMMember, error) {
	return IAMMember{}, errNotFound
}

func (s *updateMemberFailingIAMStore) UpsertMember(_ context.Context, _ IAMMember) (IAMMember, error) {
	s.upsertMemberSeen = true
	return IAMMember{}, s.upsertErr
}

// TestUpdateIAMUserPropagatesUpsertMemberError guards against the swallowed
// error bug: when the identity provider is not configured (local IAM fallback)
// and the member does not yet exist, updateIAMUser must propagate a failure
// from the UpsertMember fallback instead of returning success.
func TestUpdateIAMUserPropagatesUpsertMemberError(t *testing.T) {
	wantErr := errors.New("upsert member boom")
	store := &updateMemberFailingIAMStore{upsertErr: wantErr}
	srv := &server{iam: store}

	_, err := srv.updateIAMUser(context.Background(), "org-1", "user-1", IAMUser{Role: "admin"})

	if !store.upsertMemberSeen {
		t.Fatalf("expected UpsertMember fallback to be invoked")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected upsert error to be propagated, got %v", err)
	}
}
