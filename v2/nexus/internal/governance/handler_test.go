package governance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	actiondomain "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/governance/handler/dto"
)

func TestHandlerCheckRequiresApprovalForHighRisk(t *testing.T) {
	approvalID := "00000000-0000-0000-0000-000000000999"
	router := setupGovernanceRouter(fakeActionTypeReader{
		"calendar.events.delete": {
			ActionTypeKey: "calendar.events.delete",
			RiskClass:     actiondomain.RiskClassHigh,
			Enabled:       true,
		},
	}, &fakeCheckRecorder{approvalID: approvalID})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/governance/check", strings.NewReader(`{
		"requester_type":"virployee",
		"requester_id":"virployee-1",
		"action_type":"calendar.events.delete",
		"target_system":"calendar",
		"binding_hash":"binding-123"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out dto.CheckResponse
	decodeGovernanceJSON(t, rec, &out)
	if out.Decision != "require_approval" || out.Status != "pending_approval" {
		t.Fatalf("unexpected check response: %+v", out)
	}
	if !out.WouldRequireApproval {
		t.Fatal("expected would_require_approval=true")
	}
	if out.BindingHash != "binding-123" {
		t.Fatalf("expected binding hash, got %+v", out)
	}
	if out.ApprovalID != approvalID || out.ApprovalStatus != "pending" {
		t.Fatalf("expected approval metadata, got %+v", out)
	}
}

func TestHandlerCheckDeniesDisabledActionType(t *testing.T) {
	router := setupGovernanceRouter(fakeActionTypeReader{
		"calendar.events.update": {
			ActionTypeKey: "calendar.events.update",
			RiskClass:     actiondomain.RiskClassMedium,
			Enabled:       false,
		},
	}, &fakeCheckRecorder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/governance/check", strings.NewReader(`{
		"requester_type":"virployee",
		"requester_id":"virployee-1",
		"action_type":"calendar.events.update",
		"target_system":"calendar",
		"binding_hash":"binding-denied"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out dto.CheckResponse
	decodeGovernanceJSON(t, rec, &out)
	if out.Decision != "deny" || out.Status != "denied" {
		t.Fatalf("unexpected check response: %+v", out)
	}
	if out.WouldRequireApproval || out.ApprovalID != "" || out.ApprovalStatus != "" {
		t.Fatalf("deny should not include approval metadata: %+v", out)
	}
}

func TestHandlerDerivesMembershipAndDiscardsForgedFunctionalRoles(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	router := setupGovernanceRouter(fakeActionTypeReader{
		"calendar.events.read": {ActionTypeKey: "calendar.events.read", RiskClass: actiondomain.RiskClassLow, Enabled: true},
	}, recorder)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/governance/check", strings.NewReader(`{
		"requester_type":"human",
		"requester_id":"more-privileged-user",
		"action_type":"calendar.events.read",
		"membership_role":"owner",
		"functional_roles":["policy_admin"],
		"functional_scopes":["*"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	req.Header.Set("X-Axis-Tenant-Role", "member")
	req.Header.Set("X-Actor-ID", "user-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || len(recorder.rows) != 1 {
		t.Fatalf("expected a recorded check, status=%d body=%s rows=%d", rec.Code, rec.Body.String(), len(recorder.rows))
	}
	input := recorder.rows[0].input
	if input.RequesterID != "user-1" || input.MembershipRole != "member" || len(input.FunctionalRoles) != 0 || len(input.FunctionalScopes) != 0 {
		t.Fatalf("forged request authority was accepted: %+v", input)
	}
}

func TestHandlerCheckValidation(t *testing.T) {
	router := setupGovernanceRouter(fakeActionTypeReader{}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/governance/check", strings.NewReader(`{
		"requester_id":"virployee-1",
		"action_type":"calendar.events.create"
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func setupGovernanceRouter(reader fakeActionTypeReader, recorder CheckRecorderPort) *gin.Engine {
	gin.SetMode(gin.TestMode)
	uc := NewUseCases(reader, recorder)
	handler := NewHandler(uc)
	router := gin.New()
	handler.Routes(router.Group("/v1"))
	return router
}

func decodeGovernanceJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode json: %v; body=%s", err, rec.Body.String())
	}
}
