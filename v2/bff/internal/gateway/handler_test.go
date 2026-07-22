package gateway

import (
	"context"
	"io"
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
	var gotRole string
	var gotInternalToken string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		gotRole = r.Header.Get("X-Axis-Tenant-Role")
		gotInternalToken = r.Header.Get("X-Axis-Internal-Token")
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
	req.Header.Set("X-Axis-Tenant-Role", "owner")
	req.Header.Set("X-Axis-Internal-Token", "spoofed")

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
	if gotRole != string(tenantdomain.RoleAdmin) {
		t.Fatalf("expected resolved role to replace spoofed role, got %q", gotRole)
	}
	if gotInternalToken != "test-internal-secret" {
		t.Fatalf("expected trusted internal token, got %q", gotInternalToken)
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

func TestGatewayForwardsVirployeeRuntimeContext(t *testing.T) {
	tenantID := uuid.New()
	virployeeID := uuid.New()
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
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"capabilities":[]}`))
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
	req := httptest.NewRequest(http.MethodGet, "/api/virployees/"+virployeeID.String()+"/runtime-context", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/virployees/"+virployeeID.String()+"/runtime-context" {
		t.Fatalf("expected runtime context path, got %q", gotPath)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayForwardsVirployeeDryRun(t *testing.T) {
	tenantID := uuid.New()
	virployeeID := uuid.New()
	var gotPath string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	var gotBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"allowed"}`))
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
	req := httptest.NewRequest(http.MethodPost, "/api/virployees/"+virployeeID.String()+"/dry-run", strings.NewReader(`{"input":"Agendá una reunión"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/virployees/"+virployeeID.String()+"/dry-run" {
		t.Fatalf("expected dry run path, got %q", gotPath)
	}
	if gotBody != `{"input":"Agendá una reunión"}` {
		t.Fatalf("expected body to be forwarded, got %q", gotBody)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayForwardsVirployeeRuns(t *testing.T) {
	tenantID := uuid.New()
	virployeeID := uuid.New()
	var gotPath string
	var gotQuery string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
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
	req := httptest.NewRequest(http.MethodGet, "/api/virployees/"+virployeeID.String()+"/runs?limit=10", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/virployees/"+virployeeID.String()+"/runs" || gotQuery != "limit=10" {
		t.Fatalf("expected runs path/query, got %q?%s", gotPath, gotQuery)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayForwardsApprovalsToNexus(t *testing.T) {
	tenantID := uuid.New()
	approvalID := uuid.New()
	var gotPath string
	var gotQuery string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	var gotBody string
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"approved"}`))
	}))
	defer nexus.Close()

	router := gatewayTestRouterWithTargets(t, &fakeGatewayTenancy{
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
	}, "http://127.0.0.1:1", nexus.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/approvals/"+approvalID.String()+"/approve?view=pending", strings.NewReader(`{"note":"ok"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/approvals/"+approvalID.String()+"/approve" || gotQuery != "view=pending" {
		t.Fatalf("expected approval path/query, got %q?%s", gotPath, gotQuery)
	}
	if gotBody != `{"note":"ok"}` {
		t.Fatalf("expected body forwarded, got %q", gotBody)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayForwardsAdvancedGovernanceAndStripsForgedPermissions(t *testing.T) {
	tenantID := uuid.New()
	var gotPath, gotRole, gotPermissions, gotGrants string
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRole = r.Header.Get("X-Axis-Tenant-Role")
		gotPermissions = r.Header.Get("X-Axis-Permissions")
		gotGrants = r.Header.Get("X-Axis-Role-Grants")
		w.WriteHeader(http.StatusOK)
	}))
	defer nexus.Close()
	router := gatewayTestRouterWithTargets(t, &fakeGatewayTenancy{tenant: tenantdomain.Tenant{ID: tenantID, OrgID: "org-a", ProductSurface: "axis", Status: tenantdomain.StatusActive},
		member: tenantdomain.TenantMember{TenantID: tenantID, UserID: "user-a", Role: tenantdomain.RoleMember, Status: tenantdomain.StatusActive}}, "http://127.0.0.1:1", nexus.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/governance-policy-versions/version-a/simulate", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")
	req.Header.Set("X-Axis-Tenant-Role", "owner")
	req.Header.Set("X-Axis-Permissions", "*")
	req.Header.Set("X-Axis-Role-Grants", "policy_admin")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotPath != "/v1/governance-policy-versions/version-a/simulate" {
		t.Fatalf("advanced governance was not forwarded: status=%d path=%q", rec.Code, gotPath)
	}
	if gotRole != "member" || gotPermissions != "" || gotGrants != "" {
		t.Fatalf("forged authority headers survived: role=%q permissions=%q grants=%q", gotRole, gotPermissions, gotGrants)
	}
}

func TestGatewayValidatesRoleGrantUserBeforeForwarding(t *testing.T) {
	tenantID := uuid.New()
	calls := 0
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))
	defer nexus.Close()
	validator := &fakeSupervisorValidator{err: domainerr.Validation("user_id must reference an active tenant user")}
	router := gatewayTestRouterWithSupervisorAndTargets(t, &fakeGatewayTenancy{
		tenant: tenantdomain.Tenant{ID: tenantID, OrgID: "org-a", ProductSurface: "axis", Status: tenantdomain.StatusActive},
		member: tenantdomain.TenantMember{TenantID: tenantID, UserID: "owner-a", Role: tenantdomain.RoleOwner, Status: tenantdomain.StatusActive},
	}, "http://127.0.0.1:1", nexus.URL, validator)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/role-grants", strings.NewReader(`{"user_id":"inactive-user","role":"auditor"}`))
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "owner-a")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("expected inactive user to be rejected before forwarding, status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
	}
	if validator.lastTenantID != tenantID.String() || validator.lastUserID != "inactive-user" {
		t.Fatalf("unexpected user validation tenant=%q user=%q", validator.lastTenantID, validator.lastUserID)
	}
}

func TestGatewayForwardsApprovalGetToNexus(t *testing.T) {
	tenantID := uuid.New()
	approvalID := uuid.New()
	var gotPath string
	var gotTenant string
	var gotActor string
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotActor = r.Header.Get("X-Actor-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer nexus.Close()

	router := gatewayTestRouterWithTargets(t, &fakeGatewayTenancy{
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
	}, "http://127.0.0.1:1", nexus.URL)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/approvals/"+approvalID.String(), nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/approvals/"+approvalID.String() {
		t.Fatalf("expected approval path, got %q", gotPath)
	}
	if gotTenant != tenantID.String() || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q actor=%q", gotTenant, gotActor)
	}
}

func TestGatewayForwardsVirployeeExecutionGate(t *testing.T) {
	tenantID := uuid.New()
	virployeeID := uuid.New()
	var gotPath string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	var gotBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"execution_gate":{"decision":"blocked"}}`))
	}))
	defer downstream.Close()

	requestBody := `{"input":"Agendá una reunión","confirmed_draft":{"action":"calendar.events.create","kind":"calendar_event","fields":[{"key":"title","value":"Reunión"}]}}`
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
	req := httptest.NewRequest(http.MethodPost, "/api/virployees/"+virployeeID.String()+"/execution-gate", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/virployees/"+virployeeID.String()+"/execution-gate" {
		t.Fatalf("expected execution gate path, got %q", gotPath)
	}
	if gotBody != requestBody {
		t.Fatalf("expected body to be forwarded, got %q", gotBody)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
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

func TestGatewayForwardsCapabilitiesWithResolvedTenantHeaders(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities?lifecycle=active", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/capabilities" {
		t.Fatalf("expected /v1/capabilities, got %q", gotPath)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayForwardsProfileTemplatesWithResolvedTenantHeaders(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodGet, "/api/profile-templates?lifecycle=active", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/profile-templates" {
		t.Fatalf("expected /v1/profile-templates, got %q", gotPath)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q", gotTenant, gotOrg, gotProduct, gotActor)
	}
}

func TestGatewayForwardsGovernanceCheckToNexusWithResolvedTenantHeaders(t *testing.T) {
	tenantID := uuid.New()
	var gotPath string
	var gotQuery string
	var gotTenant string
	var gotOrg string
	var gotProduct string
	var gotActor string
	var gotForwardedBy string
	var gotBody string
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotOrg = r.Header.Get("X-Axis-Org-ID")
		gotProduct = r.Header.Get("X-Product-Surface")
		gotActor = r.Header.Get("X-Actor-ID")
		gotForwardedBy = r.Header.Get("X-Axis-Forwarded-By")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"decision":"require_approval"}`))
	}))
	defer nexus.Close()

	router := gatewayTestRouterWithTargets(t, &fakeGatewayTenancy{
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
	}, "http://127.0.0.1:1", nexus.URL)

	requestBody := `{"requester_id":"virployee-1","action_type":"calendar.events.delete","reason":"cleanup"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/governance/check?mode=simulation", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "user-a")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected downstream status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/governance/check" {
		t.Fatalf("expected /v1/governance/check, got %q", gotPath)
	}
	if gotQuery != "mode=simulation" {
		t.Fatalf("expected query to be forwarded, got %q", gotQuery)
	}
	if gotBody != requestBody {
		t.Fatalf("expected body to be forwarded, got %q", gotBody)
	}
	if gotTenant != tenantID.String() || gotOrg != "org-a" || gotProduct != "axis" || gotActor != "user-a" || gotForwardedBy != "bff-v2" {
		t.Fatalf("unexpected forwarded headers tenant=%q org=%q product=%q actor=%q forwarded_by=%q", gotTenant, gotOrg, gotProduct, gotActor, gotForwardedBy)
	}
}

func TestGatewayRoutesOperationalJobByServiceAndStripsForgedAuthority(t *testing.T) {
	tenantID := uuid.New()
	var gotPath, gotActor, gotRole, gotPermissions string
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotActor = r.Header.Get("X-Actor-ID")
		gotRole = r.Header.Get("X-Axis-Tenant-Role")
		gotPermissions = r.Header.Get("X-Permissions")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	}))
	defer nexus.Close()
	companion := httptest.NewServer(http.NotFoundHandler())
	defer companion.Close()
	router := gatewayTestRouterWithTargets(t, &fakeGatewayTenancy{tenant: tenantdomain.Tenant{ID: tenantID, OrgID: "org-a", ProductSurface: "axis", Status: tenantdomain.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, member: tenantdomain.TenantMember{TenantID: tenantID, UserID: "operator-a", Role: tenantdomain.RoleMember, Status: tenantdomain.StatusActive}}, companion.URL, nexus.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/operations/jobs/nexus/11111111-1111-1111-1111-111111111111/replay", strings.NewReader(`{}`))
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "operator-a")
	req.Header.Set("X-Axis-Tenant-Role", "owner")
	req.Header.Set("X-Permissions", "*")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/operations/jobs/11111111-1111-1111-1111-111111111111/replay" {
		t.Fatalf("unexpected downstream path %q", gotPath)
	}
	if gotActor != "operator-a" || gotRole != string(tenantdomain.RoleMember) || gotPermissions != "" {
		t.Fatalf("authority headers were not derived safely actor=%q role=%q permissions=%q", gotActor, gotRole, gotPermissions)
	}
}

func TestOperationsOverviewReportsPartialWhenOneServiceIsDown(t *testing.T) {
	tenantID := uuid.New()
	companion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","fleet":{"ready":2}}`))
	}))
	defer companion.Close()
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer nexus.Close()
	router := gatewayTestRouterWithTargets(t, &fakeGatewayTenancy{tenant: tenantdomain.Tenant{ID: tenantID, OrgID: "org-a", ProductSurface: "axis", Status: tenantdomain.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, member: tenantdomain.TenantMember{TenantID: tenantID, UserID: "auditor-a", Role: tenantdomain.RoleAdmin, Status: tenantdomain.StatusActive}}, companion.URL, nexus.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/operations/overview", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-Actor-ID", "auditor-a")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"partial"`) || !strings.Contains(rec.Body.String(), `"nexus":{"status":"unavailable"}`) {
		t.Fatalf("partial outage must remain visible: %d %s", rec.Code, rec.Body.String())
	}
}

func gatewayTestRouter(t *testing.T, tenancy TenancyPort, companionURL string) *gin.Engine {
	return gatewayTestRouterWithSupervisor(t, tenancy, companionURL, nil)
}

func gatewayTestRouterWithSupervisor(t *testing.T, tenancy TenancyPort, companionURL string, supervisor SupervisorValidatorPort) *gin.Engine {
	return gatewayTestRouterWithSupervisorAndTargets(t, tenancy, companionURL, companionURL, supervisor)
}

func gatewayTestRouterWithTargets(t *testing.T, tenancy TenancyPort, companionURL string, nexusURL string) *gin.Engine {
	return gatewayTestRouterWithSupervisorAndTargets(t, tenancy, companionURL, nexusURL, nil)
}

func gatewayTestRouterWithSupervisorAndTargets(t *testing.T, tenancy TenancyPort, companionURL string, nexusURL string, supervisor SupervisorValidatorPort) *gin.Engine {
	t.Helper()
	uc, err := NewUseCases(tenancy, companionURL, nexusURL)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(uc, Options{DefaultPrincipalID: "dev-user", InternalAuthSecret: "test-internal-secret", SupervisorValidator: supervisor}).Routes(router.Group("/api"))
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
