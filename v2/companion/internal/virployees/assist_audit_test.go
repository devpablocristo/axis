package virployees

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type fakeAuditEmitter struct {
	events []AuditEventInput
	err    error
}

func (f *fakeAuditEmitter) AppendAuditEvent(_ context.Context, in AuditEventInput) error {
	f.events = append(f.events, in)
	return f.err
}

func TestAssistEmitsAuditEventOnSuccess(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	repo := &fakeAssistRepo{reserved: true}
	ans := &fakeAnswerer{out: AnswerOutput{
		OutputJSON:    json.RawMessage(`{"summary":"anemia ferropénica","patient_note":"PHI-SECRET"}`),
		Answered:      true,
		ModelID:       "gemini",
		PromptVersion: "answer.v1",
	}}
	emitter := &fakeAuditEmitter{}
	uc.assistRepo = repo
	uc.answerer = ans
	uc.auditEmitter = emitter

	run, err := uc.Assist(context.Background(), "organization-1", created.ID, json.RawMessage(`{"documents":[]}`), "idem-1", AssistMetadata{})
	if err != nil {
		t.Fatalf("Assist: %v", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("expected exactly one audit event, got %d", len(emitter.events))
	}
	ev := emitter.events[0]
	if ev.EventType != "assist_completed" {
		t.Fatalf("expected assist_completed, got %q", ev.EventType)
	}
	if ev.SubjectType != "assist_run" || ev.SubjectID != run.ID.String() {
		t.Fatalf("expected subject assist_run/%s, got %s/%s", run.ID, ev.SubjectType, ev.SubjectID)
	}
	if ev.VirployeeID != created.ID.String() || ev.ActorID != created.ID.String() || ev.ActorType != "virployee" {
		t.Fatalf("expected the virployee as subject+actor, got %+v", ev)
	}
	if _, ok := ev.Data["output_hash"]; !ok {
		t.Fatal("expected output_hash binding the event to the exact answer")
	}
	// The ledger must carry hashes + metadata only — never the raw diagnosis/PHI.
	raw, _ := json.Marshal(ev.Data)
	if strings.Contains(string(raw), "PHI-SECRET") || strings.Contains(string(raw), "anemia") {
		t.Fatalf("audit data must not contain raw output/PHI, got %s", raw)
	}
}

func TestAssistEmitsAuditEventOnFailure(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: true}
	uc.answerer = &fakeAnswerer{err: errors.New("runtime down")}
	emitter := &fakeAuditEmitter{}
	uc.auditEmitter = emitter

	if _, err := uc.Assist(context.Background(), "organization-1", created.ID, json.RawMessage(`{"x":1}`), "idem-f", AssistMetadata{}); err == nil {
		t.Fatal("expected an error when the runtime fails")
	}
	if len(emitter.events) != 1 || emitter.events[0].EventType != "assist_failed" {
		t.Fatalf("expected one assist_failed audit event, got %+v", emitter.events)
	}
}

func TestAssistSucceedsWhenAuditEmitFails(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.assistRepo = &fakeAssistRepo{reserved: true}
	uc.answerer = &fakeAnswerer{out: AnswerOutput{OutputJSON: json.RawMessage(`{"summary":"ok"}`), Answered: true}}
	uc.auditEmitter = &fakeAuditEmitter{err: errors.New("nexus down")}

	run, err := uc.Assist(context.Background(), "organization-1", created.ID, json.RawMessage(`{"x":1}`), "idem-e", AssistMetadata{})
	if err != nil {
		t.Fatalf("assist must succeed despite a best-effort audit failure: %v", err)
	}
	if run.Status != "done" {
		t.Fatalf("expected a completed run, got %+v", run)
	}
}
