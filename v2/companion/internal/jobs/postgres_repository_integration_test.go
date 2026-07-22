package jobs

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositoryConcurrentClaimRecoveryAndReplay(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_JOBS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_JOBS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	orgID := "jobs-test-" + uuid.NewString()
	kind := "test.concurrent." + uuid.NewString()
	var created []uuid.UUID
	t.Cleanup(func() {
		for _, id := range created {
			_, _ = pool.Exec(context.Background(), `DELETE FROM companion_runtime_jobs WHERE org_id=$1 AND id=$2`, orgID, id)
		}
	})

	job, inserted, err := repository.Enqueue(ctx, EnqueueInput{
		OrgID: orgID, ProductSurface: "tests", Kind: kind,
		DedupeKey: "logical-1", MaxAttempts: 2,
	})
	if err != nil || !inserted {
		t.Fatalf("enqueue inserted=%v err=%v", inserted, err)
	}
	created = append(created, job.ID)

	var waitGroup sync.WaitGroup
	claims := make(chan []Job, 2)
	errors := make(chan error, 2)
	for _, workerID := range []string{"replica-a", "replica-b"} {
		workerID := workerID
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			claimed, err := repository.Claim(ctx, ClaimOptions{
				WorkerID: workerID, Kinds: []string{kind}, BatchSize: 1, LeaseDuration: time.Minute,
			})
			claims <- claimed
			errors <- err
		}()
	}
	waitGroup.Wait()
	close(claims)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var claimed []Job
	for result := range claims {
		claimed = append(claimed, result...)
	}
	if len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("expected one logical claim, got %+v", claimed)
	}
	if err := repository.Complete(ctx, job.ID, claimed[0].LeaseOwner, nil); err != nil {
		t.Fatal(err)
	}
	deduplicated, inserted, err := repository.Enqueue(ctx, EnqueueInput{
		OrgID: orgID, ProductSurface: "tests", Kind: kind, DedupeKey: "logical-1",
	})
	if err != nil || inserted || deduplicated.ID != job.ID || deduplicated.Status != StatusSucceeded {
		t.Fatalf("durable dedupe=%+v inserted=%v err=%v", deduplicated, inserted, err)
	}

	deadJob, _, err := repository.Enqueue(ctx, EnqueueInput{
		OrgID: orgID, ProductSurface: "tests", Kind: kind,
		DedupeKey: "logical-2", MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	created = append(created, deadJob.ID)
	claimed, err = repository.Claim(ctx, ClaimOptions{
		WorkerID: "crashed-replica", Kinds: []string{kind}, BatchSize: 1, LeaseDuration: time.Minute,
	})
	if err != nil || len(claimed) != 1 || claimed[0].ID != deadJob.ID {
		t.Fatalf("dead-letter claim=%+v err=%v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE companion_runtime_jobs SET lease_until=now()-interval '1 second' WHERE org_id=$1 AND id=$2`, orgID, deadJob.ID); err != nil {
		t.Fatal(err)
	}
	recovery, err := repository.RecoverExpiredLeases(ctx, 10)
	if err != nil || recovery.DeadLetter != 1 {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}
	replayed, err := repository.ReplayDeadLetter(ctx, orgID, deadJob.ID, time.Now().UTC())
	if err != nil || replayed.Status != StatusQueued || replayed.Attempts != 0 {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}

func TestPostgresRepositoryWorkerControlAndCircuitBreaker(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_JOBS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_JOBS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	orgID := "jobs-test-" + uuid.NewString()
	product, kind := "tests", "test.circuit."+uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM companion_runtime_jobs WHERE org_id=$1`, orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM companion_worker_controls WHERE org_id=$1`, orgID)
	})

	if _, err = pool.Exec(ctx, `INSERT INTO companion_worker_controls(org_id,product_surface,kind,state,changed_by,reason_code)VALUES($1,$2,$3,'paused','test','manual_pause')`, orgID, product, kind); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, _, err = repository.Enqueue(ctx, EnqueueInput{OrgID: orgID, ProductSurface: product, Kind: kind, DedupeKey: uuid.NewString(), MaxAttempts: 10}); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := repository.Claim(ctx, ClaimOptions{WorkerID: "paused-worker", Kinds: []string{kind}, BatchSize: 10})
	if err != nil || len(claimed) != 0 {
		t.Fatalf("paused control claimed=%d err=%v", len(claimed), err)
	}
	if _, err = pool.Exec(ctx, `UPDATE companion_worker_controls SET state='closed' WHERE org_id=$1 AND product_surface=$2 AND kind=$3`, orgID, product, kind); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		claimed, err = repository.Claim(ctx, ClaimOptions{WorkerID: "failing-worker", Kinds: []string{kind}, BatchSize: 1})
		if err != nil || len(claimed) != 1 {
			t.Fatalf("failure claim %d claimed=%d err=%v", attempt, len(claimed), err)
		}
		if _, err = repository.Fail(ctx, FailInput{JobID: claimed[0].ID, WorkerID: "failing-worker", ErrorCode: "dependency_unavailable", Retryable: true, Backoff: time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		_, _ = pool.Exec(ctx, `UPDATE companion_runtime_jobs SET run_after=now() WHERE org_id=$1 AND status='queued'`, orgID)
	}
	var state string
	var failures int
	if err = pool.QueryRow(ctx, `SELECT state,failure_count FROM companion_worker_controls WHERE org_id=$1 AND product_surface=$2 AND kind=$3`, orgID, product, kind).Scan(&state, &failures); err != nil {
		t.Fatal(err)
	}
	if state != "open" || failures != 5 {
		t.Fatalf("expected open circuit after five failures, state=%s failures=%d", state, failures)
	}
	claimed, err = repository.Claim(ctx, ClaimOptions{WorkerID: "blocked-worker", Kinds: []string{kind}, BatchSize: 10})
	if err != nil || len(claimed) != 0 {
		t.Fatalf("open circuit claimed=%d err=%v", len(claimed), err)
	}
	if _, err = pool.Exec(ctx, `UPDATE companion_worker_controls SET opened_until=now()-interval '1 second' WHERE org_id=$1 AND product_surface=$2 AND kind=$3`, orgID, product, kind); err != nil {
		t.Fatal(err)
	}
	claimed, err = repository.Claim(ctx, ClaimOptions{WorkerID: "probe-worker", Kinds: []string{kind}, BatchSize: 10})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("half-open probe claimed=%d err=%v", len(claimed), err)
	}
	if err = repository.Complete(ctx, claimed[0].ID, "probe-worker", nil); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT state,failure_count FROM companion_worker_controls WHERE org_id=$1 AND product_surface=$2 AND kind=$3`, orgID, product, kind).Scan(&state, &failures); err != nil {
		t.Fatal(err)
	}
	if state != "closed" || failures != 0 {
		t.Fatalf("successful probe did not close circuit, state=%s failures=%d", state, failures)
	}
}
