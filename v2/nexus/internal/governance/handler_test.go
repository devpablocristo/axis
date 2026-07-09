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
