package virployees

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeOperationalRepository struct {
	*fakeExecRepo
	assists []TimedOutAssist
}

func (f *fakeOperationalRepository) ReconcileStaleAssistRuns(context.Context, time.Time, int, int) ([]TimedOutAssist, error) {
	return f.assists, nil
}
func (f *fakeOperationalRepository) ClaimStaleExecutions(context.Context, time.Time, int, string, time.Duration, int) ([]ExecutionWork, error) {
	return nil, nil
}
func (f *fakeOperationalRepository) ReleaseExecutionRecovery(context.Context, string, uuid.UUID, string) error {
	return nil
}

func TestOperationalWatcherFinalizesStaleAssistAndAudits(t *testing.T) {
	runID, virployeeID := uuid.New(), uuid.New()
	repo := &fakeOperationalRepository{fakeExecRepo: &fakeExecRepo{}, assists: []TimedOutAssist{{
		ID: runID, OrgID: "organization-1", VirployeeID: virployeeID, InputHash: "sha256:input", Outcome: "timed_out",
	}}}
	emitter := &fakeAuditEmitter{}
	uc := &UseCases{executionRepo: repo, auditEmitter: emitter, executors: map[string]ActionExecutorPort{}}
	err := uc.RunOperationalWatchersOnce(context.Background(), WatcherConfig{
		StaleAssistAfter: time.Minute, StaleExecutionAfter: time.Minute, Lease: time.Second,
		BatchSize: 10, MaxRecoveryAttempts: 3,
	})
	if err != nil {
		t.Fatalf("RunOperationalWatchersOnce: %v", err)
	}
	if len(emitter.events) != 1 || emitter.events[0].EventType != "assist_timed_out" {
		t.Fatalf("expected assist_timed_out audit event, got %+v", emitter.events)
	}
	if emitter.events[0].Data["input_hash"] != "sha256:input" {
		t.Fatalf("expected hash-only evidence, got %+v", emitter.events[0].Data)
	}
}

func TestOperationalWatcherRecoversPreAnswerAssistAndAudits(t *testing.T) {
	runID, virployeeID := uuid.New(), uuid.New()
	repo := &fakeOperationalRepository{fakeExecRepo: &fakeExecRepo{}, assists: []TimedOutAssist{{
		ID: runID, OrgID: "organization-1", VirployeeID: virployeeID, InputHash: "sha256:input", Outcome: "recovered",
	}}}
	emitter := &fakeAuditEmitter{}
	uc := &UseCases{executionRepo: repo, auditEmitter: emitter, executors: map[string]ActionExecutorPort{}}
	err := uc.RunOperationalWatchersOnce(context.Background(), WatcherConfig{
		StaleAssistAfter: time.Minute, StaleExecutionAfter: time.Minute, Lease: time.Second,
		BatchSize: 10, MaxRecoveryAttempts: 3,
	})
	if err != nil {
		t.Fatalf("RunOperationalWatchersOnce: %v", err)
	}
	if len(emitter.events) != 1 || emitter.events[0].EventType != "assist_recovered" {
		t.Fatalf("expected assist_recovered audit event, got %+v", emitter.events)
	}
}
