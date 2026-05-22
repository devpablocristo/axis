package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/devpablocristo/nexus/governance/internal/dashboard"
	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

type fakeRequestLister struct {
	mu       sync.RWMutex
	requests []requestdomain.Request
	// Capturado en la última call para que los tests verifiquen tenancy.
	lastOrgID    *string
	lastAllowAll bool
}

func (r *fakeRequestLister) List(_ context.Context, _, _ string, _ int, orgID *string, allowAll bool) ([]requestdomain.Request, error) {
	r.mu.Lock()
	r.lastOrgID = orgID
	r.lastAllowAll = allowAll
	r.mu.Unlock()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.requests, nil
}

func TestDashboardEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	dashboard.NewHandler(&fakeRequestLister{}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/metrics/summary", nil))
	if rec.Code != http.StatusOK { t.Fatalf("expected 200, got %d", rec.Code) }
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total_requests"].(float64) != 0 { t.Fatalf("expected 0, got %v", resp["total_requests"]) }
}

// V7 lockdown: caller con auth context pero sin scope ni cross_org debe
// recibir 403. Anti-leak por default.
func TestDashboard_RejectsCallerWithoutScope(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	dashboard.NewHandler(&fakeRequestLister{}).Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/summary", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", "nexus:requests:read")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// Caller con scope dashboard:read + X-Org-ID debe filtrar a ese org.
func TestDashboard_TenantScopedFiltering(t *testing.T) {
	t.Parallel()
	lister := &fakeRequestLister{}
	mux := http.NewServeMux()
	dashboard.NewHandler(lister).Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/summary", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", "nexus:dashboard:read")
	req.Header.Set("X-Org-ID", "orgA")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if lister.lastAllowAll {
		t.Error("expected allowAll=false for tenant-scoped caller")
	}
	if lister.lastOrgID == nil || *lister.lastOrgID != "orgA" {
		t.Errorf("expected org filter=orgA, got %v", lister.lastOrgID)
	}
}

// Caller con scope cross_org sin X-Org-ID ve agregado global.
func TestDashboard_CrossOrgScopeSeesAll(t *testing.T) {
	t.Parallel()
	lister := &fakeRequestLister{}
	mux := http.NewServeMux()
	dashboard.NewHandler(lister).Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/summary", nil)
	req.Header.Set("X-Auth-Method", "jwt")
	req.Header.Set("X-Auth-Scopes", "nexus:dashboard:read nexus:cross_org")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !lister.lastAllowAll {
		t.Error("expected allowAll=true for cross-org admin without X-Org-ID")
	}
}

func TestDashboardWithData(t *testing.T) {
	t.Parallel()
	lister := &fakeRequestLister{requests: []requestdomain.Request{
		{ID: uuid.New(), Status: requestdomain.StatusAllowed},
		{ID: uuid.New(), Status: requestdomain.StatusAllowed},
		{ID: uuid.New(), Status: requestdomain.StatusDenied},
		{ID: uuid.New(), Status: requestdomain.StatusPendingApproval},
		{ID: uuid.New(), Status: requestdomain.StatusExecuted},
	}}
	mux := http.NewServeMux()
	dashboard.NewHandler(lister).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/metrics/summary", nil))
	if rec.Code != http.StatusOK { t.Fatalf("expected 200, got %d", rec.Code) }
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total_requests"].(float64) != 5 { t.Fatalf("expected 5, got %v", resp["total_requests"]) }
	if resp["allowed"].(float64) != 2 { t.Fatalf("expected 2 allowed, got %v", resp["allowed"]) }
}
