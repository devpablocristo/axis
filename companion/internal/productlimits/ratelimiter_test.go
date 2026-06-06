package productlimits

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiterScopesByOrgProductAndArea(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	limiter := NewMemoryLimiter()
	limiter.SetClock(func() time.Time { return now })
	limit := Limit{Max: 1, Window: time.Minute}
	key := Key{OrgID: "org-a", ProductSurface: "ponti", Area: AreaRuntime}

	if err := Enforce(context.Background(), limiter, key, limit); err != nil {
		t.Fatal(err)
	}
	if err := Enforce(context.Background(), limiter, key, limit); !IsRateLimited(err) {
		t.Fatalf("expected second same-scope call to be rate limited, got %v", err)
	}
	if err := Enforce(context.Background(), limiter, Key{OrgID: "org-a", ProductSurface: "pymes", Area: AreaRuntime}, limit); err != nil {
		t.Fatalf("different product should have independent bucket: %v", err)
	}
	if err := Enforce(context.Background(), limiter, Key{OrgID: "org-a", ProductSurface: "ponti", Area: AreaEval}, limit); err != nil {
		t.Fatalf("different area should have independent bucket: %v", err)
	}

	now = now.Add(time.Minute + time.Second)
	if err := Enforce(context.Background(), limiter, key, limit); err != nil {
		t.Fatalf("bucket should reset after window: %v", err)
	}
}

func TestDefaultLimitDefinesMCPArea(t *testing.T) {
	t.Parallel()

	limit := DefaultLimit(AreaMCP)
	if limit.Max <= 0 || limit.Window <= 0 {
		t.Fatalf("expected positive mcp default limit, got %+v", limit)
	}
}
