package ops

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/devpablocristo/platform/authn/go/identityhttp"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RateLimiter struct {
	db *sharedpostgres.DB
}

type rateRule struct {
	ID            uuid.UUID
	Mode          string
	WindowSeconds int
	MaxRequests   int
}

type rateDecision struct {
	Allowed        bool
	Mode           string
	LimitRemaining int
	Reason         string
}

func NewRateLimiter(db *sharedpostgres.DB) *RateLimiter {
	return &RateLimiter{db: db}
}

func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l == nil || l.db == nil || isRateLimitBypassPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		decision, err := l.check(r.Context(), r)
		if err != nil {
			slog.Error("nexus rate limit check failed", "error", err, "path", r.URL.Path)
			httpjson.WriteFlatError(w, http.StatusServiceUnavailable, "RATE_LIMIT_UNAVAILABLE", "rate limit check unavailable")
			return
		}
		if !decision.Allowed && decision.Mode == "enforce" {
			w.Header().Set("Retry-After", "60")
			httpjson.WriteFlatError(w, http.StatusTooManyRequests, "RATE_LIMITED", decision.Reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RateLimiter) check(ctx context.Context, r *http.Request) (rateDecision, error) {
	identity := identityhttp.FromRequest(r)
	orgID := strings.TrimSpace(identity.OrgID)
	principalID := strings.TrimSpace(identity.Actor)
	endpoint := strings.TrimSpace(r.Method + " " + r.URL.Path)
	pathOnly := strings.TrimSpace(r.URL.Path)

	rule, ok, err := l.matchRule(ctx, orgID, principalID, endpoint, pathOnly)
	if err != nil {
		return rateDecision{}, err
	}
	if !ok {
		return rateDecision{Allowed: true, Mode: "none", LimitRemaining: -1}, nil
	}
	if rule.WindowSeconds <= 0 || rule.MaxRequests <= 0 {
		return rateDecision{Allowed: true, Mode: rule.Mode, LimitRemaining: -1}, nil
	}
	bucketKey := strings.Join([]string{orgID, principalID, endpoint}, "|")
	var count int
	if err := l.db.Pool().QueryRow(ctx, `
		WITH window AS (
			SELECT to_timestamp(floor(extract(epoch from now()) / $2::int) * $2::int) AS starts_at
		)
		INSERT INTO nexus_rate_limit_counters (rule_id, bucket_key, window_start, count)
		SELECT $1, $3, starts_at, 1 FROM window
		ON CONFLICT (rule_id, bucket_key, window_start)
		DO UPDATE SET count = nexus_rate_limit_counters.count + 1, updated_at = now()
		RETURNING count
	`, rule.ID, rule.WindowSeconds, bucketKey).Scan(&count); err != nil {
		return rateDecision{}, fmt.Errorf("increment rate limit counter: %w", err)
	}
	remaining := rule.MaxRequests - count
	allowed := count <= rule.MaxRequests
	reason := "ok"
	if !allowed {
		reason = "rate limit exceeded"
	}
	if err := l.recordDecision(ctx, orgID, principalID, endpoint, rule, allowed, remaining, reason); err != nil {
		return rateDecision{}, err
	}
	return rateDecision{Allowed: allowed, Mode: rule.Mode, LimitRemaining: remaining, Reason: reason}, nil
}

func (l *RateLimiter) matchRule(ctx context.Context, orgID, principalID, endpoint, pathOnly string) (rateRule, bool, error) {
	row := l.db.Pool().QueryRow(ctx, `
		SELECT id, mode, window_seconds, max_requests
		FROM nexus_rate_limit_rules
		WHERE enabled = true
		  AND (org_id IS NULL OR org_id = $1)
		  AND (principal_id IS NULL OR principal_id = $2)
		  AND action_type IS NULL
		  AND (endpoint IS NULL OR endpoint = $3 OR endpoint = $4)
		ORDER BY
		  (CASE WHEN org_id IS NULL THEN 0 ELSE 1 END) +
		  (CASE WHEN principal_id IS NULL THEN 0 ELSE 1 END) +
		  (CASE WHEN endpoint IS NULL THEN 0 ELSE 1 END) DESC,
		  created_at DESC
		LIMIT 1
	`, nullIfEmpty(orgID), nullIfEmpty(principalID), endpoint, pathOnly)
	var rule rateRule
	if err := row.Scan(&rule.ID, &rule.Mode, &rule.WindowSeconds, &rule.MaxRequests); err != nil {
		if err == pgx.ErrNoRows {
			return rateRule{}, false, nil
		}
		return rateRule{}, false, fmt.Errorf("match rate limit rule: %w", err)
	}
	return rule, true, nil
}

func (l *RateLimiter) recordDecision(ctx context.Context, orgID, principalID, endpoint string, rule rateRule, allowed bool, remaining int, reason string) error {
	_, err := l.db.Pool().Exec(ctx, `
		INSERT INTO nexus_rate_limit_decisions
			(org_id, principal_id, endpoint, rule_id, mode, allowed, limit_remaining, reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, nullIfEmpty(orgID), nullIfEmpty(principalID), endpoint, rule.ID, rule.Mode, allowed, remaining, reason)
	if err != nil {
		return fmt.Errorf("record rate limit decision: %w", err)
	}
	return nil
}

func isRateLimitBypassPath(path string) bool {
	switch strings.TrimSpace(path) {
	case "/healthz", "/readyz", "/metrics":
		return true
	default:
		return false
	}
}

func nullIfEmpty(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
