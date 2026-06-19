package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devpablocristo/companion/internal/identityctx"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
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

func TestChatUsesRequestedOrgWithCrossOrgScope(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	principal := &authn.Principal{
		OrgID:      "org-a",
		Actor:      "axis-admin",
		Scopes:     []string{scopeCompanionTasksWrite, scopeCompanionCrossOrg},
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_id":          "axis-admin",
			"actor_type":        "service",
			"service_principal": true,
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat?org_id=org-b", strings.NewReader(`{"message":"hola","product_surface":"medmory"}`))
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	req = identityctx.WithPrincipal(req, principal)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.OrgID != "org-b" {
		t.Fatalf("expected requested org-b, got %q", task.OrgID)
	}
}

func TestChatAcceptsChatIDAndReturnsTaskID(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	taskID := uuid.New()
	repo := &fakeRepo{tasks: map[uuid.UUID]domain.Task{
		taskID: {
			ID:          taskID,
			OrgID:       "org-a",
			Title:       "existing conversation",
			Status:      domain.TaskStatusNew,
			CreatedBy:   "user-a",
			Channel:     "api",
			ContextJSON: json.RawMessage(`{"agent_conversation_id":"` + chatID.String() + `"}`),
		},
	}}
	mux := http.NewServeMux()
	NewHandler(NewUsecases(repo, &stubNexus{})).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/chat", `{"chat_id":"`+chatID.String()+`","message":"seguimos"}`, &authn.Principal{
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
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ChatID string `json:"chat_id"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.ChatID != chatID.String() {
		t.Fatalf("expected chat_id %s, got %q", chatID, out.ChatID)
	}
	if out.TaskID != taskID.String() {
		t.Fatalf("expected task_id %s, got %q", taskID, out.TaskID)
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

func TestCustomerMessagingInboundUsesPublicContractAndIdentity(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	chatID := uuid.New()
	uc := NewUsecases(repo, &stubNexus{})
	uc.SetAgentMemory(stubAgentMemory{conversationID: chatID})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/customer-messaging/inbound", `{
		"org_id":"org-a",
		"phone_number_id":"phone-1",
		"from_phone":"5491112345678",
		"message":"Hola",
		"message_id":"wamid-1",
		"profile_name":"Juan"
	}`, &authn.Principal{
		OrgID:      "org-a",
		Actor:      "pymes-whatsapp-bridge",
		Scopes:     []string{scopeCompanionTasksWrite},
		AuthMethod: "internal_jwt",
		Claims: map[string]any{
			"actor_id":          "pymes-whatsapp-bridge",
			"actor_type":        "service",
			"service_principal": true,
			"product_surface":   "pymes",
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	task := onlyTask(t, repo)
	if task.OrgID != "org-a" || task.CreatedBy != "whatsapp:5491112345678:Juan" {
		t.Fatalf("unexpected task identity: %+v", task)
	}
	var out customerMessagingInboundResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.ConversationID != chatID.String() {
		t.Fatalf("expected conversation id %s, got %+v", chatID, out)
	}
}

type stubAgentMemory struct {
	conversationID uuid.UUID
}

func (s stubAgentMemory) StartConversation(ctx context.Context, orgID, userID, productSurface, title string) (uuid.UUID, error) {
	return s.conversationID, nil
}

func (s stubAgentMemory) AppendMessage(ctx context.Context, conversationID uuid.UUID, orgID, role, content string) error {
	return nil
}

func TestCustomerMessagingInboundRejectsInternalRouteAndInvalidAuth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}, &stubNexus{})).Register(mux)

	internal := httptest.NewRecorder()
	mux.ServeHTTP(internal, httptest.NewRequest(http.MethodPost, "/v1/internal/customer-messaging/inbound", strings.NewReader(`{}`)))
	if internal.Code != http.StatusNotFound {
		t.Fatalf("expected old internal route 404, got %d: %s", internal.Code, internal.Body.String())
	}

	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/v1/customer-messaging/inbound", strings.NewReader(`{}`)))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated request 401, got %d: %s", unauth.Code, unauth.Body.String())
	}

	wrongSurface := authenticatedTaskRequest(http.MethodPost, "/v1/customer-messaging/inbound", `{
		"org_id":"org-a",
		"phone_number_id":"phone-1",
		"from_phone":"5491112345678",
		"message":"Hola"
	}`, &authn.Principal{
		OrgID:  "org-a",
		Actor:  "other-service",
		Scopes: []string{scopeCompanionTasksWrite},
		Claims: map[string]any{
			"actor_id":        "other-service",
			"actor_type":      "service",
			"product_surface": "other",
		},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, wrongSurface)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected wrong product surface 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCustomerMessagingInboundValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}, &stubNexus{})).Register(mux)

	req := authenticatedTaskRequest(http.MethodPost, "/v1/customer-messaging/inbound", `{
		"org_id":"org-a",
		"phone_number_id":"phone-1",
		"message":"Hola"
	}`, &authn.Principal{
		OrgID:  "org-a",
		Actor:  "pymes-whatsapp-bridge",
		Scopes: []string{scopeCompanionTasksWrite},
		Claims: map[string]any{
			"actor_id":        "pymes-whatsapp-bridge",
			"actor_type":      "service",
			"product_surface": "pymes",
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing from_phone 400, got %d: %s", rec.Code, rec.Body.String())
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
