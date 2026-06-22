package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

func TestDevProxyInjectsInternalJWTAndOrg(t *testing.T) {
	var gotPath, gotAuth string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"companion:tasks:read"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/tasks", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/tasks" {
		t.Fatalf("expected stripped path /v1/tasks, got %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	claims := decodeClaims(t, strings.TrimPrefix(gotAuth, "Bearer "))
	if claims["org_id"] != "org-a" {
		t.Fatalf("expected org claim org-a, got %#v", claims["org_id"])
	}
	if claims["aud"] != "companion" {
		t.Fatalf("expected companion audience, got %#v", claims["aud"])
	}
}

func TestDevProxyStripsOnBehalfOfHeader(t *testing.T) {
	var gotOnBehalfOf, gotCookie, gotAPIKey string
	called := false
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotOnBehalfOf = r.Header.Get("X-On-Behalf-Of")
		gotCookie = r.Header.Get("Cookie")
		gotAPIKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"nexus:approvals:decide"})
	if err != nil {
		t.Fatal(err)
	}

	// A browser must not be able to smuggle identity delegation downstream:
	// nexus honors X-On-Behalf-Of for api-key service principals, and a
	// forwarded header would let a console human forge decided_by.
	req := httptest.NewRequest(http.MethodGet, "/api/nexus/v1/approvals/pending", nil)
	req.Header.Set("X-On-Behalf-Of", "forged-approver")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("X-API-Key", "leaked-key")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("downstream was not called")
	}
	if gotOnBehalfOf != "" {
		t.Fatalf("expected X-On-Behalf-Of stripped, got %q", gotOnBehalfOf)
	}
	if gotCookie != "" {
		t.Fatalf("expected Cookie stripped, got %q", gotCookie)
	}
	if gotAPIKey != "" {
		t.Fatalf("expected X-API-Key stripped, got %q", gotAPIKey)
	}
}

func TestDevRejectsUnauthorizedOrgSelection(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called")
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"companion:tasks:read"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/tasks", nil)
	req.Header.Set("X-Axis-Org-ID", "org-b")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDevProxyCrossOrgEmitsScopedInternalJWT(t *testing.T) {
	var gotAuth string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"axis:cross_org", "companion:tasks:read"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/tasks", nil)
	req.Header.Set("X-Axis-Org-ID", "org-b")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	claims := decodeClaims(t, strings.TrimPrefix(gotAuth, "Bearer "))
	if claims["org_id"] != "org-b" {
		t.Fatalf("expected org-b scoped token, got %#v", claims["org_id"])
	}
	if claims["actor_id"] != "user-a" || claims["on_behalf_of"] != "user-a" {
		t.Fatalf("expected delegated actor claims, got actor=%#v on_behalf_of=%#v", claims["actor_id"], claims["on_behalf_of"])
	}
	if claims["actor_type"] != "human" || claims["service_principal"] != true {
		t.Fatalf("expected human/service principal claims, got actor_type=%#v service=%#v", claims["actor_type"], claims["service_principal"])
	}
}

func TestDevProxyCompanionCrossOrgScopeIsAccepted(t *testing.T) {
	var gotAuth string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"companion:cross_org", "companion:runtime:admin"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/runtime/policy", nil)
	req.Header.Set("X-Axis-Org-ID", "org-b")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	claims := decodeClaims(t, strings.TrimPrefix(gotAuth, "Bearer "))
	if claims["org_id"] != "org-b" {
		t.Fatalf("expected org-b scoped token, got %#v", claims["org_id"])
	}
}

func TestAgentProfilesEndpointReadsCompanionProfiles(t *testing.T) {
	var gotPath, gotAuth string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"profiles":[{"profile_id":"axis.ops.billing.v1","name":"Billing Agent","family_id":"axis.ops.billing","version_label":"v1","system_prompt":"prompt","max_autonomy":"A1","enabled":true}]}`))
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"companion:agent_profiles:read"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles?include_archived=false", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/agent-profiles?include_archived=false" {
		t.Fatalf("expected companion agent profiles path, got %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	claims := decodeClaims(t, strings.TrimPrefix(gotAuth, "Bearer "))
	if claims["aud"] != "companion" || claims["org_id"] != "org-a" {
		t.Fatalf("unexpected downstream claims: %#v", claims)
	}
	var body struct {
		Profiles []struct {
			ProfileID string `json:"profile_id"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Profiles) != 1 || body.Profiles[0].ProfileID != "axis.ops.billing.v1" {
		t.Fatalf("unexpected profiles body: %s", rec.Body.String())
	}
}

func TestSessionReturnsSelectedOrgForCrossOrgPrincipal(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", []string{"axis:cross_org"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.Header.Set("X-Axis-Org-ID", "org-b")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["org_id"] != "org-b" {
		t.Fatalf("expected org-b, got %#v", body["org_id"])
	}
}

func TestControlPlaneListsDevOrg(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/orgs", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Orgs []IAMOrg `json:"orgs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Orgs) == 0 || body.Orgs[0].ID != "org-a" {
		t.Fatalf("expected seeded dev org, got %#v", body.Orgs)
	}
}

func TestControlPlaneCreatesOrgWithOwnerMembership(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":"Acme Corp","slug":"acme"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Org IAMOrg `json:"org"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Org.ID == "" || created.Org.Slug != "acme" {
		t.Fatalf("expected created org with slug acme, got %#v", created.Org)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/orgs/"+created.Org.ID+"/members", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var members struct {
		Members []IAMMember `json:"members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &members); err != nil {
		t.Fatal(err)
	}
	if len(members.Members) != 1 || members.Members[0].UserID != "user-a" || members.Members[0].Role != "owner" {
		t.Fatalf("expected owner membership for user-a, got %#v", members.Members)
	}
}

func TestControlPlaneInvitationsAndAudit(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/orgs/org-a/invitations", strings.NewReader(`{"email":"new@example.com","role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Invitation IAMInvitation `json:"invitation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Invitation.ID == "" || created.Invitation.OrgID != "org-a" || created.Invitation.Email != "new@example.com" {
		t.Fatalf("unexpected invitation: %#v", created.Invitation)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/orgs/org-a/invitations", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Invitations []IAMInvitation `json:"invitations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Invitations) != 1 || listed.Invitations[0].Email != "new@example.com" {
		t.Fatalf("expected listed invitation, got %#v", listed.Invitations)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam-audit?org_id=org-a", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var audit struct {
		Events []IAMAuditEvent `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &audit); err != nil {
		t.Fatal(err)
	}
	if len(audit.Events) == 0 || audit.Events[0].Action != "invitation.created" {
		t.Fatalf("expected invitation audit event, got %#v", audit.Events)
	}
}

func TestControlPlaneHardDeletesIAMResources(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"email":"delete@example.com","name":"Delete Me"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected user create 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var createdUser struct {
		User IAMUser `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createdUser); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/orgs/org-a/members", strings.NewReader(fmt.Sprintf(`{"user_id":%q,"role":"member"}`, createdUser.User.ID)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected member create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/orgs/org-a/members/"+createdUser.User.ID, nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected member delete 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/users/"+createdUser.User.ID, nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected user delete 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/orgs", strings.NewReader(`{"name":"Delete Org","slug":"delete-org"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected org create 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var createdOrg struct {
		Org IAMOrg `json:"org"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createdOrg); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/orgs/"+createdOrg.Org.ID, nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected org delete 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestControlPlaneRequiresUserReadScope(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", []string{"axis:orgs:read"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSimpleIAMTenantsProductsAndUsers(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/iam/tenants", strings.NewReader(`{"name":"Pymes"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected tenant create 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var tenantBody struct {
		Item IAMTenantView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tenantBody); err != nil {
		t.Fatal(err)
	}
	if tenantBody.Item.ID == "" || tenantBody.Item.Name != "Pymes" {
		t.Fatalf("unexpected tenant: %#v", tenantBody.Item)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam/tenants", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tenants list 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "slug") {
		t.Fatalf("simple IAM response must not expose slug: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/iam/products", strings.NewReader(fmt.Sprintf(`{"tenant_id":%q,"name":"Pymes"}`, tenantBody.Item.ID)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected product create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam/products?tenant_id="+tenantBody.Item.ID, nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected products list 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var productsBody struct {
		Items []IAMProductView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &productsBody); err != nil {
		t.Fatal(err)
	}
	if len(productsBody.Items) != 1 || productsBody.Items[0].TenantID != tenantBody.Item.ID || productsBody.Items[0].ProductSurface != "pymes" {
		t.Fatalf("expected tenant product pymes, got %#v", productsBody.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/iam/users", strings.NewReader(fmt.Sprintf(`{"tenant_id":%q,"email":"admin@pymes.local","role":"admin"}`, tenantBody.Item.ID)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected user create 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var userBody struct {
		Item IAMUserView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &userBody); err != nil {
		t.Fatal(err)
	}
	if userBody.Item.TenantID != tenantBody.Item.ID || userBody.Item.Role != "admin" {
		t.Fatalf("expected tenant admin user, got %#v", userBody.Item)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/iam/users", strings.NewReader(fmt.Sprintf(`{"org_id":%q,"email":"ops@pymes.local","role":"member"}`, tenantBody.Item.ID)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected org_id user create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/iam/users", strings.NewReader(`{"org_id":"axis","email":"axis-admin@example.com","role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected axis user create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam/users?org_id=axis", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected axis users list 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var axisUsersBody struct {
		Items []IAMUserView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &axisUsersBody); err != nil {
		t.Fatal(err)
	}
	foundAxisAdmin := false
	for _, item := range axisUsersBody.Items {
		if item.Email == "axis-admin@example.com" && item.Scope == "axis" && item.OrgID == "axis" {
			foundAxisAdmin = true
		}
	}
	if !foundAxisAdmin {
		t.Fatalf("expected only global axis users, got %#v", axisUsersBody.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam/users?org_id="+tenantBody.Item.ID, nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected org users list 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var orgUsersBody struct {
		Items []IAMUserView `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &orgUsersBody); err != nil {
		t.Fatal(err)
	}
	foundAdmin := false
	foundOps := false
	for _, item := range orgUsersBody.Items {
		if item.Email == "admin@pymes.local" && item.Scope == "tenant" && item.OrgID == tenantBody.Item.ID {
			foundAdmin = true
		}
		if item.Email == "ops@pymes.local" && item.Scope == "tenant" && item.OrgID == tenantBody.Item.ID {
			foundOps = true
		}
	}
	if !foundAdmin || !foundOps {
		t.Fatalf("expected filtered org users, got %#v", orgUsersBody.Items)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/iam/users/"+userBody.Item.ID+"/purge", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected tenant user access purge 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	users, err := srv.iam.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundUser := false
	for _, user := range users {
		if user.Email == "admin@pymes.local" {
			foundUser = true
		}
	}
	if !foundUser {
		t.Fatal("expected tenant user record to remain after membership purge")
	}
}

func TestSimpleIAMMemberCannotReadIAM(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", orgMemberScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/iam/users", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSimpleIAMOrgAdminCannotListAxisUsers(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", orgAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/iam/users?org_id=axis", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam/users?org_id=org-a", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected own org users 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentsCRUDARByOrg(t *testing.T) {
	companion := newFakeCompanionAgentsServer(t)
	defer companion.Close()
	srv, err := newTestServer(companion.URL, defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"org_id":"org-a","name":"Care Agent","profile":"care.v1","autonomy":"A2","memory_enabled":true,"description":"Follow ups","capabilities":["care.read"],"tools":["care_read"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected agent create 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item IAMAgent `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Item.ID == "" || created.Item.OrgID != "org-a" || created.Item.Autonomy != "A2" || !created.Item.MemoryEnabled {
		t.Fatalf("unexpected created agent: %#v", created.Item)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"org_id":"org-b","name":"Other Agent","profile":"other.v1","autonomy":"A1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected second org agent create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agents?org_id=org-a", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected agents list 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []IAMAgent `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.Item.ID {
		t.Fatalf("expected only org-a agent, got %#v", list.Items)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/agents/"+created.Item.ID, strings.NewReader(`{"name":"Care Coordinator","profile":"care.v2","autonomy":"A3","memory_enabled":false,"description":"","capabilities":["care.write"],"tools":["care_write"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected agent update 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Item IAMAgent `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Item.Name != "Care Coordinator" || updated.Item.Profile != "care.v2" || updated.Item.Autonomy != "A3" || updated.Item.MemoryEnabled {
		t.Fatalf("unexpected updated agent: %#v", updated.Item)
	}

	for _, step := range []struct {
		method string
		path   string
		status string
	}{
		{http.MethodPost, "/api/agents/" + created.Item.ID + "/archive", "archived"},
		{http.MethodPost, "/api/agents/" + created.Item.ID + "/trash", "trash"},
		{http.MethodPost, "/api/agents/" + created.Item.ID + "/restore", "active"},
	} {
		req = httptest.NewRequest(step.method, step.path, nil)
		rec = httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected lifecycle %s 200, got %d body=%s", step.path, rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
			t.Fatal(err)
		}
		if updated.Item.Status != step.status {
			t.Fatalf("expected status %s, got %#v", step.status, updated.Item)
		}
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/agents/"+created.Item.ID+"/purge", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected purge 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentsOrgAdminAndMemberPermissions(t *testing.T) {
	companion := newFakeCompanionAgentsServer(t)
	defer companion.Close()
	adminSrv, err := newTestServer(companion.URL, orgAdminScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"org_id":"org-a","name":"Ops Agent","profile":"ops.v1","autonomy":"A1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	adminSrv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected org admin create own org 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agents?org_id=org-b", nil)
	rec = httptest.NewRecorder()
	adminSrv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected org admin cross-org 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	memberSrv, err := newTestServer("http://127.0.0.1:1", orgMemberScopes())
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/agents?org_id=org-a", nil)
	rec = httptest.NewRecorder()
	memberSrv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected member agents 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestClerkWebhookRequiresSecretInClerkMode(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.AuthMode = "clerk"

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/clerk", strings.NewReader(`{"type":"user.created","data":{}}`))
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestClerkWebhookVerifiesSignatureAndCreatesUser(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte("webhook-secret"))
	srv.cfg.ClerkWebhookSecret = secret
	body := []byte(`{"type":"user.created","data":{"id":"user_clerk","email_addresses":[{"email_address":"clerk@example.com"}]}}`)
	msgID := "msg_123"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/clerk", strings.NewReader(string(body)))
	req.Header.Set(clerkWebhookHeaderID, msgID)
	req.Header.Set(clerkWebhookHeaderTimestamp, timestamp)
	req.Header.Set(clerkWebhookHeaderSignature, signClerkWebhook(t, secret, msgID, timestamp, body))
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	users, err := srv.iam.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user.ID == "user_clerk" && user.Email == "clerk@example.com" && user.Provider == "clerk" {
			return
		}
	}
	t.Fatalf("expected synced Clerk user, got %#v", users)
}

func TestDefaultAdminScopesIncludePromptManagement(t *testing.T) {
	scopes := make(map[string]bool)
	for _, scope := range defaultAdminScopes() {
		scopes[scope] = true
	}

	for _, scope := range []string{
		"companion:assist:read",
		"companion:assist:write",
		"companion:agent_profiles:read",
		"companion:agent_profiles:admin",
		"companion:products:read",
		"companion:products:admin",
	} {
		if !scopes[scope] {
			t.Fatalf("expected default admin scope %q", scope)
		}
	}
}

func TestIdentityPrincipalScopesComeFromEffectiveRole(t *testing.T) {
	principal := axisPrincipalFromIdentity(authn.Principal{
		Actor:  "user-a",
		OrgID:  "org-a",
		Scopes: []string{"axis:cross_org", "axis:orgs:admin", "axis:users:admin"},
		Claims: map[string]any{
			"org_role": "org:member",
		},
	})

	if hasScope(principal.Scopes, "axis:cross_org", "axis:orgs:admin", "axis:users:admin") {
		t.Fatalf("member principal escalated admin scopes: %#v", principal.Scopes)
	}
	if !hasScope(principal.Scopes, "companion:assist:read") {
		t.Fatalf("expected member read scope, got %#v", principal.Scopes)
	}
}

func TestIdentityPrincipalOwnerGetsCrossOrgScopes(t *testing.T) {
	principal := axisPrincipalFromIdentity(authn.Principal{
		Actor: "owner-a",
		Claims: map[string]any{
			"axis_role": "owner",
			"org_role":  "org:member",
		},
	})

	if principal.Role != "owner" {
		t.Fatalf("expected owner role, got %q", principal.Role)
	}
	if !hasScope(principal.Scopes, "axis:cross_org", "axis:orgs:admin", "axis:users:admin") {
		t.Fatalf("expected owner admin scopes, got %#v", principal.Scopes)
	}
}

func newTestServer(target string, scopes []string) (*server, error) {
	return newServer(config{
		AuthMode:          "dev",
		DevOrgID:          "org-a",
		DevUserID:         "user-a",
		DevScopes:         scopes,
		InternalJWTSecret: "secret",
		InternalJWTIssuer: "axis-bff",
		CompanionBaseURL:  target,
		CompanionAudience: "companion",
		NexusBaseURL:      target,
		NexusAudience:     "nexus",
		DownstreamTimeout: time.Second,
	})
}

func newFakeCompanionAgentsServer(t *testing.T) *httptest.Server {
	t.Helper()
	agents := map[string]companionAgent{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/agents" && r.Method == http.MethodGet {
			orgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
			productSurface := firstNonEmpty(r.URL.Query().Get("product_surface"), "companion")
			out := []companionAgent{}
			for _, agent := range agents {
				if agent.ProductSurface == productSurface && (orgID == "*" || agent.OrgID == orgID) {
					out = append(out, agent)
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": out})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/agents/") {
			http.NotFound(w, r)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/agents/")
		parts := strings.Split(rest, "/")
		agentID, err := url.PathUnescape(parts[0])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		orgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
		productSurface := firstNonEmpty(r.URL.Query().Get("product_surface"), "companion")
		key := orgID + "/" + productSurface + "/" + agentID
		if len(parts) == 1 && r.Method == http.MethodPut {
			var agent companionAgent
			if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.OrgID = orgID
			agent.ProductSurface = productSurface
			agent.AgentID = agentID
			agent.Status = firstNonEmpty(agent.Status, "active")
			agent.LifecycleStatus = firstNonEmpty(agent.LifecycleStatus, "active")
			agent.OriginKind = firstNonEmpty(agent.OriginKind, "manual")
			agent.ReviewStatus = firstNonEmpty(agent.ReviewStatus, "approved")
			agent.MaxAutonomy = firstNonEmpty(agent.MaxAutonomy, "A2")
			now := time.Now().UTC()
			if agent.CreatedAt.IsZero() {
				agent.CreatedAt = now
			}
			agent.UpdatedAt = now
			agents[key] = agent
			writeJSON(w, http.StatusOK, agent)
			return
		}
		agent, ok := agents[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if len(parts) == 1 && r.Method == http.MethodDelete {
			delete(agents, key)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if len(parts) != 2 || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		switch parts[1] {
		case "archive":
			agent.Status = "disabled"
			agent.LifecycleStatus = "archived"
		case "trash":
			agent.Status = "disabled"
			agent.LifecycleStatus = "trash"
		case "restore":
			agent.Status = "active"
			agent.LifecycleStatus = "active"
		case "approve":
			agent.Status = "active"
			agent.LifecycleStatus = "active"
			agent.ReviewStatus = "approved"
		case "ignore":
			agent.Status = "disabled"
			agent.LifecycleStatus = "archived"
			agent.ReviewStatus = "ignored"
		default:
			http.NotFound(w, r)
			return
		}
		agent.UpdatedAt = time.Now().UTC()
		agents[key] = agent
		writeJSON(w, http.StatusOK, agent)
	}))
}

func decodeClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid token parts: %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func signClerkWebhook(t *testing.T, secret, msgID, timestamp string, body []byte) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, clerkWebhookSecretPrefix))
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msgID + "." + timestamp + "."))
	_, _ = mac.Write(body)
	return clerkWebhookSignatureScheme + "," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
