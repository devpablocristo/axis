package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestGatewayRequiresTenant(t *testing.T) {
	router := gatewayTestRouter(t, &fakeGatewayTenancy{}, "http://127.0.0.1:1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/virployees", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayRejectsTenantWithoutMembership(t *testing.T) {
	tenantID := uuid.New()
	router := gatewayTestRouter(t, &fakeGatewayTenancy{err: domainerr.Forbidden("principal is not a member of the requested tenant")}, "http://127.0.0.1:1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/virployees", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGatewayForwardsVirployeesWithResolvedTenantHeaders(t *testing.T) {
	tenantID := uuid.New()
	var gotPath string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer downstream.Close()

	router := gatewayTestRouter(t, &fakeGatewayTenancy{
		tenant: tenantdomain.Tenant{
			ID:             tenantID,
			OrgID:          "org-a",
			ProductSurface: "axis",
			Status:         tenantdomain.StatusActive,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		},
		member: tenantdomain.TenantMember{
			TenantID: tenantID,
			UserID:   "user-a",
			Role:     tenantdomain.RoleAdmin,
			Status:   tenantdomain.StatusActive,
		},
	}, downstream.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/virployees?view=active", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/virployees" {
		t.Fatalf("expected /v1/virployees, got %q", gotPath)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayValidatesVirployeeSupervisorBeforeForwarding(t *testing.T) {
	tenantID := uuid.New()
	calls := 0
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))
	defer downstream.Close()

	validator := &fakeSupervisorValidator{err: domainerr.Validation("supervisor_user_id must reference an active tenant user")}
	router := gatewayTestRouterWithSupervisor(t, &fakeGatewayTenancy{
		tenant: tenantdomain.Tenant{
			ID:             tenantID,
			OrgID:          "org-a",
			ProductSurface: "axis",
			Status:         tenantdomain.StatusActive,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		},
		member: tenantdomain.TenantMember{
			TenantID: tenantID,
			UserID:   "user-a",
			Role:     tenantdomain.RoleAdmin,
			Status:   tenantdomain.StatusActive,
		},
	}, downstream.URL, validator)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/virployees", strings.NewReader(`{"name":"Ops","supervisor_user_id":"missing-user"}`))
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if calls != 0 {
		t.Fatalf("expected downstream not to be called, got %d calls", calls)
	}
	if validator.lastTenantID != tenantID.String() || validator.lastUserID != "missing-user" {
		t.Fatalf("unexpected supervisor validation tenant=%q user=%q", validator.lastTenantID, validator.lastUserID)
	}
}

func TestGatewayForwardsVirployeeAutonomyLevels(t *testing.T) {
	tenantID := uuid.New()
	var gotPath string
	var gotQuery string
	var gotTenant string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotTenant = r.Header.Get("X-Tenant-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer downstream.Close()

	router := gatewayTestRouter(t, &fakeGatewayTenancy{
		tenant: tenantdomain.Tenant{
			ID:             tenantID,
			OrgID:          "org-a",
			ProductSurface: "axis",
			Status:         tenantdomain.StatusActive,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		},
		member: tenantdomain.TenantMember{
			TenantID: tenantID,
			UserID:   "user-a",
			Role:     tenantdomain.RoleAdmin,
			Status:   tenantdomain.StatusActive,
		},
	}, downstream.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/virployees/autonomy-levels?scope=all", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/virployees/autonomy-levels" {
		t.Fatalf("expected /v1/virployees/autonomy-levels, got %q", gotPath)
	}
	if gotQuery != "scope=all" {
		t.Fatalf("expected query to be forwarded, got %q", gotQuery)
	}
	if gotTenant != tenantID.String() {
		t.Fatalf("expected resolved tenant header, got %q", gotTenant)
	}
}

func TestGatewayForwardsJobRolesWithResolvedTenantHeaders(t *testing.T) {
	tenantID := uuid.New()
	var gotPath string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	var gotForwardedBy string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		gotForwardedBy = r.Header.Get("X-Axis-Forwarded-By")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer downstream.Close()

	router := gatewayTestRouter(t, &fakeGatewayTenancy{
		tenant: tenantdomain.Tenant{
			ID:             tenantID,
			OrgID:          "org-a",
			ProductSurface: "axis",
			Status:         tenantdomain.StatusActive,
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		},
		member: tenantdomain.TenantMember{
			TenantID: tenantID,
			UserID:   "user-a",
			Role:     tenantdomain.RoleAdmin,
			Status:   tenantdomain.StatusActive,
		},
	}, downstream.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/job-roles?lifecycle=active", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/job-roles" {
		t.Fatalf("expected /v1/job-roles, got %q", gotPath)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" || gotForwardedBy != "bff-v2" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q forwarded_by=%q", gotTenant, gotOrg, gotProduct, gotActor, gotForwardedBy)
	}
}

func gatewayTestRouter(t *testing.T, tenancy TenancyPort, companionURL string) *gin.Engine {
	return gatewayTestRouterWithSupervisor(t, tenancy, companionURL, nil)
}

func gatewayTestRouterWithSupervisor(t *testing.T, tenancy TenancyPort, companionURL string, supervisor SupervisorValidatorPort) *gin.Engine {
	t.Helper()
	uc, err := NewUseCases(tenancy, companionURL)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(uc, Options{DefaultPrincipalID: "dev-user", SupervisorValidator: supervisor}).Routes(router.Group("/api"))
	return router
}

type fakeGatewayTenancy struct {
	tenant tenantdomain.Tenant
	member tenantdomain.TenantMember
	err    error
}

func (f *fakeGatewayTenancy) ResolveAccess(context.Context, string, string) (tenantdomain.Tenant, tenantdomain.TenantMember, error) {
	if f.err != nil {
		return tenantdomain.Tenant{}, tenantdomain.TenantMember{}, f.err
	}
	return f.tenant, f.member, nil
}

type fakeSupervisorValidator struct {
	lastTenantID string
	lastUserID   string
	err          error
}

func (f *fakeSupervisorValidator) EnsureActive(_ context.Context, tenantID, userID string) error {
	f.lastTenantID = tenantID
	f.lastUserID = userID
	return f.err
}
