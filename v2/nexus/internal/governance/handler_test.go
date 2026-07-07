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
	router := setupGovernanceRouter(fakeActionTypeReader{
		"calendar.events.delete": {
			ActionTypeKey: "calendar.events.delete",
			RiskClass:     actiondomain.RiskClassHigh,
			Enabled:       true,
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/governance/check", strings.NewReader(`{
		"requester_type":"virployee",
		"requester_id":"virployee-1",
		"action_type":"calendar.events.delete",
		"target_system":"calendar"
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
}

func TestHandlerCheckValidation(t *testing.T) {
	router := setupGovernanceRouter(fakeActionTypeReader{})

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

func setupGovernanceRouter(reader fakeActionTypeReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	uc := NewUseCases(reader)
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
