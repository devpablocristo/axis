package nexusclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRequestsForOrgSendsOrgHeader(t *testing.T) {
	var gotOrg string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrg = r.Header.Get("X-Org-ID")
		if r.URL.String() != "/v1/requests?limit=1" {
			t.Fatalf("unexpected URL %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key")
	status, raw, err := client.ListRequestsForOrg(context.Background(), "limit=1", "org-a")
	if err != nil {
		t.Fatalf("ListRequestsForOrg err = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%s", status, string(raw))
	}
	if gotOrg != "org-a" {
		t.Fatalf("expected X-Org-ID org-a, got %q", gotOrg)
	}
}
