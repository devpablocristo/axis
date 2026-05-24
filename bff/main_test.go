package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
