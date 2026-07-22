package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestValidMessageTypeRequiresExactAggregateKindPair(t *testing.T) {
	tests := []struct {
		aggregateType string
		kind          string
		want          bool
	}{
		{AggregateTypeExecutionAttempt, KindExecutionResult, true},
		{AggregateTypeProfessionalAuthority, KindAuditEvent, true},
		{AggregateTypeExecutionAttempt, KindAuditEvent, false},
		{AggregateTypeProfessionalAuthority, KindExecutionResult, false},
		{"unknown", KindAuditEvent, false},
	}
	for _, test := range tests {
		if got := validMessageType(test.aggregateType, test.kind); got != test.want {
			t.Fatalf("validMessageType(%q, %q)=%v want %v", test.aggregateType, test.kind, got, test.want)
		}
	}
}

func TestPostgresOutboxTransactionalClaimDeadLetterReplayAndProjection(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_OUTBOX_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_OUTBOX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool)
	tenantID := "outbox-test-" + uuid.NewString()
	messageID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM companion_nexus_outbox WHERE tenant_id=$1`, tenantID)
	})
	input := EnqueueInput{
		ID: messageID, TenantID: tenantID, AggregateType: "execution_attempt",
		AggregateID: uuid.New(), Kind: "execution_result", DedupeKey: "execution-1",
		Payload: json.RawMessage(`{"binding_hash":"sha256:test"}`),
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, inserted, err := repository.EnqueueTx(ctx, tx, input); err != nil || !inserted {
		t.Fatalf("rollback enqueue inserted=%v err=%v", inserted, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, tenantID, messageID); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("rolled-back message must not exist: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, inserted, err := repository.EnqueueTx(ctx, tx, input); err != nil || !inserted {
		t.Fatalf("commit enqueue inserted=%v err=%v", inserted, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var waitGroup sync.WaitGroup
	claims := make(chan []Message, 2)
	claimErrors := make(chan error, 2)
	for _, workerID := range []string{"replica-a", "replica-b"} {
		workerID := workerID
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			messages, err := repository.Claim(ctx, ClaimOptions{WorkerID: workerID, Batch: 1, Lease: time.Minute})
			claims <- messages
			claimErrors <- err
		}()
	}
	waitGroup.Wait()
	close(claims)
	close(claimErrors)
	for err := range claimErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var claimed []Message
	for messages := range claims {
		claimed = append(claimed, messages...)
	}
	if len(claimed) != 1 || claimed[0].ID != messageID {
		t.Fatalf("expected one logical claim, got %+v", claimed)
	}
	message, err := repository.MarkFailed(ctx, messageID, claimed[0].LeaseOwner, "nexus_unavailable", true, time.Second)
	if err != nil || message.Status != StatusPending || message.Attempts != 1 {
		t.Fatalf("first failure=%+v err=%v", message, err)
	}
	for expectedAttempt := 2; expectedAttempt <= MaxDeliveryAttempts; expectedAttempt++ {
		if _, err := pool.Exec(ctx, `UPDATE companion_nexus_outbox SET available_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, messageID); err != nil {
			t.Fatal(err)
		}
		claimed, err = repository.Claim(ctx, ClaimOptions{WorkerID: "replica-a", Batch: 1, Lease: time.Minute})
		if err != nil || len(claimed) != 1 {
			t.Fatalf("attempt %d claim=%+v err=%v", expectedAttempt, claimed, err)
		}
		message, err = repository.MarkFailed(ctx, messageID, "replica-a", "nexus_unavailable", true, time.Second)
		if err != nil || message.Attempts != expectedAttempt {
			t.Fatalf("attempt %d message=%+v err=%v", expectedAttempt, message, err)
		}
	}
	if message.Status != StatusDead || message.LastErrorCode != "nexus_unavailable" {
		t.Fatalf("expected dead-letter after ten attempts, got %+v", message)
	}
	replayed, err := repository.Replay(ctx, tenantID, messageID, time.Now().UTC())
	if err != nil || replayed.Status != StatusPending || replayed.Attempts != 0 {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	claimed, err = repository.Claim(ctx, ClaimOptions{WorkerID: "replica-c", Batch: 1, Lease: time.Minute})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("replayed claim=%+v err=%v", claimed, err)
	}
	if err := repository.MarkDelivered(ctx, messageID, "replica-c"); err != nil {
		t.Fatal(err)
	}
	delivered, err := repository.Get(ctx, tenantID, messageID)
	if err != nil || delivered.Status != StatusDelivered || delivered.DeliveredAt == nil {
		t.Fatalf("delivered=%+v err=%v", delivered, err)
	}
}

func TestDispatcherPersistsOnlySafeErrorCode(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_OUTBOX_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_OUTBOX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool)
	tenantID := "outbox-dispatcher-test-" + uuid.NewString()
	messageID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM companion_nexus_outbox WHERE tenant_id=$1`, tenantID)
	})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.EnqueueTx(ctx, tx, EnqueueInput{
		ID: messageID, TenantID: tenantID, AggregateType: "execution_attempt",
		AggregateID: uuid.New(), Kind: "execution_result", DedupeKey: "execution-sensitive",
		Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(repository, SenderFunc(func(context.Context, Message) error {
		return errors.New("patient name and signed URL must not be persisted")
	}), DispatcherConfig{WorkerID: "dispatcher", Concurrency: 1, BaseBackoff: time.Second})
	if count, err := dispatcher.RunOnce(ctx); err != nil || count != 1 {
		t.Fatalf("dispatch count=%d err=%v", count, err)
	}
	message, err := repository.Get(ctx, tenantID, messageID)
	if err != nil || message.Status != StatusPending || message.LastErrorCode != "delivery_failed" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
}
