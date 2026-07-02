package virployees

import (
	"context"
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
	return domain.Virployee{ID: uuid.New(), Name: normalized.Name, Role: normalized.Role, SupervisorUserID: normalized.SupervisorUserID}, nil
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
	return domain.Virployee{ID: id, Name: "Ops", Role: "ops", SupervisorUserID: uuid.New()}, nil
}

func (f *handlerFakeUseCases) Update(_ context.Context, id uuid.UUID, input domain.UpdateInput) (domain.Virployee, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.Virployee{}, err
	}
	return domain.Virployee{ID: id, Name: normalized.Name, Role: normalized.Role, SupervisorUserID: normalized.SupervisorUserID}, nil
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
