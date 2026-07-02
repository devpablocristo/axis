package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeClerk records every path it is hit on and returns minimal valid bodies.
type fakeClerk struct {
	mu     sync.Mutex
	hits   []string
	server *httptest.Server
}

func newFakeClerk(t *testing.T, status int) *fakeClerk {
	t.Helper()
	f := &fakeClerk{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			w.Write([]byte(`{"errors":[{"code":"not_found"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users":
			w.Write([]byte(`{"id":"user_new","email_address":"new@co-a.com"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/organizations":
			w.Write([]byte(`{"data":[{"id":"org_FAKE","name":"cristo.tech","slug":"cristo-tech"}],"total_count":1}`))
		case strings.HasSuffix(r.URL.Path, "/memberships") && r.Method == http.MethodGet:
			w.Write([]byte(`{"data":[{"organization_id":"org_FAKE","role":"org:admin","public_user_data":{"user_id":"user_sync","identifier":"sync@co-a.com"}}],"total_count":1}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeClerk) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

func (f *fakeClerk) hit(substr string) bool {
	for _, h := range f.paths() {
		if strings.Contains(h, substr) {
			return true
		}
	}
	return false
}

// newClerkTestAdapter wires the adapter to the fake Clerk + a memory store that
// maps the Axis org "cristo.tech" to the Clerk org id "org_FAKE".
func newClerkTestAdapter(t *testing.T, f *fakeClerk) *clerkIdentityAdapter {
	t.Helper()
	store := newMemoryIAMStore()
	if _, err := store.CreateOrg(context.Background(), IAMOrg{ID: "cristo.tech", Provider: "clerk", ProviderOrgID: "org_FAKE", Name: "cristo.tech", Status: "active"}, ""); err != nil {
		t.Fatal(err)
	}
	return &clerkIdentityAdapter{secretKey: "sk_test_fake", baseURL: f.server.URL, client: &http.Client{}, store: store}
}

func TestClerkOrgIDResolvesProviderOrgID(t *testing.T) {
	a := newClerkTestAdapter(t, newFakeClerk(t, http.StatusOK))
	ctx := context.Background()
	if got := a.clerkOrgID(ctx, "cristo.tech"); got != "org_FAKE" {
		t.Fatalf("axis org id must resolve to clerk org id: got %q", got)
	}
	if got := a.clerkOrgID(ctx, "org_already"); got != "org_already" {
		t.Fatalf("a clerk org id must pass through: got %q", got)
	}
	if got := a.clerkOrgID(ctx, "unknown-org"); got != "unknown-org" {
		t.Fatalf("unknown org falls back to input: got %q", got)
	}
}

// TestClerkOrgScopedCallsUseProviderOrgID is the regression for the whole bug
// series: every org-scoped Clerk call must hit /organizations/org_FAKE, never
// the internal Axis id "cristo.tech".
func TestClerkOrgScopedCallsUseProviderOrgID(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	a := newClerkTestAdapter(t, f)
	ctx := context.Background()

	if _, err := a.UpsertMember(ctx, IAMMember{OrgID: "cristo.tech", UserID: "user_1", Role: "admin", Status: "active"}); err != nil {
		t.Fatalf("UpsertMember: %v", err)
	}
	if _, err := a.UpdateMember(ctx, "cristo.tech", "user_1", IAMMember{Role: "member", Status: "active"}); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	if err := a.DeleteMember(ctx, "cristo.tech", "user_1"); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	if err := a.SyncOrgMembers(ctx, "cristo.tech"); err != nil {
		t.Fatalf("SyncOrgMembers: %v", err)
	}
	if _, err := a.CreateUser(ctx, "cristo.tech", IAMUser{Email: "new@co-a.com", Role: "admin"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if f.hit("cristo.tech") {
		t.Fatalf("a Clerk call leaked the Axis org id; hits=%v", f.paths())
	}
	for _, want := range []string{
		"POST /organizations/org_FAKE/memberships",
		"PATCH /organizations/org_FAKE/memberships/user_1",
		"DELETE /organizations/org_FAKE/memberships/user_1",
		"GET /organizations/org_FAKE/memberships",
		"POST /users",
	} {
		if !f.hit(want) {
			t.Fatalf("expected Clerk call %q; hits=%v", want, f.paths())
		}
	}
}

// TestClerkSyncOrgMembersStoresUnderAxisOrgID: synced members are keyed by the
// Axis org id, not the Clerk org id from the response.
func TestClerkSyncOrgMembersStoresUnderAxisOrgID(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	a := newClerkTestAdapter(t, f)
	ctx := context.Background()
	if err := a.SyncOrgMembers(ctx, "cristo.tech"); err != nil {
		t.Fatalf("SyncOrgMembers: %v", err)
	}
	members, err := a.store.ListMembers(ctx, "cristo.tech")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].OrgID != "cristo.tech" {
		t.Fatalf("synced member must be stored under axis org id; got %#v", members)
	}
}

// TestClerkDeleteOrgTolerates404: an org missing in Clerk (404) must not block
// removing the local record (Axis is source of truth for tenancy).
func TestClerkDeleteOrgTolerates404(t *testing.T) {
	f := newFakeClerk(t, http.StatusNotFound)
	a := newClerkTestAdapter(t, f)
	ctx := context.Background()
	if err := a.DeleteOrg(ctx, "cristo.tech"); err != nil {
		t.Fatalf("DeleteOrg must tolerate Clerk 404, got: %v", err)
	}
	orgs, _ := a.store.ListOrgsForActor(ctx, "", true)
	for _, o := range orgs {
		if o.ID == "cristo.tech" {
			t.Fatalf("local org must be removed after purge")
		}
	}
}

// TestClerkPurgeDeletesIdentity: a hard delete (orgID="") must DELETE the user
// in the IdP (Clerk DELETE /users/{id}) and remove the local record.
func TestClerkPurgeDeletesIdentity(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	a := newClerkTestAdapter(t, f)
	ctx := context.Background()
	if _, err := a.store.CreateUser(ctx, IAMUser{ID: "user_del", Email: "del@co-a.com", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteUser(ctx, "", "user_del"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if !f.hit("DELETE /users/user_del") {
		t.Fatalf("purge must DELETE the Clerk user; hits=%v", f.paths())
	}
	users, _ := a.store.ListUsers(ctx)
	for _, u := range users {
		if u.ID == "user_del" {
			t.Fatalf("local identity must be removed after purge")
		}
	}
}

// TestClerkDeleteTolerates404: if the user/membership is already gone from Clerk
// (404), the local record is still removed (purge/remove not blocked).
func TestClerkDeleteTolerates404(t *testing.T) {
	f := newFakeClerk(t, http.StatusNotFound)
	a := newClerkTestAdapter(t, f)
	ctx := context.Background()
	if _, err := a.store.CreateUser(ctx, IAMUser{ID: "user_gone", Email: "gone@co-a.com", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.UpsertMember(ctx, IAMMember{OrgID: "cristo.tech", UserID: "user_m", Role: "member", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteUser(ctx, "", "user_gone"); err != nil {
		t.Fatalf("DeleteUser must tolerate Clerk 404, got: %v", err)
	}
	if err := a.DeleteMember(ctx, "cristo.tech", "user_m"); err != nil {
		t.Fatalf("DeleteMember must tolerate Clerk 404, got: %v", err)
	}
	users, _ := a.store.ListUsers(ctx)
	for _, u := range users {
		if u.ID == "user_gone" {
			t.Fatalf("local identity must be removed even when Clerk 404s")
		}
	}
}

func TestClerkAPIErrorCarriesStatus(t *testing.T) {
	err := &clerkAPIError{StatusCode: http.StatusNotFound}
	if clerkStatus(err) != http.StatusNotFound {
		t.Fatalf("clerkStatus must read the carried status")
	}
	if clerkStatus(context.Canceled) != 0 {
		t.Fatalf("non-clerk error must yield 0")
	}
}
