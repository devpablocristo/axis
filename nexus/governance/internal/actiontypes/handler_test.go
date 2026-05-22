package actiontypes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	domain "github.com/devpablocristo/nexus/governance/internal/actiontypes/usecases/domain"
)

// --- fakes ---

type fakeActionTypeRepo struct {
	items map[uuid.UUID]domain.ActionType
}

func newFakeRepo() *fakeActionTypeRepo {
	return &fakeActionTypeRepo{items: make(map[uuid.UUID]domain.ActionType)}
}

func (f *fakeActionTypeRepo) Create(_ context.Context, at domain.ActionType) (domain.ActionType, error) {
	at.ID = uuid.New()
	now := time.Now().UTC()
	at.CreatedAt = now
	at.UpdatedAt = now
	f.items[at.ID] = at
	return at, nil
}

func (f *fakeActionTypeRepo) GetByID(_ context.Context, id uuid.UUID) (domain.ActionType, error) {
	at, ok := f.items[id]
	if !ok {
		return domain.ActionType{}, domainerr.NotFound("not found")
	}
	return at, nil
}

func (f *fakeActionTypeRepo) GetByName(_ context.Context, name string) (domain.ActionType, error) {
	for _, at := range f.items {
		if at.Name == name {
			return at, nil
		}
	}
	return domain.ActionType{}, domainerr.NotFound("not found")
}

func (f *fakeActionTypeRepo) GetByNameForOrg(_ context.Context, name string, orgID *string) (domain.ActionType, error) {
	var global *domain.ActionType
	for _, at := range f.items {
		if at.Name != name {
			continue
		}
		if orgID != nil && at.OrgID != nil && *at.OrgID == *orgID {
			return at, nil
		}
		if at.OrgID == nil {
			copy := at
			global = &copy
		}
	}
	if global != nil {
		return *global, nil
	}
	return domain.ActionType{}, domainerr.NotFound("not found")
}

func (f *fakeActionTypeRepo) List(_ context.Context) ([]domain.ActionType, error) {
	out := make([]domain.ActionType, 0, len(f.items))
	for _, at := range f.items {
		out = append(out, at)
	}
	return out, nil
}

func (f *fakeActionTypeRepo) ListForOrg(_ context.Context, orgID *string, includeGlobal bool) ([]domain.ActionType, error) {
	out := make([]domain.ActionType, 0, len(f.items))
	for _, at := range f.items {
		if at.OrgID == nil {
			if includeGlobal {
				out = append(out, at)
			}
			continue
		}
		if orgID != nil && *at.OrgID == *orgID {
			out = append(out, at)
		}
	}
	return out, nil
}

func (f *fakeActionTypeRepo) Update(_ context.Context, at domain.ActionType) (domain.ActionType, error) {
	if _, ok := f.items[at.ID]; !ok {
		return domain.ActionType{}, domainerr.NotFound("not found")
	}
	at.UpdatedAt = time.Now().UTC()
	f.items[at.ID] = at
	return at, nil
}

func (f *fakeActionTypeRepo) DeleteByID(_ context.Context, id uuid.UUID) error {
	if _, ok := f.items[id]; !ok {
		return domainerr.NotFound("not found")
	}
	delete(f.items, id)
	return nil
}

// --- helpers ---

func setupMux() (*http.ServeMux, *fakeActionTypeRepo) {
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	h := NewHandler(uc)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, repo
}

func doRequest(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// --- tests ---

func TestActionTypes_CreateAndGet(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/action-types", `{
		"name": "treasury.transfer",
		"category": "treasury",
		"risk_class": "high",
		"reversible": false,
		"requires_break_glass": true
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	id := created["id"].(string)
	if created["name"] != "treasury.transfer" {
		t.Fatalf("unexpected name: %v", created["name"])
	}
	if created["risk_class"] != "high" {
		t.Fatalf("unexpected risk_class: %v", created["risk_class"])
	}
	if created["enabled"] != true {
		t.Fatal("expected enabled=true by default")
	}

	// GET by ID
	rec = doRequest(mux, "GET", "/v1/action-types/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestActionTypes_CreateValidation(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/action-types", `{"category":"infra"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestActionTypes_CreateDefaultsRiskClass(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/action-types", `{"name":"simple.action"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["risk_class"] != "low" {
		t.Fatalf("expected default risk_class=low, got %v", resp["risk_class"])
	}
}

func TestActionTypes_List(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	doRequest(mux, "POST", "/v1/action-types", `{"name":"a.one"}`)
	doRequest(mux, "POST", "/v1/action-types", `{"name":"b.two"}`)

	rec := doRequest(mux, "GET", "/v1/action-types", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}
}

func TestActionTypes_Update(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/action-types", `{"name":"iam.grant","risk_class":"low"}`)
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	rec = doRequest(mux, "PATCH", "/v1/action-types/"+id, `{"risk_class":"critical","requires_break_glass":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated["risk_class"] != "critical" {
		t.Fatalf("expected risk_class=critical, got %v", updated["risk_class"])
	}
	if updated["requires_break_glass"] != true {
		t.Fatal("expected requires_break_glass=true")
	}
}

func TestActionTypes_Delete(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "POST", "/v1/action-types", `{"name":"temp.action"}`)
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	rec = doRequest(mux, "DELETE", "/v1/action-types/"+id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	rec = doRequest(mux, "GET", "/v1/action-types/"+id, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestActionTypes_GetNotFound(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "GET", fmt.Sprintf("/v1/action-types/%s", uuid.New()), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestActionTypes_InvalidID(t *testing.T) {
	t.Parallel()
	mux, _ := setupMux()

	rec := doRequest(mux, "GET", "/v1/action-types/not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid uuid, got %d", rec.Code)
	}
}
