package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type RecurringConfig struct {
	TenantID       string
	ProductSurface string
	Kind           string
	ShardKey       string
	DedupePrefix   string
	Interval       time.Duration
	Timeout        time.Duration
	MaxAttempts    int
}

// RunRecurringScheduler only materializes due work. It never executes work:
// PostgreSQL retains each logical tick and workers claim it using a lease.
func RunRecurringScheduler(ctx context.Context, repository Repository, config RecurringConfig) {
	if config.Interval <= 0 {
		return
	}
	enqueue := func(now time.Time) {
		bucket := now.UTC().Truncate(config.Interval)
		prefix := strings.TrimSpace(config.DedupePrefix)
		if prefix == "" {
			prefix = strings.TrimSpace(config.Kind)
		}
		_, _, err := repository.Enqueue(ctx, EnqueueInput{
			TenantID: config.TenantID, ProductSurface: config.ProductSurface,
			Kind: config.Kind, ShardKey: config.ShardKey,
			DedupeKey: fmt.Sprintf("%s:%d", prefix, bucket.Unix()),
			Payload:   json.RawMessage(`{}`), RunAfter: bucket,
			Timeout: config.Timeout, MaxAttempts: config.MaxAttempts,
		})
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "recurring job enqueue failed", "kind", config.Kind, "error", err)
		}
	}
	enqueue(time.Now())
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			enqueue(now)
		}
	}
}
