package actiontypes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/handler/dto"
)

func TestHandlerCreateAndListActionTypes(t *testing.T) {
	router := setupActionTypeRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/action-types", strings.NewReader(`{
		"action_type_key":"calendar.events.create",
		"name":"Create event",
		"risk_class":"medium"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "organization-1")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created dto.ActionTypeResponse
	decodeJSON(t, rec, &created)
	if created.OrgID != "organization-1" {
		t.Fatalf("organization id = %q, want organization-1", created.OrgID)
	}
	if created.RiskClass != "medium" {
		t.Fatalf("risk class = %q, want medium", created.RiskClass)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/action-types", nil)
	req.Header.Set("X-Org-ID", "organization-1")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list dto.ListActionTypesResponse
	decodeJSON(t, rec, &list)
	if len(list.Data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list.Data))
	}
}

func TestHandlerCreateValidation(t *testing.T) {
	router := setupActionTypeRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/action-types", strings.NewReader(`{
		"action_type_key":"Calendar Events Create",
		"name":"Create event"
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerUpdateActionType(t *testing.T) {
	router := setupActionTypeRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/action-types", strings.NewReader(`{
		"action_type_key":"calendar.events.delete",
		"name":"Delete event",
		"risk_class":"high"
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created dto.ActionTypeResponse
	decodeJSON(t, rec, &created)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/v1/action-types/"+created.ID, strings.NewReader(`{
		"name":"Delete calendar event",
		"description":"Requires approval",
		"category":"calendar",
		"risk_class":"high",
		"enabled":false
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated dto.ActionTypeResponse
	decodeJSON(t, rec, &updated)
	if updated.Enabled {
		t.Fatal("expected enabled=false after update")
	}
}

func setupActionTypeRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	repo := newFakeActionTypeRepo()
	uc := NewUseCases(repo)
	handler := NewHandler(uc)
	router := gin.New()
	handler.Routes(router.Group("/v1"))
	return router
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode json: %v; body=%s", err, rec.Body.String())
	}
}
