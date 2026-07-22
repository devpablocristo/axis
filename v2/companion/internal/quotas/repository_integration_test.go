package quotas

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryConcurrentLimitAndIdempotency(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_QUOTA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_QUOTA_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool, true)
	tenant := "quota-test-" + uuid.NewString()
	key := Key{TenantID: tenant, ProductSurface: "medmory", Area: AreaInbound}
	if _, err := repository.UpsertPolicy(ctx, Policy{Key: key, WindowSeconds: 60, RequestLimit: 1, UnitLimit: 10, Active: true}); err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, idempotencyKey := range []string{"one", "two"} {
		wait.Add(1)
		go func(idem string) {
			defer wait.Done()
			_, consumeErr := repository.Consume(ctx, ConsumeRequest{Key: key, IdempotencyKey: idem, Units: 1})
			results <- consumeErr
		}(idempotencyKey)
	}
	wait.Wait()
	close(results)
	allowed, denied := 0, 0
	for consumeErr := range results {
		if consumeErr == nil {
			allowed++
		} else if _, ok := RetryAfter(consumeErr); ok {
			denied++
		} else {
			t.Fatalf("unexpected consume error: %v", consumeErr)
		}
	}
	if allowed != 1 || denied != 1 {
		t.Fatalf("expected one allowed and one denied, got allowed=%d denied=%d", allowed, denied)
	}

	decision, err := repository.Consume(ctx, ConsumeRequest{Key: key, IdempotencyKey: "one", Units: 1})
	if err != nil && !decision.Allowed {
		// The winning idempotency key is nondeterministic. Verify replay against
		// whichever ledger row was allowed instead.
		decision, err = repository.Consume(ctx, ConsumeRequest{Key: key, IdempotencyKey: "two", Units: 1})
	}
	if err != nil || !decision.Allowed {
		t.Fatalf("allowed consumption must replay without another charge: %+v err=%v", decision, err)
	}

	bytesKey := Key{TenantID: tenant, ProductSurface: "medmory", Area: AreaBytes}
	if _, err := repository.UpsertPolicy(ctx, Policy{Key: bytesKey, WindowSeconds: 60, RequestLimit: 10, UnitLimit: 1, Active: true}); err != nil {
		t.Fatal(err)
	}
	decision, err = repository.Consume(ctx, ConsumeRequest{Key: bytesKey, IdempotencyKey: "oversized", Units: 2})
	if _, ok := RetryAfter(err); !ok || decision.Allowed {
		t.Fatalf("first oversized consumption must be denied: %+v err=%v", decision, err)
	}
	var windowCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM quota_windows
		WHERE tenant_id = $1 AND product_surface = $2 AND area = $3
	`, tenant, "medmory", AreaBytes).Scan(&windowCount); err != nil {
		t.Fatal(err)
	}
	if windowCount != 0 {
		t.Fatalf("denied first consumption must not create a quota window, got %d", windowCount)
	}

	if _, err := pool.Exec(ctx, `UPDATE quota_usage_ledger SET units = units WHERE tenant_id = $1`, tenant); err == nil {
		t.Fatal("usage ledger update must be rejected")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM quota_usage_ledger WHERE tenant_id = $1`, tenant); err == nil {
		t.Fatal("usage ledger delete must be rejected")
	}
}
