package watchers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devpablocristo/companion/internal/watchers/handler/dto"
)

func TestHandlerCreateWatcherDerivesOrgFromPrincipal(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo(), nil)).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/watchers", strings.NewReader(`{
		"name":"Stock",
		"watcher_type":"low_stock",
		"enabled":true
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.WatcherResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.OrgID != "org-a" {
		t.Fatalf("expected org-a, got %q", resp.OrgID)
	}
}

func TestHandlerCreateWatcherRejectsForeignOrg(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo(), nil)).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/watchers", strings.NewReader(`{
		"org_id":"org-b",
		"name":"Stock",
		"watcher_type":"low_stock",
		"enabled":true
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateWatcherRequiresWriteScopeWhenAuthenticated(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo(), nil)).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/watchers", strings.NewReader(`{
		"org_id":"org-a",
		"name":"Stock",
		"watcher_type":"low_stock",
		"enabled":true
	}`))
	req.Header.Set("X-Org-ID", "org-a")
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", scopeCompanionWatchersRead)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
