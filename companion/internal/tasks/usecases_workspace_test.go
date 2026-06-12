package tasks

import (
	"context"
	"encoding/json"
	"testing"
)

// capturingOrchestrator captura el OrchestratorInput del último Run.
type capturingOrchestrator struct {
	lastInput OrchestratorInput
}

func (c *capturingOrchestrator) Run(_ context.Context, in OrchestratorInput) (OrchestratorResult, error) {
	c.lastInput = in
	return OrchestratorResult{Reply: "ok"}, nil
}

func TestUsecases_ChatPropagatesTopLevelWorkspaceOverHandoff(t *testing.T) {
	t.Parallel()
	orch := &capturingOrchestrator{}
	uc := NewUsecases(&fakeRepo{}, &stubNexus{})
	uc.SetOrchestrator(orch)

	workspace := json.RawMessage(`{"customer_id":99,"project_id":7}`)
	handoff := json.RawMessage(`{"source":"ponti-web","workspace":{"customer_id":17}}`)
	result, err := uc.Chat(context.Background(), ChatInput{
		UserID:    "user-1",
		OrgID:     "org-1",
		Message:   "Hola",
		Workspace: workspace,
		Handoff:   handoff,
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if string(orch.lastInput.Workspace) != string(workspace) {
		t.Fatalf("expected top-level workspace propagated, got %s", string(orch.lastInput.Workspace))
	}
	if string(orch.lastInput.Handoff) != string(handoff) {
		t.Fatalf("expected handoff propagated untouched, got %s", string(orch.lastInput.Handoff))
	}

	// El workspace queda persistido en task.context_json bajo "workspace".
	var contextJSON map[string]any
	if err := json.Unmarshal(result.Task.ContextJSON, &contextJSON); err != nil {
		t.Fatal(err)
	}
	stored, ok := contextJSON[workspaceContextKey].(map[string]any)
	if !ok || stored["customer_id"] != float64(99) || stored["project_id"] != float64(7) {
		t.Fatalf("expected workspace persisted in task context, got %s", string(result.Task.ContextJSON))
	}
}

func TestUsecases_ChatReusesPersistedWorkspaceWhenNotResent(t *testing.T) {
	t.Parallel()
	orch := &capturingOrchestrator{}
	repo := &fakeRepo{}
	uc := NewUsecases(repo, &stubNexus{})
	uc.SetOrchestrator(orch)

	first, err := uc.Chat(context.Background(), ChatInput{
		UserID:    "user-1",
		OrgID:     "org-1",
		Message:   "Hola",
		Workspace: json.RawMessage(`{"customer_id":99,"project_id":7}`),
	})
	if err != nil {
		t.Fatalf("first chat failed: %v", err)
	}

	// Segundo turno sin workspace: se reusa el persistido en la task.
	taskID := first.Task.ID
	if _, err := uc.Chat(context.Background(), ChatInput{
		TaskID:  &taskID,
		UserID:  "user-1",
		OrgID:   "org-1",
		Message: "Seguimos",
	}); err != nil {
		t.Fatalf("second chat failed: %v", err)
	}
	var reused map[string]any
	if err := json.Unmarshal(orch.lastInput.Workspace, &reused); err != nil {
		t.Fatalf("expected reused workspace, got %q: %v", string(orch.lastInput.Workspace), err)
	}
	if reused["customer_id"] != float64(99) || reused["project_id"] != float64(7) {
		t.Fatalf("expected persisted workspace reused, got %+v", reused)
	}

	// Tercer turno con workspace nuevo: gana y reemplaza el persistido.
	if _, err := uc.Chat(context.Background(), ChatInput{
		TaskID:    &taskID,
		UserID:    "user-1",
		OrgID:     "org-1",
		Message:   "Cambio de proyecto",
		Workspace: json.RawMessage(`{"customer_id":99,"project_id":8}`),
	}); err != nil {
		t.Fatalf("third chat failed: %v", err)
	}
	if string(orch.lastInput.Workspace) != `{"customer_id":99,"project_id":8}` {
		t.Fatalf("expected resent workspace to win, got %s", string(orch.lastInput.Workspace))
	}
	updated, err := repo.GetTaskByID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	var contextJSON map[string]any
	if err := json.Unmarshal(updated.ContextJSON, &contextJSON); err != nil {
		t.Fatal(err)
	}
	stored, _ := contextJSON[workspaceContextKey].(map[string]any)
	if stored["project_id"] != float64(8) {
		t.Fatalf("expected persisted workspace updated, got %s", string(updated.ContextJSON))
	}
}
