package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestMemoryRepositoryDedupeIsOrgAndProductScoped(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	first, inserted, err := repository.Enqueue(context.Background(), EnqueueInput{
		OrgID: "organization-a", ProductSurface: "producta", Kind: "artifact.extract",
		DedupeKey: "document-1", Payload: json.RawMessage(`{"version":1}`),
	})
	if err != nil || !inserted {
		t.Fatalf("first enqueue inserted=%v err=%v", inserted, err)
	}
	replaced, inserted, err := repository.Enqueue(context.Background(), EnqueueInput{
		OrgID: "organization-a", ProductSurface: "producta", Kind: "artifact.extract",
		DedupeKey: "document-1", Payload: json.RawMessage(`{"version":2}`), ReplacePayload: true,
	})
	if err != nil || inserted || replaced.ID != first.ID || string(replaced.Payload) != `{"version":2}` {
		t.Fatalf("deduplicated enqueue=%+v inserted=%v err=%v", replaced, inserted, err)
	}
	for _, input := range []EnqueueInput{
		{OrgID: "organization-b", ProductSurface: "producta", Kind: "artifact.extract", DedupeKey: "document-1"},
		{OrgID: "organization-a", ProductSurface: "productb", Kind: "artifact.extract", DedupeKey: "document-1"},
		{OrgID: "organization-a", ProductSurface: "producta", Kind: "artifact.index", DedupeKey: "document-1"},
	} {
		if _, inserted, err := repository.Enqueue(context.Background(), input); err != nil || !inserted {
			t.Fatalf("scoped enqueue %+v inserted=%v err=%v", input, inserted, err)
		}
	}
}

func TestMemoryRepositoryExpiredLeaseRecoversThenDeadLetters(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	repository.SetClock(func() time.Time { return now })
	job, _, err := repository.Enqueue(context.Background(), EnqueueInput{
		OrgID: "organization-a", Kind: "demo", DedupeKey: "demo-1", MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(context.Background(), ClaimOptions{WorkerID: "worker-1", BatchSize: 1, LeaseDuration: time.Second})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("first claim=%+v err=%v", claimed, err)
	}
	now = now.Add(2 * time.Second)
	recovery, err := repository.RecoverExpiredLeases(context.Background(), 10)
	if err != nil || recovery.Requeued != 1 || recovery.DeadLetter != 0 {
		t.Fatalf("first recovery=%+v err=%v", recovery, err)
	}
	claimed, err = repository.Claim(context.Background(), ClaimOptions{WorkerID: "worker-2", BatchSize: 1, LeaseDuration: time.Second})
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 2 {
		t.Fatalf("second claim=%+v err=%v", claimed, err)
	}
	now = now.Add(2 * time.Second)
	recovery, err = repository.RecoverExpiredLeases(context.Background(), 10)
	if err != nil || recovery.Requeued != 0 || recovery.DeadLetter != 1 {
		t.Fatalf("second recovery=%+v err=%v", recovery, err)
	}
	stored, err := repository.Get(context.Background(), "organization-a", job.ID)
	if err != nil || stored.Status != StatusDeadLetter || stored.LastErrorCode != "lease_expired" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	replayed, err := repository.ReplayDeadLetter(context.Background(), "organization-a", job.ID, now)
	if err != nil || replayed.Status != StatusQueued || replayed.Attempts != 0 || replayed.CompletedAt != nil {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}

func TestWorkerRetriesThenSucceedsWithoutPersistingRawError(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	job, _, err := repository.Enqueue(context.Background(), EnqueueInput{
		OrgID: "organization-a", Kind: "demo", DedupeKey: "retry", MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	worker := NewWorker(repository, WorkerConfig{
		WorkerID: "worker-1", Concurrency: 1,
		Backoff: func(int) time.Duration { return time.Millisecond },
	})
	worker.Register("demo", func(context.Context, Job) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return nil, Retryable("provider_unavailable", errors.New("patient secret raw error"))
		}
		return json.RawMessage(`{"output_hash":"sha256:abc"}`), nil
	})
	if count, err := worker.RunOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("first run count=%d err=%v", count, err)
	}
	stored, err := repository.Get(context.Background(), "organization-a", job.ID)
	if err != nil || stored.Status != StatusQueued || stored.LastErrorCode != "provider_unavailable" {
		t.Fatalf("after retry=%+v err=%v", stored, err)
	}
	time.Sleep(2 * time.Millisecond)
	if count, err := worker.RunOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("second run count=%d err=%v", count, err)
	}
	stored, err = repository.Get(context.Background(), "organization-a", job.ID)
	if err != nil || stored.Status != StatusSucceeded || calls != 2 {
		t.Fatalf("after success=%+v calls=%d err=%v", stored, calls, err)
	}
}

func TestWorkerPermanentFailureAndReplay(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	job, _, err := repository.Enqueue(context.Background(), EnqueueInput{
		OrgID: "organization-a", Kind: "validate", DedupeKey: "invalid", MaxAttempts: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(repository, WorkerConfig{WorkerID: "worker-1", Concurrency: 1})
	worker.Register("validate", func(context.Context, Job) (json.RawMessage, error) {
		return nil, Permanent("unsupported_media_type", errors.New("raw filename and PHI"))
	})
	if count, err := worker.RunOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}
	stored, err := repository.Get(context.Background(), "organization-a", job.ID)
	if err != nil || stored.Status != StatusDeadLetter || stored.LastErrorCode != "unsupported_media_type" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestWorkerHeartbeatsLongRunningJob(t *testing.T) {
	repository := NewMemoryRepository()
	job, _, err := repository.Enqueue(context.Background(), EnqueueInput{
		OrgID: "organization-a", Kind: "slow", DedupeKey: "slow-1", Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(repository, WorkerConfig{
		WorkerID: "worker-1", Concurrency: 1, LeaseDuration: 300 * time.Millisecond,
	})
	worker.Register("slow", func(context.Context, Job) (json.RawMessage, error) {
		time.Sleep(450 * time.Millisecond)
		return nil, nil
	})
	if count, err := worker.RunOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}
	stored, err := repository.Get(context.Background(), "organization-a", job.ID)
	if err != nil || stored.Status != StatusSucceeded || stored.HeartbeatAt == nil || stored.LockedAt == nil || !stored.HeartbeatAt.After(*stored.LockedAt) {
		t.Fatalf("expected heartbeat before success, stored=%+v err=%v", stored, err)
	}
}

func TestNormalizeErrorCodeRejectsSensitiveFreeText(t *testing.T) {
	t.Parallel()
	if got := NormalizeErrorCode("Patient Jane Doe glucose 126"); got != "job_failed" {
		t.Fatalf("unexpected normalized error code %q", got)
	}
	if got := NormalizeErrorCode("provider.timeout"); got != "provider.timeout" {
		t.Fatalf("expected stable safe code, got %q", got)
	}
}

func TestRecurringSchedulerPersistsOneJobPerTimeBucket(t *testing.T) {
	repository := NewMemoryRepository()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunRecurringScheduler(ctx, repository, RecurringConfig{
			OrgID: "system", ProductSurface: "companion", Kind: "operational.reconcile",
			Interval: time.Hour, Timeout: time.Minute, MaxAttempts: 3,
		})
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	<-done
	jobs, err := repository.List(context.Background(), "system", "companion", "", 10)
	if err != nil || len(jobs) != 1 || jobs[0].Kind != "operational.reconcile" {
		t.Fatalf("scheduled jobs=%+v err=%v", jobs, err)
	}
}

func TestRunningJobCancellationIsRequestedThenAcknowledged(t *testing.T) {
	repo := NewMemoryRepository()
	created, _, err := repo.Enqueue(context.Background(), EnqueueInput{OrgID: "organization", ProductSurface: "companion", Kind: "test", DedupeKey: "cancel-running", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(context.Background(), ClaimOptions{WorkerID: "worker", BatchSize: 1})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if err = repo.Cancel(context.Background(), "organization", created.ID, "operator_cancelled"); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.Get(context.Background(), "organization", created.ID)
	if err != nil || pending.Status != StatusCancelRequested || pending.CompletedAt != nil {
		t.Fatalf("running cancellation must remain a request until worker acknowledgement: %+v %v", pending, err)
	}
	if err = repo.Heartbeat(context.Background(), created.ID, "worker", time.Second); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("heartbeat must observe cancellation: %v", err)
	}
	finished, err := repo.Get(context.Background(), "organization", created.ID)
	if err != nil || finished.Status != StatusCancelled || finished.CompletedAt == nil {
		t.Fatalf("cancellation must finalize without claiming rollback: %+v %v", finished, err)
	}
}
