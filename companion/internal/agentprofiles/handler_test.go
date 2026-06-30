package agentprofiles

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

func TestHandlerEmployeeProfileDefaultsEnabled(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	body := bytes.NewBufferString(`{"profile_key":"axis.ops.billing.v1","family_id":"axis.ops.billing","version_label":"v1","name":"Billing Profile","system_prompt":"Handle billing.","max_autonomy":"A1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/employee-profiles", body)
	req = withProfilePrincipal(req, []string{"companion:employee_profiles:admin"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", res.Code, res.Body.String())
	}
	var profile EmployeeProfile
	if err := json.Unmarshal(res.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if !profile.Enabled {
		t.Fatalf("expected default enabled profile, got %+v", profile)
	}
	if profile.FamilyID != "axis.ops.billing" || profile.VersionLabel != "v1" {
		t.Fatalf("expected family/version, got %+v", profile)
	}
}

func TestHandlerRejectsMissingScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/employee-profiles", nil)
	req = withProfilePrincipal(req, []string{"companion:tasks:read"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestHandlerDoesNotRegisterAgentProfilesPublicRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/agent-profiles", nil)
	req = withProfilePrincipal(req, []string{"companion:employee_profiles:read"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestHandlerEmployeeProfilePublicSurface(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
	body := bytes.NewBufferString(`{"name":"Medical Case Assistant","system_prompt":"Assist medical case review.","max_autonomy":"A2"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/employee-profiles", body)
	req = withProfilePrincipal(req, []string{"companion:employee_profiles:admin"})
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", res.Code, res.Body.String())
	}
	var profile EmployeeProfile
	if err := json.Unmarshal(res.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(profile.ProfileID); err != nil {
		t.Fatalf("expected public UUID profile_id, got %q", profile.ProfileID)
	}
	if profile.ProfileKey != "employee.medical.case.assistant.v1" {
		t.Fatalf("expected generated profile_key, got %+v", profile)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/employee-profiles/"+profile.ProfileID+"/status", bytes.NewBufferString(`{"status":"archived"}`))
	req = withProfilePrincipal(req, []string{"companion:employee_profiles:admin"})
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Status != "archived" {
		t.Fatalf("expected archived employee profile, got %+v", profile)
	}
}

func withProfilePrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "axis", Actor: "admin", Scopes: scopes, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
