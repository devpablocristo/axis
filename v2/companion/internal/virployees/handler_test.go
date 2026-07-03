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
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(`{"name":"","role":"ops","supervisor_user_id":"`+uuid.NewString()+`"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateReturnsAutonomy(t *testing.T) {
	router := testRouter(&handlerFakeUseCases{})
	rec := httptest.NewRecorder()
	body := `{"name":"Ops","role":"ops","supervisor_user_id":"` + uuid.NewString() + `","autonomy":"A2"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/virployees", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Autonomy string `json:"autonomy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
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
			Level                string `json:"level"`
			Name                 string `json:"name"`
			AllowedActionClasses []struct {
				Class            string `json:"class"`
				RequiresApproval bool   `json:"requires_approval"`
			} `json:"allowed_action_classes"`
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
	var a3Actions []struct {
		Class            string `json:"class"`
		RequiresApproval bool   `json:"requires_approval"`
	}
	for _, item := range payload.Data {
		if item.Level == "A3" {
			a3Actions = item.AllowedActionClasses
		}
	}
	if len(a3Actions) != 4 {
		t.Fatalf("expected A3 to allow 4 action classes, got %+v", a3Actions)
	}
	if a3Actions[len(a3Actions)-1].Class != "write_low" || a3Actions[len(a3Actions)-1].RequiresApproval {
		t.Fatalf("unexpected A3 last action: %+v", a3Actions[len(a3Actions)-1])
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
}

func (f *handlerFakeUseCases) Create(_ context.Context, input domain.CreateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: uuid.New(), Name: normalized.Name, Role: normalized.Role, SupervisorUserID: normalized.SupervisorUserID, Autonomy: normalized.Autonomy}, nil
}

func (f *handlerFakeUseCases) ListActive(context.Context) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) ListArchived(context.Context) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) ListTrash(context.Context) ([]domain.Virployee, error) {
	return []domain.Virployee{}, nil
}

func (f *handlerFakeUseCases) Get(_ context.Context, id uuid.UUID) (domain.Virployee, error) {
	return domain.Virployee{ID: id, Name: "Ops", Role: "ops", SupervisorUserID: uuid.New(), Autonomy: domain.AutonomyA1}, nil
}

func (f *handlerFakeUseCases) Update(_ context.Context, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: id, Name: normalized.Name, Role: normalized.Role, SupervisorUserID: normalized.SupervisorUserID, Autonomy: normalized.Autonomy}, nil
}

func (f *handlerFakeUseCases) Archive(_ context.Context, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "archive"
	f.lastActor = actor
	return nil
}

func (f *handlerFakeUseCases) Unarchive(_ context.Context, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "unarchive"
	f.lastActor = actor
	return nil
}

func (f *handlerFakeUseCases) Trash(_ context.Context, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "trash"
	f.lastActor = actor
	return nil
}

func (f *handlerFakeUseCases) Restore(_ context.Context, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "restore"
	f.lastActor = actor
	return nil
}

func (f *handlerFakeUseCases) Purge(_ context.Context, _ uuid.UUID, actor, _ string) error {
	f.lastAction = "purge"
	f.lastActor = actor
	return nil
}
