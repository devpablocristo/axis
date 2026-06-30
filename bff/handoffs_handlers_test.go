package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandoffsPublicContractProxiesEmployeeIDs(t *testing.T) {
	var requests []string
	var gotTenant string
	var gotBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		gotTenant = r.Header.Get("X-Tenant-ID")
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/handoffs":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/handoffs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"handoff_id":"11111111-1111-4111-8111-111111111111","tenant_id":"22222222-2222-4222-8222-222222222222","from_employee_id":null,"to_employee_id":"33333333-3333-4333-8333-333333333333","reason":"handoff","status":"pending"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/handoffs/11111111-1111-4111-8111-111111111111":
			_, _ = w.Write([]byte(`{"handoff_id":"11111111-1111-4111-8111-111111111111","tenant_id":"22222222-2222-4222-8222-222222222222","to_employee_id":"33333333-3333-4333-8333-333333333333","reason":"handoff","status":"accepted"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer downstream.Close()

	srv, err := newTestServer(downstream.URL, defaultAdminScopes())
	if err != nil {
		t.Fatal(err)
	}
	tenantID := seedTenantForActor(t, srv, "org-a", "medical", "admin")

	for _, tc := range []struct {
		method string
		path   string
		body   string
		code   int
	}{
		{http.MethodGet, "/api/handoffs", "", http.StatusOK},
		{http.MethodPost, "/api/handoffs", `{"to_employee_id":"33333333-3333-4333-8333-333333333333","reason":"handoff"}`, http.StatusCreated},
		{http.MethodPatch, "/api/handoffs/11111111-1111-4111-8111-111111111111", `{"status":"accepted"}`, http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", tenantID)
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("expected %d for %s %s, got %d body=%s", tc.code, tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if gotTenant != tenantID {
		t.Fatalf("expected tenant header %q, got %q", tenantID, gotTenant)
	}
	if strings.Contains(gotBody, "agent_id") {
		t.Fatalf("handoff payload must not expose agent IDs: %s", gotBody)
	}
	want := []string{
		"GET /v1/handoffs",
		"POST /v1/handoffs",
		"PATCH /v1/handoffs/11111111-1111-4111-8111-111111111111",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected downstream requests:\n%s", strings.Join(requests, "\n"))
	}
}
