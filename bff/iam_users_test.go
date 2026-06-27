package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- helpers ---------------------------------------------------------------

type userListResp struct {
	Items []IAMUserView `json:"items"`
}
type userItemResp struct {
	Item IAMUserView `json:"item"`
}

func doReq(t *testing.T, srv *server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	return rec
}

// seedDevPrincipal makes the dev test principal (user-a) a member of org-a so
// canAccessOrg passes for org-scoped tests. The implicit dev seed was removed
// from the store, so tests now set up their own fixture explicitly.
func seedDevPrincipal(t *testing.T, srv *server) {
	t.Helper()
	ctx := context.Background()
	if _, err := srv.iam.CreateOrg(ctx, IAMOrg{ID: "org-a", Name: "org-a", Status: "active"}, ""); err != nil {
		t.Fatalf("seed dev org: %v", err)
	}
	if _, err := srv.iam.CreateUser(ctx, IAMUser{ID: "user-a", ExternalID: "user-a", Email: "user-a", Name: "user-a", Status: "active"}); err != nil {
		t.Fatalf("seed dev user: %v", err)
	}
	if _, err := srv.iam.UpsertMember(ctx, IAMMember{OrgID: "org-a", UserID: "user-a", Role: "admin", Status: "active"}); err != nil {
		t.Fatalf("seed dev member: %v", err)
	}
}

// seedTenant creates an org + tenant in the (memory) store and returns its id.
func seedTenant(t *testing.T, srv *server, orgID, product string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := srv.iam.CreateOrg(ctx, IAMOrg{ID: orgID, Name: orgID, Status: "active"}, ""); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	tn, err := srv.iam.CreateTenant(ctx, IAMTenant{OrgID: orgID, ProductSurface: product, Name: orgID + " / " + product, Status: "active"})
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return tn.ID
}

func listTenantUsers(t *testing.T, srv *server, orgID, tenantID, status string) []IAMUserView {
	t.Helper()
	path := "/api/iam/users?org_id=" + orgID
	if status != "" {
		path = "/api/iam/users/" + status + "?org_id=" + orgID
	}
	rec := doReq(t, srv, http.MethodGet, path, "", map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusOK {
		t.Fatalf("list users (%s): want 200 got %d body=%s", status, rec.Code, rec.Body.String())
	}
	var out userListResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("list users decode: %v", err)
	}
	return out.Items
}

func hasEmail(items []IAMUserView, email string) *IAMUserView {
	for i := range items {
		if items[i].Email == email {
			return &items[i]
		}
	}
	return nil
}

// --- tests -----------------------------------------------------------------

// TestTenantUserFullLifecycle exercises create → list → edit role → archive →
// restore → trash → purge for a user scoped to a tenant (org × product). Every
// step here used to 500 because the handlers treated the tenant id as an org id.
func TestTenantUserFullLifecycle(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	orgID := "co-a"
	tenantID := seedTenant(t, srv, orgID, "axis")
	email := "euge@soalen.com"

	// 1) Create user in the tenant.
	rec := doReq(t, srv, http.MethodPost, "/api/iam/users",
		fmt.Sprintf(`{"email":%q,"role":"member","org_id":%q}`, email, orgID),
		map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var created userItemResp
	json.Unmarshal(rec.Body.Bytes(), &created)
	rowID := created.Item.ID
	if created.Item.Scope != "tenant" || created.Item.TenantID != tenantID || rowID == "" {
		t.Fatalf("create: unexpected view %#v", created.Item)
	}
	if created.Item.ID != tenantUserRowID(tenantID, created.Item.UserID) {
		t.Fatalf("create: row id not tenant-encoded: %s", created.Item.ID)
	}

	// 2) List → present and active.
	if hasEmail(listTenantUsers(t, srv, orgID, tenantID, ""), email) == nil {
		t.Fatalf("create: user not in active tenant list")
	}

	// 3) Edit role to admin (was 500).
	rec = doReq(t, srv, http.MethodPut, "/api/iam/users/"+rowID,
		fmt.Sprintf(`{"role":"admin","org_id":%q}`, orgID),
		map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusOK {
		t.Fatalf("edit role: want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if u := hasEmail(listTenantUsers(t, srv, orgID, tenantID, ""), email); u == nil || u.Role != "admin" {
		t.Fatalf("edit role: role not updated, got %#v", u)
	}

	// 4) Archive (was 500) → leaves active list, appears in archived.
	rec = doReq(t, srv, http.MethodPost, "/api/iam/users/"+rowID+"/archive", "", map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if hasEmail(listTenantUsers(t, srv, orgID, tenantID, ""), email) != nil {
		t.Fatalf("archive: still in active list")
	}
	if hasEmail(listTenantUsers(t, srv, orgID, tenantID, "archived"), email) == nil {
		t.Fatalf("archive: not in archived list")
	}

	// 5) Restore → back to active.
	rec = doReq(t, srv, http.MethodPost, "/api/iam/users/"+rowID+"/restore", "", map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if hasEmail(listTenantUsers(t, srv, orgID, tenantID, ""), email) == nil {
		t.Fatalf("restore: not back in active list")
	}

	// 6) Trash.
	rec = doReq(t, srv, http.MethodPost, "/api/iam/users/"+rowID+"/trash", "", map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusOK {
		t.Fatalf("trash: want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if hasEmail(listTenantUsers(t, srv, orgID, tenantID, "trash"), email) == nil {
		t.Fatalf("trash: not in trash list")
	}

	// 7) Purge (was 500) → hard delete: gone from the tenant AND deleted from the
	// IdP. In dev-mode that means removed from axis_users (the local store);
	// against Clerk the adapter issues DELETE /users/{id} (see identity_clerk_test).
	rec = doReq(t, srv, http.MethodDelete, "/api/iam/users/"+rowID+"/purge", "", map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("purge: want 204 got %d body=%s", rec.Code, rec.Body.String())
	}
	for _, st := range []string{"", "archived", "trash"} {
		if hasEmail(listTenantUsers(t, srv, orgID, tenantID, st), email) != nil {
			t.Fatalf("purge: still in %q list", st)
		}
	}
	users, _ := srv.iam.ListUsers(context.Background())
	if hasUserEmail(users, email) != nil {
		t.Fatalf("purge: identity must be deleted (hard delete to the IdP)")
	}
}

func hasUserEmail(users []IAMUser, email string) *IAMUser {
	for i := range users {
		if users[i].Email == email {
			return &users[i]
		}
	}
	return nil
}

// TestTenantUserOwnerIsGlobal: creating an 'owner' grants the global platform role.
func TestTenantUserOwnerIsGlobal(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "co-a", "axis")
	rec := doReq(t, srv, http.MethodPost, "/api/iam/users",
		`{"email":"boss@co-a.com","role":"owner","org_id":"co-a"}`,
		map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create owner: want 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var created userItemResp
	json.Unmarshal(rec.Body.Bytes(), &created)
	roles, _ := srv.iam.PlatformRolesForUser(context.Background(), created.Item.UserID)
	if !isPlatformAdmin(roles) {
		t.Fatalf("owner must get a global platform role, got %v", roles)
	}
}

// TestTenantUserOwnerDemotionDropsPlatformRole: editing a user away from 'owner'
// must remove the global platform role (no lingering super-admin).
func TestTenantUserOwnerDemotionDropsPlatformRole(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "co-a", "axis")
	rec := doReq(t, srv, http.MethodPost, "/api/iam/users",
		`{"email":"boss@co-a.com","role":"owner","org_id":"co-a"}`,
		map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create owner: want 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var c userItemResp
	json.Unmarshal(rec.Body.Bytes(), &c)
	roles, _ := srv.iam.PlatformRolesForUser(context.Background(), c.Item.UserID)
	if !isPlatformAdmin(roles) {
		t.Fatalf("owner must hold the global platform role, got %v", roles)
	}
	// Demote to admin.
	rec = doReq(t, srv, http.MethodPut, "/api/iam/users/"+c.Item.ID,
		`{"role":"admin","org_id":"co-a"}`, map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusOK {
		t.Fatalf("demote: want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	roles, _ = srv.iam.PlatformRolesForUser(context.Background(), c.Item.UserID)
	if isPlatformAdmin(roles) {
		t.Fatalf("demotion must drop the global platform role, still has %v", roles)
	}
}

// TestTenantUserFindOrCreateByEmail: re-adding an existing email reuses the identity.
func TestTenantUserFindOrCreateByEmail(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	axis := seedTenant(t, srv, "co-a", "axis")
	medmory := seedTenant(t, srv, "co-a", "medmory")
	email := "shared@co-a.com"

	mk := func(tenantID string) string {
		rec := doReq(t, srv, http.MethodPost, "/api/iam/users",
			fmt.Sprintf(`{"email":%q,"role":"member","org_id":"co-a"}`, email),
			map[string]string{"X-Tenant-ID": tenantID})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: want 201 got %d body=%s", rec.Code, rec.Body.String())
		}
		var c userItemResp
		json.Unmarshal(rec.Body.Bytes(), &c)
		return c.Item.UserID
	}
	u1 := mk(axis)
	u2 := mk(medmory)
	if u1 != u2 {
		t.Fatalf("same email must map to one identity, got %s vs %s", u1, u2)
	}
	users, _ := srv.iam.ListUsers(context.Background())
	count := 0
	for _, u := range users {
		if u.Email == email {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 identity for %s, found %d", email, count)
	}
}

// TestTenantUserAccessIsPerProduct: a user added to one product tenant is NOT
// listed in another product tenant of the same org.
func TestTenantUserAccessIsPerProduct(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	axis := seedTenant(t, srv, "co-a", "axis")
	medmory := seedTenant(t, srv, "co-a", "medmory")
	email := "only-axis@co-a.com"
	rec := doReq(t, srv, http.MethodPost, "/api/iam/users",
		fmt.Sprintf(`{"email":%q,"role":"member","org_id":"co-a"}`, email),
		map[string]string{"X-Tenant-ID": axis})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	if hasEmail(listTenantUsers(t, srv, "co-a", axis, ""), email) == nil {
		t.Fatalf("user must be in axis tenant")
	}
	if hasEmail(listTenantUsers(t, srv, "co-a", medmory, ""), email) != nil {
		t.Fatalf("user must NOT leak into medmory tenant (access is per product)")
	}
}

// TestTenantUserPurgeRequiresScope: purge needs axis:iam:purge.
func TestTenantUserPurgeRequiresScope(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", withoutScopes(defaultAdminScopes(), "axis:iam:purge"))
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "co-a", "axis")
	rec := doReq(t, srv, http.MethodPost, "/api/iam/users",
		`{"email":"x@co-a.com","role":"member","org_id":"co-a"}`,
		map[string]string{"X-Tenant-ID": tenantID})
	var c userItemResp
	json.Unmarshal(rec.Body.Bytes(), &c)
	rec = doReq(t, srv, http.MethodDelete, "/api/iam/users/"+c.Item.ID+"/purge", "", map[string]string{"X-Tenant-ID": tenantID})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("purge without scope: want 403 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestParseUserRefRoundTrip(t *testing.T) {
	cases := []struct {
		ref          string
		wantScope    string
		wantUser     string
		wantGlobal   bool
		isRoundTrip  bool
		tenantID     string
		roundTripUID string
	}{
		{ref: "axis__user_123", wantScope: "", wantUser: "user_123", wantGlobal: true},
		{ref: "tenant__tn_x__user_456", wantScope: "tn_x", wantUser: "user_456", wantGlobal: false},
		{ref: "tenant__tn_with__under__user_789", wantScope: "tn_with", wantUser: "under__user_789", wantGlobal: false},
		{ref: "plain_user", wantScope: "", wantUser: "plain_user", wantGlobal: true},
	}
	for _, c := range cases {
		scope, user, global := parseUserRef(c.ref)
		if scope != c.wantScope || user != c.wantUser || global != c.wantGlobal {
			t.Fatalf("parseUserRef(%q) = (%q,%q,%v), want (%q,%q,%v)", c.ref, scope, user, global, c.wantScope, c.wantUser, c.wantGlobal)
		}
	}
	if got := tenantUserRowID("tn_x", "user_456"); got != "tenant__tn_x__user_456" {
		t.Fatalf("tenantUserRowID round-trip: %s", got)
	}
}
