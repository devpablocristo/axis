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

func (f *fakeOperationalRepository) FailStaleAssistRuns(context.Context, time.Time, int) ([]TimedOutAssist, error) {
	return f.assists, nil
}
func (f *fakeOperationalRepository) ClaimStaleExecutions(context.Context, time.Time, int, string, time.Duration, int) ([]ExecutionWork, error) {
	return nil, nil
}
func (f *fakeOperationalRepository) ClaimDueNexusReports(context.Context, time.Time, int, string, time.Duration, int) ([]ExecutionWork, error) {
	return nil, nil
}
func (f *fakeOperationalRepository) ReleaseExecutionRecovery(context.Context, string, uuid.UUID, string) error {
	return nil
}
func (f *fakeOperationalRepository) RecordNexusReportAttempt(context.Context, string, uuid.UUID, string, time.Time, string) error {
	return nil
}

func TestOperationalWatcherFinalizesStaleAssistAndAudits(t *testing.T) {
	runID, virployeeID := uuid.New(), uuid.New()
	repo := &fakeOperationalRepository{fakeExecRepo: &fakeExecRepo{}, assists: []TimedOutAssist{{
		ID: runID, TenantID: "tenant-1", VirployeeID: virployeeID, InputHash: "sha256:input",
	}}}
	emitter := &fakeAuditEmitter{}
	uc := &UseCases{executionRepo: repo, auditEmitter: emitter, executors: map[string]ActionExecutorPort{}}
	err := uc.RunOperationalWatchersOnce(context.Background(), WatcherConfig{
		StaleAssistAfter: time.Minute, StaleExecutionAfter: time.Minute, Lease: time.Second,
		ReportBackoff: time.Second, BatchSize: 10, MaxRecoveryAttempts: 3, MaxReportAttempts: 3,
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

func TestExponentialBackoffIsBoundedAndDoubles(t *testing.T) {
	if got := exponentialBackoff(5*time.Second, 2); got != 20*time.Second {
		t.Fatalf("expected 20s, got %s", got)
	}
	if got := exponentialBackoff(time.Second, 100); got != 1024*time.Second {
		t.Fatalf("expected capped exponent, got %s", got)
	}
}
