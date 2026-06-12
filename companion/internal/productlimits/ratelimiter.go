package productlimits

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	DefaultProductSurface = "companion"

	AreaRuntime            = "runtime"
	AreaConnectorExecution = "connector_execution"
	AreaWatcher            = "watcher"
	AreaEval               = "eval"
	AreaMCP                = "mcp"
)

type Key struct {
	OrgID          string
	ProductSurface string
	Area           string
}

type Limit struct {
	Max    int
	Window time.Duration
}

type Decision struct {
	Allowed    bool
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration
}

type Limiter interface {
	Allow(ctx context.Context, key Key, limit Limit) (Decision, error)
}

type RateLimitError struct {
	Key        Key
	Limit      Limit
	RetryAfter time.Duration
}

func (e RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded for org_id=%s product_surface=%s area=%s", e.Key.OrgID, e.Key.ProductSurface, e.Key.Area)
}

func IsRateLimited(err error) bool {
	var rateErr RateLimitError
	return errors.As(err, &rateErr)
}

func Enforce(ctx context.Context, limiter Limiter, key Key, limit Limit) error {
	key = normalizeKey(key)
	limit = normalizeLimit(limit)
	if limiter == nil || limit.Max <= 0 {
		return nil
	}
	decision, err := limiter.Allow(ctx, key, limit)
	if err != nil {
		return err
	}
	if decision.Allowed {
		return nil
	}
	retryAfter := decision.RetryAfter
	if retryAfter <= 0 && !decision.ResetAt.IsZero() {
		retryAfter = time.Until(decision.ResetAt)
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	return RateLimitError{Key: key, Limit: limit, RetryAfter: retryAfter}
}

func DefaultLimit(area string) Limit {
	switch strings.TrimSpace(area) {
	case AreaRuntime:
		return Limit{Max: 120, Window: time.Minute}
	case AreaConnectorExecution:
		return Limit{Max: 600, Window: time.Minute}
	case AreaWatcher:
		return Limit{Max: 300, Window: time.Minute}
	case AreaEval:
		return Limit{Max: 30, Window: time.Minute}
	case AreaMCP:
		return Limit{Max: 120, Window: time.Minute}
	default:
		return Limit{Max: 120, Window: time.Minute}
	}
}

type MemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
	now     func() time.Time
}

type bucket struct {
	count   int
	resetAt time.Time
}

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{
		buckets: make(map[string]bucket),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (l *MemoryLimiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now == nil {
		l.now = func() time.Time { return time.Now().UTC() }
		return
	}
	l.now = now
}

func (l *MemoryLimiter) Allow(_ context.Context, key Key, limit Limit) (Decision, error) {
	key = normalizeKey(key)
	limit = normalizeLimit(limit)
	if limit.Max <= 0 {
		return Decision{Allowed: true}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[string]bucket)
	}
	now := l.now()
	storageKey := key.storageKey()
	current := l.buckets[storageKey]
	if current.resetAt.IsZero() || !now.Before(current.resetAt) {
		current = bucket{resetAt: now.Add(limit.Window)}
	}
	if current.count >= limit.Max {
		return Decision{
			Allowed:    false,
			Remaining:  0,
			ResetAt:    current.resetAt,
			RetryAfter: current.resetAt.Sub(now),
		}, nil
	}
	current.count++
	l.buckets[storageKey] = current
	return Decision{
		Allowed:   true,
		Remaining: maxInt(0, limit.Max-current.count),
		ResetAt:   current.resetAt,
	}, nil
}

func normalizeKey(key Key) Key {
	key.OrgID = strings.TrimSpace(key.OrgID)
	key.ProductSurface = strings.TrimSpace(strings.ToLower(key.ProductSurface))
	if key.ProductSurface == "" {
		key.ProductSurface = DefaultProductSurface
	}
	key.Area = strings.TrimSpace(strings.ToLower(key.Area))
	return key
}

func normalizeLimit(limit Limit) Limit {
	if limit.Window <= 0 {
		limit.Window = time.Minute
	}
	return limit
}

func (k Key) storageKey() string {
	return strings.Join([]string{k.OrgID, k.ProductSurface, k.Area}, "|")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
