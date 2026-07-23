package clerk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devpablocristo/bff-v2/internal/identity"
)

type fakeClerk struct {
	mu     sync.Mutex
	hits   []string
	status int
	server *httptest.Server
}

func newFakeClerk(t *testing.T, status int) *fakeClerk {
	t.Helper()
	f := &fakeClerk{status: status}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		if f.status != 0 && f.status != http.StatusOK {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"errors":[{"code":"not_found"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users":
			if r.URL.Query().Has("email_address[]") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"code":"invalid_query"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"user_123","first_name":"Ada","email_addresses":[{"id":"email_1","email_address":"ada@example.com"}],"primary_email_address_id":"email_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/users":
			_, _ = w.Write([]byte(`{"id":"user_created","email_address":"created@example.com"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/users/user_123/email_address":
			_, _ = w.Write([]byte(`{"id":"email_updated","email_address":"updated@example.com"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/users/user_123":
			_, _ = w.Write([]byte(`{"id":"user_123","email_addresses":[{"id":"email_updated","email_address":"updated@example.com"}],"primary_email_address_id":"email_updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/users/user_123":
			_, _ = w.Write([]byte(`{"id":"user_123","deleted":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/users/user_123/organization_memberships":
			_, _ = w.Write([]byte(`{"data":[{"role":"org:admin","organization":{"id":"org_PROVIDER","name":"Provider Org","slug":"provider-org"}}],"total_count":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/organization_memberships":
			_, _ = w.Write([]byte(`{"data":[{"role":"org:member","organization":{"id":"org_PROVIDER","name":"Provider Org","slug":"provider-org"},"public_user_data":{"user_id":"user_456","identifier":"member@example.com"}}],"total_count":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/organizations":
			_, _ = w.Write([]byte(`{"id":"org_CREATED","name":"Created Org","slug":"created-org"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/organizations/org_PROVIDER":
			_, _ = w.Write([]byte(`{"id":"org_PROVIDER","name":"Updated Org","slug":"updated-org"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/organizations/org_PROVIDER":
			_, _ = w.Write([]byte(`{"id":"org_PROVIDER","deleted":true}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/invitations"):
			_, _ = w.Write([]byte(`{"id":"invite_123","status":"pending"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeClerk) hit(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, hit := range f.hits {
		if strings.Contains(hit, substr) {
			return true
		}
	}
	return false
}

func (f *fakeClerk) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

func TestOrgScopedCallsUseProviderOrgID(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})
	ctx := context.Background()

	if err := provider.EnsureOrgMembership(ctx, "org_PROVIDER", "user_123", "admin"); err != nil {
		t.Fatalf("EnsureOrgMembership: %v", err)
	}
	if err := provider.DeleteOrgMembership(ctx, "org_PROVIDER", "user_123"); err != nil {
		t.Fatalf("DeleteOrgMembership: %v", err)
	}
	if _, err := provider.CreateOrgInvitation(ctx, identity.CreateOrgInvitationInput{
		ProviderOrgID: "org_PROVIDER",
		Email:         "new@example.com",
		Role:          "member",
	}); err != nil {
		t.Fatalf("CreateOrgInvitation: %v", err)
	}

	if f.hit("axis-org") {
		t.Fatalf("clerk call leaked axis org id; hits=%v", f.paths())
	}
	for _, want := range []string{
		"POST /organizations/org_PROVIDER/memberships",
		"DELETE /organizations/org_PROVIDER/memberships/user_123",
		"POST /organizations/org_PROVIDER/invitations",
	} {
		if !f.hit(want) {
			t.Fatalf("expected Clerk call %q; hits=%v", want, f.paths())
		}
	}
}

func TestFindUserByEmailMapsClerkUser(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	user, err := provider.FindUserByEmail(context.Background(), "ada@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if user.ProviderUserID != "user_123" || user.Email != "ada@example.com" {
		t.Fatalf("unexpected mapped user: %+v", user)
	}
}

func TestListOrganizationMembershipsMapsClerkDirectory(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	memberships, err := provider.ListOrganizationMemberships(context.Background(), "org_PROVIDER")
	if err != nil {
		t.Fatalf("ListOrganizationMemberships: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("expected one membership, got %+v", memberships)
	}
	got := memberships[0]
	if got.Org.ProviderOrgID != "org_PROVIDER" || got.User.ProviderUserID != "user_456" ||
		got.User.Email != "member@example.com" || got.Role != "member" {
		t.Fatalf("unexpected mapped membership: %+v", got)
	}
	if !f.hit("GET /organization_memberships?") || !f.hit("organization_id=org_PROVIDER") {
		t.Fatalf("expected Clerk organization directory endpoint; hits=%v", f.paths())
	}
}

func TestCreateOrgUsesClerkOrganizationsEndpoint(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	org, err := provider.CreateOrg(context.Background(), "Created Org")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if org.ProviderOrgID != "org_CREATED" || org.Name != "Created Org" {
		t.Fatalf("unexpected mapped org: %+v", org)
	}
	if !f.hit("POST /organizations") {
		t.Fatalf("expected Clerk organizations create endpoint; hits=%v", f.paths())
	}
}

func TestUpdateOrgUsesProviderOrgID(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	org, err := provider.UpdateOrg(context.Background(), "org_PROVIDER", "Updated Org")
	if err != nil {
		t.Fatalf("UpdateOrg: %v", err)
	}
	if org.ProviderOrgID != "org_PROVIDER" || org.Name != "Updated Org" {
		t.Fatalf("unexpected mapped org: %+v", org)
	}
	if !f.hit("PATCH /organizations/org_PROVIDER") {
		t.Fatalf("expected Clerk organizations update endpoint; hits=%v", f.paths())
	}
	if f.hit("axis-org") {
		t.Fatalf("clerk call leaked axis org id; hits=%v", f.paths())
	}
}

func TestDeleteOrgUsesProviderOrgID(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	if err := provider.DeleteOrg(context.Background(), "org_PROVIDER"); err != nil {
		t.Fatalf("DeleteOrg: %v", err)
	}
	if !f.hit("DELETE /organizations/org_PROVIDER") {
		t.Fatalf("expected Clerk organizations delete endpoint; hits=%v", f.paths())
	}
	if f.hit("axis-org") {
		t.Fatalf("clerk call leaked axis org id; hits=%v", f.paths())
	}
}

func TestFindUserByEmailAcceptsArrayResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/users" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"user_array","email_addresses":[{"id":"email_1","email_address":"array@example.com"}],"primary_email_address_id":"email_1"}]`))
	}))
	t.Cleanup(server.Close)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: server.URL})

	user, err := provider.FindUserByEmail(context.Background(), "array@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if user.ProviderUserID != "user_array" || user.Email != "array@example.com" {
		t.Fatalf("unexpected mapped user: %+v", user)
	}
}

func TestCreateUserUsesClerkUsersEndpoint(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	user, err := provider.CreateUser(context.Background(), "Created@Example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ProviderUserID != "user_created" || user.Email != "created@example.com" {
		t.Fatalf("unexpected mapped user: %+v", user)
	}
	if !f.hit("POST /users") {
		t.Fatalf("expected Clerk users create endpoint; hits=%v", f.paths())
	}
}

func TestUpdateUserEmailUsesClerkUsersEndpoint(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	user, err := provider.UpdateUserEmail(context.Background(), "user_123", "Updated@Example.com")
	if err != nil {
		t.Fatalf("UpdateUserEmail: %v", err)
	}
	if user.ProviderUserID != "user_123" || user.Email != "updated@example.com" {
		t.Fatalf("unexpected mapped user: %+v", user)
	}
	if !f.hit("PUT /users/user_123/email_address") || !f.hit("GET /users/user_123") {
		t.Fatalf("expected Clerk user update endpoint; hits=%v", f.paths())
	}
}

func TestDeleteUserUsesClerkUsersEndpoint(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	if err := provider.DeleteUser(context.Background(), "user_123"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if !f.hit("DELETE /users/user_123") {
		t.Fatalf("expected Clerk user delete endpoint; hits=%v", f.paths())
	}
}

func TestListUserOrgMembershipsMapsClerkOrganizations(t *testing.T) {
	f := newFakeClerk(t, http.StatusOK)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	memberships, err := provider.ListUserOrgMemberships(context.Background(), "user_123")
	if err != nil {
		t.Fatalf("ListUserOrgMemberships: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("expected one membership, got %+v", memberships)
	}
	got := memberships[0]
	if got.Org.ProviderOrgID != "org_PROVIDER" || got.Org.Name != "Provider Org" || got.Org.Slug != "provider-org" {
		t.Fatalf("unexpected org mapping: %+v", got.Org)
	}
	if got.Role != "admin" {
		t.Fatalf("expected org:admin to map to admin, got %q", got.Role)
	}
	if !f.hit("GET /users/user_123/organization_memberships") {
		t.Fatalf("expected user organization memberships endpoint; hits=%v", f.paths())
	}
}

func TestRoleTranslation(t *testing.T) {
	if got := clerkRole("owner"); got != "org:admin" {
		t.Fatalf("owner should map to org:admin, got %q", got)
	}
	if got := clerkRole("admin"); got != "org:admin" {
		t.Fatalf("admin should map to org:admin, got %q", got)
	}
	if got := clerkRole("member"); got != "org:member" {
		t.Fatalf("member should map to org:member, got %q", got)
	}
}

func TestDeleteOrgMembershipTolerates404(t *testing.T) {
	f := newFakeClerk(t, http.StatusNotFound)
	provider := NewProvider(Config{SecretKey: "sk_test_fake", BaseURL: f.server.URL})

	if err := provider.DeleteOrgMembership(context.Background(), "org_PROVIDER", "user_123"); err != nil {
		t.Fatalf("DeleteOrgMembership must tolerate 404, got %v", err)
	}
}
