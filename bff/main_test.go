package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	tenantID := seedTenantForActor(t, srv, "org-a", "axis", "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/tasks", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
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
	if claims["tenant_id"] != tenantID || claims["product_surface"] != "axis" {
		t.Fatalf("expected tenant-scoped claims, got tenant=%#v product=%#v", claims["tenant_id"], claims["product_surface"])
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
	tenantID := seedTenantForActor(t, srv, "org-a", "axis", "admin")

	// A browser must not be able to smuggle identity delegation downstream:
	// nexus honors X-On-Behalf-Of for api-key service principals, and a
	// forwarded header would let a console human forge decided_by.
	req := httptest.NewRequest(http.MethodGet, "/api/nexus/v1/approvals/pending", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
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

func TestDevRejectsUnauthorizedTenantSelection(t *testing.T) {
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called")
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"companion:tasks:read"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "org-b", "axis")

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/tasks", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
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
	tenantID := seedTenantForActor(t, srv, "org-b", "axis", "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/tasks", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
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
	tenantID := seedTenantForActor(t, srv, "org-b", "axis", "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/companion/v1/runtime/policy", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
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
	tenantID := seedTenantForActor(t, srv, "org-a", "axis", "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles?include_archived=false", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
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

func TestAgentProfilesEndpointWritesCompanionProfiles(t *testing.T) {
	var requests []string
	var gotAuth string
	var gotBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.String())
		gotAuth = r.Header.Get("Authorization")
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agent-profiles/axis.ops.support.v1":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.support.v1","family_id":"axis.ops.support","version_label":"v1","name":"Support Agent","system_prompt":"Help users.","max_autonomy":"A1","enabled":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-profiles/axis.ops.support.v1/archive":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.support.v1","family_id":"axis.ops.support","version_label":"v1","name":"Support Agent","system_prompt":"Help users.","max_autonomy":"A1","enabled":true,"archived_at":"2026-06-22T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-profiles/axis.ops.support.v1/trash":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.support.v1","family_id":"axis.ops.support","version_label":"v1","name":"Support Agent","system_prompt":"Help users.","max_autonomy":"A1","enabled":true,"trashed_at":"2026-06-22T00:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-profiles/axis.ops.support.v1/restore":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.support.v1","family_id":"axis.ops.support","version_label":"v1","name":"Support Agent","system_prompt":"Help users.","max_autonomy":"A1","enabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/agent-profiles/axis.ops.support.v1/purge":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"companion:agent_profiles:admin"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenantForActor(t, srv, "org-a", "axis", "admin")
	if err := srv.iam.SetPlatformRole(context.Background(), srv.cfg.DevUserID, "platform_admin"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/agent-profiles/axis.ops.support.v1", strings.NewReader(`{"family_id":"axis.ops.support","version_label":"v1","name":"Support Agent","system_prompt":"Help users.","max_autonomy":"A1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected profile put 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotBody == "" || !strings.Contains(gotBody, `"system_prompt":"Help users."`) {
		t.Fatalf("expected forwarded body, got %q", gotBody)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}

	for _, path := range []string{
		"/api/agent-profiles/axis.ops.support.v1/archive",
		"/api/agent-profiles/axis.ops.support.v1/trash",
		"/api/agent-profiles/axis.ops.support.v1/restore",
	} {
		req = httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("X-Tenant-ID", tenantID)
		rec = httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected profile action 200 for %s, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/agent-profiles/axis.ops.support.v1/purge", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected profile purge 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	want := []string{
		"PUT /v1/agent-profiles/axis.ops.support.v1",
		"POST /v1/agent-profiles/axis.ops.support.v1/archive",
		"POST /v1/agent-profiles/axis.ops.support.v1/trash",
		"POST /v1/agent-profiles/axis.ops.support.v1/restore",
		"DELETE /v1/agent-profiles/axis.ops.support.v1/purge",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected downstream requests:\n%s", strings.Join(requests, "\n"))
	}
}

func TestAgentProfilesEndpointRejectsMemberWrites(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", orgMemberScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/agent-profiles/axis.ops.support.v1", strings.NewReader(`{"family_id":"axis.ops.support","version_label":"v1","name":"Support Agent","system_prompt":"Help users.","max_autonomy":"A1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPromptsEndpointAdaptsAssistPacks(t *testing.T) {
	var requests []string
	var gotAuth string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.String())
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/assist-packs":
			_, _ = w.Write([]byte(`[{"id":"pack-a","product_surface":"medmory","assist_type":"summary","name":"Summary","prompt_template":"old","enabled":true}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/assist-packs/archived":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"axis:products:admin", "companion:assist:read"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenantForActor(t, srv, "org-a", "medmory", "admin")

	req := httptest.NewRequest(http.MethodGet, "/api/prompts/assist-packs?product_surface=medmory&lifecycle=active", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected assist pack list 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/prompts/assist-packs?product_surface=medmory&lifecycle=archived", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected archived assist pack list 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/prompts/assist-packs/pack-a/content", strings.NewReader(`{"prompt_template":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected assist pack content update 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "assist pack prompts must be loaded from the owner product") {
		t.Fatalf("expected owner product error, got %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/prompts/assist-packs/pack-a/archive", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected assist pack archive 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "assist pack prompts are managed from the owner product") {
		t.Fatalf("expected owner product lifecycle error, got %s", rec.Body.String())
	}

	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	want := []string{
		"GET /v1/assist-packs?product_surface=medmory",
		"GET /v1/assist-packs/archived?product_surface=medmory",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected downstream requests:\n%s", strings.Join(requests, "\n"))
	}
}

func TestPromptsEndpointSanitizesTransportError(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", []string{"axis:products:admin", "companion:assist:read"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenantForActor(t, srv, "org-a", "medmory", "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/prompts/assist-packs", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "downstream request failed") {
		t.Fatalf("expected sanitized downstream error, got %s", body)
	}
	if strings.Contains(body, "127.0.0.1") || strings.Contains(body, "connect") {
		t.Fatalf("transport details leaked in body: %s", body)
	}
}

func TestPromptsEndpointAdaptsAgentProfilePrompts(t *testing.T) {
	var requests []string
	var gotBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.String())
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agent-profiles":
			_, _ = w.Write([]byte(`{"profiles":[{"profile_id":"axis.ops.billing.v1","name":"Billing Agent","family_id":"axis.ops.billing","version_label":"v1","system_prompt":"old","max_autonomy":"A1","enabled":true}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/agent-profiles/axis.ops.billing.v1":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.billing.v1","name":"Billing Agent","family_id":"axis.ops.billing","version_label":"v2","system_prompt":"new","max_autonomy":"A1","enabled":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-profiles/axis.ops.billing.v1/restore":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.billing.v1","name":"Billing Agent","family_id":"axis.ops.billing","version_label":"v2","system_prompt":"new","max_autonomy":"A1","enabled":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-profiles/axis.ops.billing.v1/trash":
			_, _ = w.Write([]byte(`{"profile_id":"axis.ops.billing.v1","name":"Billing Agent","family_id":"axis.ops.billing","version_label":"v2","system_prompt":"new","max_autonomy":"A1","enabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/agent-profiles/axis.ops.billing.v1/purge":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, []string{"axis:agents:admin", "companion:agent_profiles:read"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenantForActor(t, srv, "org-a", "axis", "admin")
	if err := srv.iam.SetPlatformRole(context.Background(), srv.cfg.DevUserID, "platform_admin"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/prompts/agent-profiles?lifecycle=all", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected profile prompt list 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/prompts/agent-profiles/axis.ops.billing.v1/system-prompt", strings.NewReader(`{"system_prompt":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected profile prompt update 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotBody != `{"system_prompt":"new"}` {
		t.Fatalf("expected forwarded profile body, got %q", gotBody)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/prompts/agent-profiles/axis.ops.billing.v1/restore", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected profile prompt restore 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/prompts/agent-profiles/axis.ops.billing.v1/trash", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected profile prompt trash 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/prompts/agent-profiles/axis.ops.billing.v1/purge", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected profile prompt purge 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	want := []string{
		"GET /v1/agent-profiles?lifecycle=all",
		"PUT /v1/agent-profiles/axis.ops.billing.v1",
		"POST /v1/agent-profiles/axis.ops.billing.v1/restore",
		"POST /v1/agent-profiles/axis.ops.billing.v1/trash",
		"DELETE /v1/agent-profiles/axis.ops.billing.v1/purge",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected downstream requests:\n%s", strings.Join(requests, "\n"))
	}
}

func TestAgentProfilesEndpointSanitizesTransportError(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", []string{"companion:agent_profiles:read"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenantForActor(t, srv, "org-a", "axis", "admin")
	req := httptest.NewRequest(http.MethodGet, "/api/agent-profiles", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "downstream request failed") {
		t.Fatalf("expected sanitized downstream error, got %s", body)
	}
	if strings.Contains(body, "127.0.0.1") || strings.Contains(body, "connect") {
		t.Fatalf("transport details leaked in body: %s", body)
	}
}

func TestAgentsEndpointSanitizesCompanionError(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/agents?org_id=org-a", nil)
	rec := httptest.NewRecorder()

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "companion agents request failed") {
		t.Fatalf("expected sanitized companion agents error, got %s", body)
	}
	if strings.Contains(body, "127.0.0.1") || strings.Contains(body, "connect") {
		t.Fatalf("transport details leaked in body: %s", body)
	}
}

func TestPromptsEndpointRejectsMemberWrites(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", orgMemberScopes())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/prompts/assist-packs/pack-a/content", strings.NewReader(`{"prompt_template":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
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
	// Modelo nuevo: crear user con tenant_id=<org> lo agrega al tenant 'axis' del
	// org (no a un legacy org-member). OrgID = el org; TenantID = su tenant 'axis'
	// (≠ org id); Scope = tenant; Role = admin.
	if userBody.Item.OrgID != tenantBody.Item.ID || userBody.Item.Role != "admin" || userBody.Item.Scope != "tenant" {
		t.Fatalf("expected tenant admin user in org's 'axis' tenant, got %#v", userBody.Item)
	}
	if userBody.Item.TenantID == "" || userBody.Item.TenantID == tenantBody.Item.ID {
		t.Fatalf("expected a real 'axis' tenant id (≠ org id), got %q", userBody.Item.TenantID)
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
	req.Header.Set("X-Tenant-ID", userBody.Item.TenantID)
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
	req.Header.Set("X-Tenant-ID", userBody.Item.TenantID)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected tenant user access purge 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	users, err := srv.iam.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Purge is a hard delete to the IdP (Clerk DELETE /users/{id}): the identity
	// is removed. Soft "remove access" is archive/trash, not purge.
	for _, user := range users {
		if user.Email == "admin@pymes.local" {
			t.Fatal("expected user identity to be deleted on purge")
		}
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
	seedDevPrincipal(t, srv)
	tenant, err := srv.iam.CreateTenant(context.Background(), IAMTenant{OrgID: "org-a", ProductSurface: "axis", Name: "org-a / axis", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.iam.UpsertTenantMember(context.Background(), IAMTenantMember{TenantID: tenant.ID, UserID: "user-a", Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/iam/users?org_id=axis", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/iam/users?org_id=org-a", nil)
	req.Header.Set("X-Tenant-ID", tenant.ID)
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
	if created.Item.OriginKind != "manual" || created.Item.ReviewStatus != "approved" || created.Item.ValidationStatus != "approved" || created.Item.Status != "active" {
		t.Fatalf("expected manual approved active agent, got %#v", created.Item)
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
	if list.Items[0].ValidationStatus != "approved" {
		t.Fatalf("expected validation_status alias, got %#v", list.Items[0])
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
		{http.MethodPost, "/api/agents/" + created.Item.ID + "/approve", "archived"},
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
	seedDevPrincipal(t, adminSrv)

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

// TestIAMTenantsCreateProvisionsDefaultTenant: el POST /api/iam/tenants (que crea
// un org) ahora también provisiona el tenant 'axis' del org + el creador como
// owner, igual que el control plane → ningún org nace sin tenant.
func TestIAMTenantsCreateProvisionsDefaultTenant(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/iam/tenants", strings.NewReader(`{"name":"Pymes"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Item IAMTenantView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	tenants, err := srv.iam.ResolveTenantsForUser(context.Background(), srv.cfg.DevUserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tn := range tenants {
		if tn.OrgID == body.Item.ID && tn.ProductSurface == defaultOrgProductSurface {
			return
		}
	}
	t.Fatalf("creator should resolve the org's %q tenant, got %#v", defaultOrgProductSurface, tenants)
}

// TestControlCreateOrgProvisionsDefaultTenant locks in the B#3 prerequisite: a
// newly-created org is never tenantless — it gets a default 'axis' tenant and the
// creator is a member of it, so the console can always resolve an X-Tenant-ID.
func TestControlCreateOrgProvisionsDefaultTenant(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	p := authn.Principal{Actor: "admin-user"}
	req := httptest.NewRequest(http.MethodPost, "/api/control/organizations", strings.NewReader(`{"name":"Acme"}`))
	rec := httptest.NewRecorder()

	srv.controlCreateOrg(rec, req, p)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var org IAMOrg
	if err := json.Unmarshal(rec.Body.Bytes(), &org); err != nil {
		t.Fatal(err)
	}
	tenants, err := srv.iam.ResolveTenantsForUser(context.Background(), "admin-user")
	if err != nil {
		t.Fatal(err)
	}
	for _, tn := range tenants {
		if tn.OrgID == org.ID && tn.ProductSurface == defaultOrgProductSurface {
			return
		}
	}
	t.Fatalf("creator should resolve the org's default %q tenant, got %#v", defaultOrgProductSurface, tenants)
}

// TestResolveAppContextDerivesScopesFromTenantMembership locks in the tenancy
// cutover: proxied app-plane requests must resolve org/product/scopes from the
// active tenant, not from org-only membership or Clerk claim scopes.
func TestResolveAppContextDerivesScopesFromTenantMembership(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "org-a", "axis")
	if _, err := srv.iam.CreateUser(context.Background(), IAMUser{ID: "user-a", ExternalID: "user-a", Email: "user-a@example.com", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.iam.UpsertTenantMember(context.Background(), IAMTenantMember{TenantID: tenantID, UserID: "user-a", Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	// Principal carries only minimal (member) claim scopes — Axis says admin.
	p := authn.Principal{Actor: "user-a", OrgID: "org-a", Scopes: orgMemberScopes()}
	req := httptest.NewRequest(http.MethodGet, "/api/nexus/x", nil)
	req.Header.Set("X-Tenant-ID", tenantID)

	orgID, productSurface, resolvedTenant, scopes, err := srv.resolveAppContext(req, p)
	if err != nil {
		t.Fatalf("resolveAppContext: %v", err)
	}
	if orgID != "org-a" || productSurface != "axis" || resolvedTenant != tenantID {
		t.Fatalf("expected org/product/tenant from tenant, got org=%q product=%q tenant=%q", orgID, productSurface, resolvedTenant)
	}
	if !hasScope(scopes, "axis:users:admin") {
		t.Fatalf("expected Axis-derived admin scopes, got %#v", scopes)
	}
}

// TestResolveAppContextRequiresTenant verifies the legacy app-plane org path has
// been removed: no X-Tenant-ID means no proxied Companion/Nexus request.
func TestResolveAppContextRequiresTenant(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := []string{"companion:tasks:read"}
	p := authn.Principal{Actor: "ghost", OrgID: "org-a", Scopes: sentinel}
	req := httptest.NewRequest(http.MethodGet, "/api/nexus/x", nil)

	_, _, _, _, err = srv.resolveAppContext(req, p)
	if err == nil || !strings.Contains(err.Error(), "tenant_id is required") {
		t.Fatalf("expected tenant required error, got %v", err)
	}
}

func TestResolveAppContextSurfacesPlatformRoleStoreError(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "org-a", "axis")
	srv.iam = platformRolesErrStore{IAMStore: srv.iam}

	p := authn.Principal{Actor: "user-a", OrgID: "org-a", Scopes: defaultAdminScopes()}
	req := httptest.NewRequest(http.MethodGet, "/api/nexus/x", nil)
	req.Header.Set("X-Tenant-ID", tenantID)

	_, _, _, _, err = srv.resolveAppContext(req, p)
	if err == nil || !strings.Contains(err.Error(), "platform roles lookup failed") {
		t.Fatalf("expected platform role lookup error, got %v", err)
	}
}

func TestProxySurfacesAppContextStoreErrorAsStable500(t *testing.T) {
	called := false
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenant(t, srv, "org-a", "axis")
	srv.iam = platformRolesErrStore{IAMStore: srv.iam}

	req := httptest.NewRequest(http.MethodGet, "/api/nexus/v1/approvals/pending", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if called {
		t.Fatal("downstream should not be called when app context cannot be resolved")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "platform roles store down") || strings.Contains(rec.Body.String(), "platform roles lookup failed") {
		t.Fatalf("response leaked app context store error: %s", rec.Body.String())
	}
}

// TestClerkWebhookUserDeletedRemovesAxisUser verifies the Clerk->Axis sync drops
// the orphaned axis_users row when a user is deleted in the IdP.
func TestClerkWebhookUserDeletedRemovesAxisUser(t *testing.T) {
	srv, err := newTestServer("http://127.0.0.1:1", defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.iam.CreateUser(context.Background(), IAMUser{ID: "user_gone", Provider: "clerk", Email: "gone@example.com", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte("webhook-secret"))
	srv.cfg.ClerkWebhookSecret = secret
	body := []byte(`{"type":"user.deleted","data":{"id":"user_gone"}}`)
	msgID := "msg_del"
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
		if user.ID == "user_gone" {
			t.Fatalf("expected user_gone removed after user.deleted, still present: %#v", users)
		}
	}
}

// sessionStoreErrStore makes the first session() store read fail, to prove the
// handler surfaces a store outage instead of rendering an empty session.
type sessionStoreErrStore struct {
	IAMStore
}

func (sessionStoreErrStore) ListOrgsForActor(context.Context, string, bool) ([]IAMOrg, error) {
	return nil, fmt.Errorf("store down")
}

type platformRolesErrStore struct {
	IAMStore
}

func (platformRolesErrStore) PlatformRolesForUser(context.Context, string) ([]string, error) {
	return nil, fmt.Errorf("platform roles store down")
}

type fakeAuthenticator struct {
	principal authn.Principal
	err       error
}

func (f fakeAuthenticator) Authenticate(context.Context, authn.Credential) (*authn.Principal, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &f.principal, nil
}

type noopIdentity struct {
	HumanIdentityProvider
}

func (noopIdentity) PrincipalFromClaims(_ context.Context, p authn.Principal) (authn.Principal, error) {
	return p, nil
}

func (noopIdentity) SyncPrincipal(context.Context, authn.Principal) error {
	return nil
}

func TestSessionSurfacesStoreError(t *testing.T) {
	srv := &server{iam: sessionStoreErrStore{}}
	req := httptest.NewRequest(http.MethodGet, "/v1/session", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalContextKey{}, authn.Principal{Actor: "user-a", OrgID: "org-a"}))
	rec := httptest.NewRecorder()

	srv.session(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected store outage to surface as non-200, got 200 body=%s", rec.Body.String())
	}
}

func TestAuthenticateSurfacesPlatformRoleStoreErrorAsStable500(t *testing.T) {
	srv := &server{
		cfg: config{
			AuthMode:          "clerk",
			InternalJWTSecret: "secret",
			CompanionBaseURL:  "http://127.0.0.1:1",
			NexusBaseURL:      "http://127.0.0.1:1",
		},
		oidcAuth: fakeAuthenticator{principal: authn.Principal{
			Actor:  "user-a",
			OrgID:  "org-a",
			Claims: map[string]any{"sub": "user-a", "org_id": "org-a"},
		}},
		identity: noopIdentity{},
		iam:      platformRolesErrStore{IAMStore: newMemoryIAMStore()},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "platform roles store down") || strings.Contains(rec.Body.String(), "platform roles lookup failed") {
		t.Fatalf("response leaked auth store error: %s", rec.Body.String())
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

func seedTenantForActor(t *testing.T, srv *server, orgID, productSurface, role string) string {
	t.Helper()
	ctx := context.Background()
	tenantID := seedTenant(t, srv, orgID, productSurface)
	users, err := srv.iam.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, user := range users {
		if user.ID == srv.cfg.DevUserID {
			found = true
			break
		}
	}
	if !found {
		if _, err := srv.iam.CreateUser(ctx, IAMUser{ID: srv.cfg.DevUserID, ExternalID: srv.cfg.DevUserID, Email: srv.cfg.DevUserID + "@example.com", Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.iam.UpsertTenantMember(ctx, IAMTenantMember{TenantID: tenantID, UserID: srv.cfg.DevUserID, Role: role, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	return tenantID
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
		if len(parts) == 1 && r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, agent)
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
