package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type fakeAssistRepo struct {
	reserved      bool
	existing      AssistRun
	beginCalls    int
	completeCalls int
	lastComplete  AssistRun
}

func (r *fakeAssistRepo) BeginAssistRun(_ context.Context, _ string, vid uuid.UUID, _, idem, _, _ string) (AssistRun, bool, error) {
	r.beginCalls++
	if !r.reserved {
		return r.existing, false, nil
	}
	return AssistRun{ID: uuid.New(), VirployeeID: vid, IdempotencyKey: idem, Status: "running"}, true, nil
}

func (r *fakeAssistRepo) CompleteAssistRun(_ context.Context, _ string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, pv, runErr string, dur int64) (AssistRun, error) {
	r.completeCalls++
	r.lastComplete = AssistRun{ID: id, Status: status, Output: output, OutputText: outputText, Answered: answered, Degraded: degraded, Model: model, PromptVersion: pv, Error: runErr, DurationMS: dur}
	return r.lastComplete, nil
}

func (r *fakeAssistRepo) GetAssistRunByKey(context.Context, string, uuid.UUID, string) (AssistRun, error) {
	return r.existing, nil
}

type fakeAnswerer struct {
	called bool
	out    AnswerOutput
	err    error
}

func (a *fakeAnswerer) Answer(context.Context, AnswerInput) (AnswerOutput, error) {
	a.called = true
	return a.out, a.err
}

func TestAssistProcessesAndPersistsAnswer(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	ans := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true, ModelID: "gemini", PromptVersion: "answer.v1"}}
	uc.assistRepo = repo
	uc.answerer = ans

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"documents":[]}`), "idem-1")
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if !ans.called {
		t.Fatal("expected the answerer to be called")
	}
	if run.Status != "done" || !run.Answered || run.Degraded {
		t.Fatalf("expected a completed answered run, got %+v", run)
	}
	if string(run.Output) != `{"summary":"ok"}` || repo.completeCalls != 1 {
		t.Fatalf("expected the model output persisted once, got %+v (completes=%d)", run, repo.completeCalls)
	}
}

func TestAssistMarksDegradedWhenModelDidNotAnswer(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: true}
	uc.answerer = &fakeAnswerer{out: AnswerOutput{OutputText: "Recibido (modo echo).", Answered: false}}

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "idem-2")
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if run.Status != "done" || run.Answered || !run.Degraded {
		t.Fatalf("Echo/no-answer must yield a degraded run, got %+v", run)
	}
}

func TestAssistReplayReturnsExistingRunWithoutCallingModel(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	existing := AssistRun{ID: uuid.New(), Status: "done", Answered: true, Output: json.RawMessage(`{"summary":"prev"}`)}
	repo := &fakeAssistRepo{reserved: false, existing: existing}
	ans := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo = repo
	uc.answerer = ans

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "idem-dup")
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if ans.called {
		t.Fatal("a completed run must NOT re-invoke the model")
	}
	if run.ID != existing.ID || string(run.Output) != `{"summary":"prev"}` {
		t.Fatalf("replay must return the stored run, got %+v", run)
	}
}

func TestAssistFailsClosedOnRuntimeError(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	uc.assistRepo = repo
	uc.answerer = &fakeAnswerer{err: errors.New("runtime down")}

	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "idem-3")
	if err == nil {
		t.Fatal("expected an error when the runtime fails")
	}
	if repo.lastComplete.Status != "failed" || repo.lastComplete.Error == "" {
		t.Fatalf("a runtime failure must be recorded as failed with an error, got %+v", repo.lastComplete)
	}
}

func TestAssistRequiresAnswerer(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: true}
	// answerer intentionally nil
	_, err := uc.Assist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "k")
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict when the answerer is not configured, got %v", err)
	}
}
