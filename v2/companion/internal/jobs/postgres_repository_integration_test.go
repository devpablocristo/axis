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
	tenantID := "jobs-test-" + uuid.NewString()
	kind := "test.concurrent"
	var created []uuid.UUID
	t.Cleanup(func() {
		for _, id := range created {
			_, _ = pool.Exec(context.Background(), `DELETE FROM companion_jobs WHERE tenant_id=$1 AND id=$2`, tenantID, id)
		}
	})

	job, inserted, err := repository.Enqueue(ctx, EnqueueInput{
		TenantID: tenantID, ProductSurface: "tests", Kind: kind,
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
		TenantID: tenantID, ProductSurface: "tests", Kind: kind, DedupeKey: "logical-1",
	})
	if err != nil || inserted || deduplicated.ID != job.ID || deduplicated.Status != StatusSucceeded {
		t.Fatalf("durable dedupe=%+v inserted=%v err=%v", deduplicated, inserted, err)
	}

	deadJob, _, err := repository.Enqueue(ctx, EnqueueInput{
		TenantID: tenantID, ProductSurface: "tests", Kind: kind,
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
	if _, err := pool.Exec(ctx, `UPDATE companion_jobs SET lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, tenantID, deadJob.ID); err != nil {
		t.Fatal(err)
	}
	recovery, err := repository.RecoverExpiredLeases(ctx, 10)
	if err != nil || recovery.DeadLetter != 1 {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}
	replayed, err := repository.ReplayDeadLetter(ctx, tenantID, deadJob.ID, time.Now().UTC())
	if err != nil || replayed.Status != StatusQueued || replayed.Attempts != 0 {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}
