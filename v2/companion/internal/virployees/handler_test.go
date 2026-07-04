package virployees

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

func TestHandlerCreateValidation(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(`{"name":"","job_role_id":"`+uuid.NewString()+`","supervisor_user_id":"dev-user"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateReturnsAutonomy(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	body := `{"name":"Ops","job_role_id":"` + jobRoleID.String() + `","profile_template_id":"` + profileTemplateID.String() + `","supervisor_user_id":"dev-user","autonomy":"A2"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-1")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		JobRoleID         string `json:"job_role_id"`
		ProfileTemplateID string `json:"profile_template_id"`
		Autonomy          string `json:"autonomy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, ok := raw["virployee_profile"]; ok {
		t.Fatalf("response must not include virployee_profile: %s", rec.Body.String())
	}
	if payload.JobRoleID != jobRoleID.String() {
		t.Fatalf("expected job_role_id %s, got %q", jobRoleID, payload.JobRoleID)
	}
	if payload.ProfileTemplateID != profileTemplateID.String() {
		t.Fatalf("expected profile_template_id %s, got %q", profileTemplateID, payload.ProfileTemplateID)
	}
	if payload.Autonomy != "A2" {
		t.Fatalf("expected autonomy A2, got %q", payload.Autonomy)
	}
}

func TestHandlerListsAutonomyLevels(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/autonomy-levels", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			Level                    string   `json:"level"`
			Name                     string   `json:"name"`
			AllowsRequiredAutonomies []string `json:"allows_required_autonomies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data) != 6 {
		t.Fatalf("expected 6 autonomy levels, got %d", len(payload.Data))
	}
	if payload.Data[0].Level != "A0" || payload.Data[0].Name != "Conversation" {
		t.Fatalf("unexpected first autonomy level: %+v", payload.Data[0])
	}
	var a3Autonomies []string
	for _, item := range payload.Data {
		if item.Level == "A3" {
			a3Autonomies = item.AllowsRequiredAutonomies
		}
	}
	if got, want := strings.Join(a3Autonomies, ","), "A0,A1,A2,A3"; got != want {
		t.Fatalf("expected A3 to allow %s, got %s", want, got)
	}
}

func TestHandlerRoutesLifecycle(t *testing.T) {
	fake := &handlerFakeUseCases{}
	router := testRouter(fake)
	id := uuid.New()

	for _, tc := range []struct {
		method string
		path   string
		action string
	}{
		{http.MethodPost, "/v1/virployees/" + id.String() + "/archive", "archive"},
		{http.MethodPost, "/v1/virployees/" + id.String() + "/unarchive", "unarchive"},
		{http.MethodPost, "/v1/virployees/" + id.String() + "/trash", "trash"},
		{http.MethodPost, "/v1/virployees/" + id.String() + "/restore", "restore"},
		{http.MethodDelete, "/v1/virployees/" + id.String() + "/purge", "purge"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-Actor-ID", "tester")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s expected 204, got %d body=%s", tc.action, rec.Code, rec.Body.String())
		}
		if fake.lastAction != tc.action || fake.lastActor != "tester" {
			t.Fatalf("unexpected action call: action=%s actor=%s", fake.lastAction, fake.lastActor)
		}
	}
}

func TestHandlerInvalidUUID(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/virployees/nope", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func testRouter(ucs UseCasesPort) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ginmw.RegisterHealthEndpoints(router, nil)
	NewHandler(ucs).Routes(router.Group("/v1"))
	return router
}

type handlerFakeUseCases struct {
	lastAction string
	lastActor  string
	lastTenant string
}

func (f *handlerFakeUseCases) Create(_ context.Context, tenantID string, input domain.CreateInput) (domain.Virployee, error) {
	f.lastTenant = tenantID
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: uuid.New(), Name: normalized.Name, JobRoleID: normalized.JobRoleID, ProfileTemplateID: normalized.ProfileTemplateID, SupervisorUserID: normalized.SupervisorUserID, Autonomy: normalized.Autonomy}, nil
}

func (f *handlerFakeUseCases) ListActive(context.Context, string) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) ListArchived(context.Context, string) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) ListTrash(context.Context, string) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) Get(_ context.Context, _ string, id uuid.UUID) (domain.Virployee, error) {
	return domain.Virployee{ID: id, Name: "Ops", JobRoleID: uuid.New(), ProfileTemplateID: uuid.New(), SupervisorUserID: "dev-user", Autonomy: domain.AutonomyA1}, nil
}

func (f *handlerFakeUseCases) Update(_ context.Context, _ string, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: id, Name: normalized.Name, JobRoleID: normalized.JobRoleID, ProfileTemplateID: normalized.ProfileTemplateID, SupervisorUserID: normalized.SupervisorUserID, Autonomy: normalized.Autonomy}, nil
}

func (f *handlerFakeUseCases) Archive(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "archive"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Unarchive(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "unarchive"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Trash(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "trash"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Restore(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "restore"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}

func (f *handlerFakeUseCases) Purge(_ context.Context, tenantID string, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "purge"
	f.lastActor = actor
	f.lastTenant = tenantID
	return nil
}
