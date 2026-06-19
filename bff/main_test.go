package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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
