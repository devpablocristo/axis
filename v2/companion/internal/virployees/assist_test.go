package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
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
	current       AssistRun
}

func (r *fakeAssistRepo) BeginAssistRun(_ context.Context, tenant string, vid uuid.UUID, _, idem, inputHash, _ string, input json.RawMessage) (AssistRun, bool, error) {
	r.beginCalls++
	if !r.reserved {
		return r.existing, false, nil
	}
	r.current = AssistRun{ID: uuid.New(), TenantID: tenant, VirployeeID: vid, IdempotencyKey: idem, Status: "received", InputHash: inputHash, InputJSON: input}
	return r.current, true, nil
}

func (r *fakeAssistRepo) ClaimAssistRun(_ context.Context, _ string, id uuid.UUID) (AssistRun, bool, error) {
	if r.current.ID == id {
		r.current.Status = "answering"
		return r.current, true, nil
	}
	return r.existing, false, nil
}

func (r *fakeAssistRepo) CompleteAssistRun(_ context.Context, _ string, id uuid.UUID, status string, output json.RawMessage, outputText string, answered, degraded bool, model, pv, runErr string, dur int64) (AssistRun, error) {
	r.completeCalls++
	r.lastComplete = AssistRun{ID: id, Status: status, Output: output, OutputText: outputText, Answered: answered, Degraded: degraded, Model: model, PromptVersion: pv, Error: runErr, DurationMS: dur}
	r.lastComplete.TenantID = r.current.TenantID
	r.lastComplete.VirployeeID = r.current.VirployeeID
	return r.lastComplete, nil
}

func (r *fakeAssistRepo) GetAssistRunByKey(context.Context, string, uuid.UUID, string) (AssistRun, error) {
	return r.existing, nil
}

func (r *fakeAssistRepo) GetAssistRunByID(context.Context, string, uuid.UUID) (AssistRun, error) {
	if r.current.ID != uuid.Nil {
		return r.current, nil
	}
	return r.existing, nil
}

func (r *fakeAssistRepo) ListReceivedAssistRuns(context.Context, int) ([]AssistRun, error) {
	if r.current.Status == "received" {
		return []AssistRun{r.current}, nil
	}
	return nil, nil
}

type fakeAnswerer struct {
	called    bool
	lastInput AnswerInput
	out       AnswerOutput
	err       error
}

func (a *fakeAnswerer) Answer(_ context.Context, in AnswerInput) (AnswerOutput, error) {
	a.called = true
	a.lastInput = in
	return a.out, a.err
}

type fakeDocFetcher struct {
	calls int
	docs  map[string]FetchedDocument
}

type fakeAssistQueue struct {
	runs []AssistRun
	err  error
}

func (q *fakeAssistQueue) EnqueueAssist(_ context.Context, run AssistRun) error {
	q.runs = append(q.runs, run)
	return q.err
}

func (f *fakeDocFetcher) Fetch(_ context.Context, key, _, _ string) FetchedDocument {
	f.calls++
	if d, ok := f.docs[key]; ok {
		return d
	}
	return FetchedDocument{Key: key, Note: "not stubbed"}
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

func TestAssistFetchesDocumentsAndPassesContentToModel(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	ans := &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true}}
	fetcher := &fakeDocFetcher{docs: map[string]FetchedDocument{
		"labs.txt": {Key: "labs.txt", ContentType: "text/plain", Content: "Glucosa 126 mg/dL", Readable: true},
	}}
	uc.assistRepo = repo
	uc.answerer = ans
	uc.docFetcher = fetcher

	input := json.RawMessage(`{"schema_version":"medmory.diagnosis_input.v1","documents":[{"key":"labs.txt","read_url":"https://x/labs","content_type":"text/plain"}]}`)
	if _, err := uc.Assist(context.Background(), "tenant-1", created.ID, input, "idem-docs"); err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected the document to be fetched once, got %d", fetcher.calls)
	}
	// The model must receive the fetched CONTENT, not the raw read_url reference.
	got := string(ans.lastInput.InputJSON)
	if !strings.Contains(got, "Glucosa 126 mg/dL") {
		t.Fatalf("model input must contain the fetched content, got %s", got)
	}
	if strings.Contains(got, "read_url") || strings.Contains(got, "https://x/labs") {
		t.Fatalf("model input must not leak the presigned read_url, got %s", got)
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
	input := json.RawMessage(`{"x":1}`)
	existing := AssistRun{ID: uuid.New(), Status: "done", Answered: true, InputHash: runtraces.HashString(string(input)), Output: json.RawMessage(`{"summary":"prev"}`)}
	repo := &fakeAssistRepo{reserved: false, existing: existing}
	ans := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo = repo
	uc.answerer = ans

	run, err := uc.Assist(context.Background(), "tenant-1", created.ID, input, "idem-dup")
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

func TestSubmitAssistAsyncPersistsAndQueuesIdentifiersWithoutCallingModel(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	queue := &fakeAssistQueue{}
	answerer := &fakeAnswerer{out: AnswerOutput{Answered: true}}
	uc.assistRepo = repo
	uc.assistQueue = queue
	uc.answerer = answerer

	run, err := uc.SubmitAssistAsync(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"document_id":"doc-1"}`), "manifest-generation-1")
	if err != nil {
		t.Fatalf("SubmitAssistAsync: %v", err)
	}
	if run.Status != "received" || len(queue.runs) != 1 || queue.runs[0].ID != run.ID {
		t.Fatalf("expected one received run queued, run=%+v queue=%+v", run, queue.runs)
	}
	if answerer.called {
		t.Fatal("request path must not invoke the model")
	}
}

func TestRequeueReceivedAssistRunsClosesPersistEnqueueGap(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	queue := &fakeAssistQueue{}
	uc.assistRepo = repo
	uc.assistQueue = queue
	uc.answerer = &fakeAnswerer{}

	run, _, err := uc.SubmitAssist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":1}`), "generation-a")
	if err != nil {
		t.Fatalf("SubmitAssist: %v", err)
	}
	count, err := uc.RequeueReceivedAssistRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("RequeueReceivedAssistRuns: %v", err)
	}
	if count != 1 || len(queue.runs) != 1 || queue.runs[0].ID != run.ID {
		t.Fatalf("expected persisted run to be requeued: count=%d runs=%+v", count, queue.runs)
	}
}

func TestSubmitAssistRejectsIdempotencyKeyReusedWithDifferentInput(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: false, existing: AssistRun{ID: uuid.New(), Status: "done", InputHash: runtraces.HashString(`{"x":1}`)}}
	uc.answerer = &fakeAnswerer{}

	_, _, err := uc.SubmitAssist(context.Background(), "tenant-1", created.ID, json.RawMessage(`{"x":2}`), "generation-a")
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected idempotency conflict, got %v", err)
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
