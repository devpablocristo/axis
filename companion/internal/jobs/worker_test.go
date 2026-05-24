package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestMemoryRepository_DedupeAndLeaseRecovery(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository()
	job, inserted, err := repo.Enqueue(context.Background(), EnqueueInput{
		OrgID:     "org-1",
		Kind:      "demo",
		DedupeKey: "demo:1",
		Payload:   json.RawMessage(`{"n":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("expected first enqueue to insert")
	}
	again, inserted, err := repo.Enqueue(context.Background(), EnqueueInput{
		OrgID:          "org-1",
		Kind:           "demo",
		DedupeKey:      "demo:1",
		Payload:        json.RawMessage(`{"n":2}`),
		ReplacePayload: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inserted || again.ID != job.ID {
		t.Fatalf("expected dedupe to return existing job, inserted=%v job=%s existing=%s", inserted, job.ID, again.ID)
	}

	claimed, err := repo.Claim(context.Background(), ClaimOptions{WorkerID: "w1", BatchSize: 1, LeaseDuration: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected one claimed job, got %d", len(claimed))
	}
	time.Sleep(2 * time.Millisecond)
	recovered, err := repo.RecoverExpiredLeases(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered lease, got %d", recovered)
	}
	claimed, err = repo.Claim(context.Background(), ClaimOptions{WorkerID: "w2", BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].LeaseOwner != "w2" {
		t.Fatalf("expected recovered job to be claimable by w2, got %+v", claimed)
	}
}

func TestWorker_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository()
	job, _, err := repo.Enqueue(context.Background(), EnqueueInput{
		OrgID:       "org-1",
		Kind:        "demo",
		DedupeKey:   "demo:retry",
		MaxAttempts: 3,
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	worker := NewWorker(repo, WorkerConfig{
		WorkerID:    "w1",
		Concurrency: 1,
		Backoff:     func(int) time.Duration { return time.Millisecond },
	})
	worker.Register("demo", func(context.Context, Job) (json.RawMessage, error) {
		calls++
		if calls == 1 {
			return json.RawMessage(`{"attempt":1}`), errors.New("transient")
		}
		return json.RawMessage(`{"ok":true}`), nil
	})
	if claimed, err := worker.RunOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("first run claimed=%d err=%v", claimed, err)
	}
	stored, err := repo.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusQueued || stored.Attempts != 1 {
		t.Fatalf("expected retry queued after first failure, got %+v", stored)
	}
	time.Sleep(2 * time.Millisecond)
	if claimed, err := worker.RunOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("second run claimed=%d err=%v", claimed, err)
	}
	stored, err = repo.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusSucceeded || calls != 2 {
		t.Fatalf("expected success after retry, calls=%d job=%+v", calls, stored)
	}
}

func TestWorker_PermanentFailureGoesToDeadLetter(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository()
	job, _, err := repo.Enqueue(context.Background(), EnqueueInput{
		OrgID:       "org-1",
		Kind:        "demo",
		DedupeKey:   "demo:permanent",
		MaxAttempts: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(repo, WorkerConfig{WorkerID: "w1", Concurrency: 1})
	worker.Register("demo", func(context.Context, Job) (json.RawMessage, error) {
		return json.RawMessage(`{"reason":"bad_input"}`), Permanent(errors.New("bad input"))
	})
	if claimed, err := worker.RunOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("run claimed=%d err=%v", claimed, err)
	}
	stored, err := repo.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusDeadLetter {
		t.Fatalf("expected dead letter, got %+v", stored)
	}
}

func TestWorker_TimeoutIsRetryable(t *testing.T) {
	t.Parallel()

	repo := NewMemoryRepository()
	deadline := time.Now().UTC().Add(10 * time.Millisecond)
	job, _, err := repo.Enqueue(context.Background(), EnqueueInput{
		OrgID:      "org-1",
		Kind:       "slow",
		DedupeKey:  "slow:1",
		DeadlineAt: &deadline,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(repo, WorkerConfig{
		WorkerID:    "w1",
		Concurrency: 1,
		Backoff:     func(int) time.Duration { return time.Millisecond },
	})
	worker.Register("slow", func(ctx context.Context, _ Job) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if claimed, err := worker.RunOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("run claimed=%d err=%v", claimed, err)
	}
	stored, err := repo.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusQueued || stored.LastError == "" {
		t.Fatalf("expected timeout retry, got %+v", stored)
	}
}
