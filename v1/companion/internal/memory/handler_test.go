package memory

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeTaskOrgs struct {
	orgs map[uuid.UUID]string
}

func (f *fakeTaskOrgs) GetTaskOrg(_ context.Context, id uuid.UUID) (string, error) {
	org, ok := f.orgs[id]
	if !ok {
		return "", errors.New("not found")
	}
	return org, nil
}

func TestHandlerRejectsForeignOrgMemoryScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}), nil).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"user_preference",
		"scope_type":"org",
		"scope_id":"org-b",
		"key":"timezone",
		"content_text":"UTC"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAllowsOwnOrgMemoryScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}), nil).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"user_preference",
		"scope_type":"org",
		"scope_id":"org-a",
		"key":"timezone",
		"content_text":"UTC"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsForeignOrgTaskScope(t *testing.T) {
	t.Parallel()

	taskID := uuid.New()
	taskOrgs := &fakeTaskOrgs{orgs: map[uuid.UUID]string{taskID: "org-b"}}

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}), taskOrgs).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"task_note",
		"scope_type":"task",
		"scope_id":"`+taskID.String()+`",
		"key":"summary",
		"content_text":"x"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAllowsOwnOrgTaskScope(t *testing.T) {
	t.Parallel()

	taskID := uuid.New()
	taskOrgs := &fakeTaskOrgs{orgs: map[uuid.UUID]string{taskID: "org-a"}}

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}), taskOrgs).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"task_note",
		"scope_type":"task",
		"scope_id":"`+taskID.String()+`",
		"key":"summary",
		"content_text":"x"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsTaskScopeWithoutGetter(t *testing.T) {
	t.Parallel()

	taskID := uuid.New()
	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{}), nil).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"task_note",
		"scope_type":"task",
		"scope_id":"`+taskID.String()+`",
		"key":"summary",
		"content_text":"x"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (fail-closed), got %d: %s", rec.Code, rec.Body.String())
	}
}
