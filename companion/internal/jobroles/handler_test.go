package jobroles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devpablocristo/companion/internal/identityctx"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

func TestHandlerPutJobRole(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	body := bytes.NewBufferString(`{"name":"Billing Specialist","slug":"billing-specialist","mission":"Keep billing clean.","responsibilities":[{"title":"Review invoices","priority":1}],"recommended_capabilities":["billing.read"],"default_autonomy_level":"A2"}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/job-roles/billing-specialist?org_id=org-a&product_surface=axis", body)
	req = withJobRolePrincipal(req, []string{"companion:virployees:admin"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var role JobRole
	if err := json.Unmarshal(res.Body.Bytes(), &role); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(role.JobRoleID); err != nil {
		t.Fatalf("expected public UUID job_role_id, got %q", role.JobRoleID)
	}
	if role.JobRoleKey != "billing-specialist" || role.OrgID != "org-a" || role.ProductSurface != "axis" {
		t.Fatalf("unexpected role: %+v", role)
	}
}

func TestHandlerPostJobRoleGeneratesID(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	body := bytes.NewBufferString(`{"name":"Medical Case Assistant","mission":"Review medical cases.","responsibilities":[{"title":"Summarize case","priority":1}],"recommended_capability_ids":["11111111-1111-4111-8111-111111111111"],"default_autonomy":"A2","success_criteria":[{"title":"Clear summary","target_value":"accepted"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/job-roles?org_id=org-a&product_surface=axis&tenant_id=22222222-2222-4222-8222-222222222222", body)
	req = withJobRolePrincipal(req, []string{"companion:virployees:admin"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", res.Code, res.Body.String())
	}
	var role JobRole
	if err := json.Unmarshal(res.Body.Bytes(), &role); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(role.JobRoleID); err != nil {
		t.Fatalf("expected generated UUID job_role_id, got %q", role.JobRoleID)
	}
	if role.JobRoleKey != "medical-case-assistant" || role.TenantID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("unexpected generated role: %+v", role)
	}
	if len(role.SuccessCriteria) != 1 || role.SuccessCriteria[0].Title != "Clear summary" {
		t.Fatalf("expected structured success criteria, got %+v", role.SuccessCriteria)
	}
}

func TestHandlerListJobRolesRequiresScope(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/job-roles?org_id=org-a&product_surface=axis", nil)
	req = withJobRolePrincipal(req, []string{"companion:tasks:read"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestHandlerJobRoleLifecycleAndVersions(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/job-roles/billing-specialist?org_id=org-a&product_surface=axis", bytes.NewBufferString(`{"name":"Billing Specialist","slug":"billing-specialist","default_autonomy_level":"A2"}`))
	req = withJobRolePrincipal(req, []string{"companion:virployees:admin"})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected put 200, got %d body=%s", res.Code, res.Body.String())
	}

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/job-roles/billing-specialist/status?org_id=org-a&product_surface=axis", `{"status":"archived"}`},
		{http.MethodPost, "/v1/job-roles/billing-specialist/status?org_id=org-a&product_surface=axis", `{"status":"active"}`},
		{http.MethodPost, "/v1/job-roles/billing-specialist/status?org_id=org-a&product_surface=axis", `{"status":"trash"}`},
		{http.MethodPost, "/v1/job-roles/billing-specialist/status?org_id=org-a&product_surface=axis", `{"status":"active"}`},
		{http.MethodPost, "/v1/job-roles/billing-specialist/archive?org_id=org-a&product_surface=axis", ""},
		{http.MethodPost, "/v1/job-roles/billing-specialist/trash?org_id=org-a&product_surface=axis", ""},
		{http.MethodPost, "/v1/job-roles/billing-specialist/restore?org_id=org-a&product_surface=axis", ""},
		{http.MethodGet, "/v1/job-roles/billing-specialist/versions?org_id=org-a&product_surface=axis", ""},
	} {
		req = httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req = withJobRolePrincipal(req, []string{"companion:virployees:admin"})
		res = httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d body=%s", tc.path, res.Code, res.Body.String())
		}
	}
}

func withJobRolePrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "org-a", Actor: "admin", Scopes: scopes, AuthMethod: "internal_jwt", Claims: map[string]any{"product_surface": "axis"}}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
