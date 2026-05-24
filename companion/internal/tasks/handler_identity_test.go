package tasks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devpablocristo/companion/internal/identityctx"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

func TestChatUsesCanonicalPrincipalIdentity(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	principal := &authn.Principal{
		OrgID:      "org-a",
		Actor:      "user-a",
		Scopes:     []string{scopeCompanionTasksWrite},
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_id":          "user-a",
			"actor_type":        "human",
			"service_principal": true,
			"on_behalf_of":      "user-a",
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{"message":"hola"}`))
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = identityctx.WithPrincipal(req, principal)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.OrgID != "org-a" {
		t.Fatalf("expected org-a, got %q", task.OrgID)
	}
	if task.CreatedBy != "user-a" {
		t.Fatalf("expected created_by from principal, got %q", task.CreatedBy)
	}
	if task.CreatedBy == "subscriber" {
		t.Fatal("chat must not use subscriber fallback with authenticated identity")
	}
}

func TestChatFallsBackToCompanionPrincipalWithoutHumanUser(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	principal := &authn.Principal{
		OrgID:      "org-a",
		Actor:      "axis-bff",
		Scopes:     []string{scopeCompanionTasksWrite},
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_id":          "axis-bff",
			"actor_type":        "service",
			"service_principal": true,
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{"message":"hola"}`))
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = identityctx.WithPrincipal(req, principal)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.CreatedBy != identityctx.CompanionPrincipal {
		t.Fatalf("expected companion principal fallback, got %q", task.CreatedBy)
	}
}

func TestChatRequiresCustomerOrg(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	principal := &authn.Principal{
		Actor:  "user-a",
		Scopes: []string{scopeCompanionTasksWrite},
		Claims: map[string]any{
			"actor_id":   "user-a",
			"actor_type": "human",
		},
	}
	req := authenticatedTaskRequest(http.MethodPost, "/v1/chat", `{"message":"hola"}`, principal)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.tasks) != 0 {
		t.Fatalf("expected no task, got %d", len(repo.tasks))
	}
}

func TestCreateTaskDefaultsCreatedByFromIdentity(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	mux := http.NewServeMux()
	NewHandler(NewUsecases(repo, &stubNexus{})).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/tasks", `{"title":"crear"}`, &authn.Principal{
		OrgID:  "org-a",
		Actor:  "user-a",
		Scopes: []string{scopeCompanionTasksWrite},
		Claims: map[string]any{
			"actor_id":   "user-a",
			"actor_type": "human",
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.OrgID != "org-a" || task.CreatedBy != "user-a" {
		t.Fatalf("unexpected task identity: %+v", task)
	}
}

func TestCreateTaskRejectsUnvalidatedCreatedBy(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	mux := http.NewServeMux()
	NewHandler(NewUsecases(repo, &stubNexus{})).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/tasks", `{"title":"crear","created_by":"user-b"}`, &authn.Principal{
		OrgID:  "org-a",
		Actor:  "user-a",
		Scopes: []string{scopeCompanionTasksWrite},
		Claims: map[string]any{
			"actor_id":   "user-a",
			"actor_type": "human",
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.tasks) != 0 {
		t.Fatalf("expected no task to be created, got %d", len(repo.tasks))
	}
}

func TestCreateTaskAllowsCreatedByOnBehalfOf(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	mux := http.NewServeMux()
	NewHandler(NewUsecases(repo, &stubNexus{})).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/tasks", `{"title":"crear","created_by":"user-b"}`, &authn.Principal{
		OrgID:  "org-a",
		Actor:  "axis-bff",
		Scopes: []string{scopeCompanionTasksWrite},
		Claims: map[string]any{
			"actor_id":          "axis-bff",
			"actor_type":        "service",
			"service_principal": true,
			"on_behalf_of":      "user-b",
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.CreatedBy != "user-b" {
		t.Fatalf("expected delegated created_by, got %q", task.CreatedBy)
	}
}

func TestCreateTaskAllowsCreatedByWithOperatorScope(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	mux := http.NewServeMux()
	NewHandler(NewUsecases(repo, &stubNexus{})).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/tasks", `{"title":"crear","created_by":"user-b"}`, &authn.Principal{
		OrgID:  "org-a",
		Actor:  "operator-a",
		Scopes: []string{scopeCompanionTasksWrite, scopeCompanionCrossOrg},
		Claims: map[string]any{
			"actor_id":   "operator-a",
			"actor_type": "human",
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.CreatedBy != "user-b" {
		t.Fatalf("expected operator-created actor, got %q", task.CreatedBy)
	}
}

func authenticatedTaskRequest(method, target, body string, principal *authn.Principal) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}

func onlyTask(t *testing.T, repo *fakeRepo) domainTaskView {
	t.Helper()
	if len(repo.tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(repo.tasks))
	}
	for _, task := range repo.tasks {
		if task.ID == uuid.Nil {
			t.Fatal("task id was not assigned")
		}
		return domainTaskView{OrgID: task.OrgID, CreatedBy: task.CreatedBy}
	}
	t.Fatal("unreachable")
	return domainTaskView{}
}

type domainTaskView struct {
	OrgID     string
	CreatedBy string
}
