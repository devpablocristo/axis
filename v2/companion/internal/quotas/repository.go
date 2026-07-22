package quotas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool          *pgxpool.Pool
	requirePolicy bool
	now           func() time.Time
}

func NewRepository(pool *pgxpool.Pool, requirePolicy bool) *Repository {
	return &Repository{pool: pool, requirePolicy: requirePolicy, now: time.Now}
}

func (r *Repository) Consume(ctx context.Context, request ConsumeRequest) (Decision, error) {
	key, err := normalizeKey(request.Key)
	if err != nil {
		return Decision{}, err
	}
	request.Key = key
	if request.Units < 0 || request.IdempotencyKey == "" {
		return Decision{}, fmt.Errorf("quota consumption is invalid")
	}
	metadata, err := safeMetadata(request.Metadata)
	if err != nil {
		return Decision{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Decision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lockKey := fmt.Sprintf("%d:%s%d:%s%d:%s%d:%s", len(key.OrgID), key.OrgID,
		len(key.ProductSurface), key.ProductSurface, len(key.Area), key.Area,
		len(request.IdempotencyKey), request.IdempotencyKey)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Decision{}, err
	}
	if existing, found, err := existingDecision(ctx, tx, request); err != nil {
		return Decision{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return Decision{}, err
		}
		return existing, decisionError(key, existing)
	}

	var policy Policy
	err = tx.QueryRow(ctx, `
		SELECT window_seconds, request_limit, unit_limit, active, created_at, updated_at
		FROM quota_policies
		WHERE org_id = $1 AND product_surface = $2 AND area = $3 AND active = true
		FOR SHARE
	`, key.OrgID, key.ProductSurface, key.Area).Scan(
		&policy.WindowSeconds, &policy.RequestLimit, &policy.UnitLimit, &policy.Active, &policy.CreatedAt, &policy.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		decision := Decision{Allowed: !r.requirePolicy, PolicyFound: false}
		if r.requirePolicy {
			decision.RetryAfterSeconds = 60
		}
		if err := insertLedger(ctx, tx, request, decision, metadata, "", 0); err != nil {
			return Decision{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Decision{}, err
		}
		return decision, decisionError(key, decision)
	}
	if err != nil {
		return Decision{}, err
	}
	policy.Key = key
	now := r.now().UTC()
	window := int64(policy.WindowSeconds)
	windowStart := time.Unix((now.Unix()/window)*window, 0).UTC()
	retryAfter := int(windowStart.Add(time.Duration(policy.WindowSeconds)*time.Second).Sub(now).Seconds()) + 1
	decision := Decision{PolicyFound: true, RetryAfterSeconds: retryAfter}
	err = tx.QueryRow(ctx, `
		INSERT INTO quota_windows (
			org_id, product_surface, area, window_started_at, request_count, unit_count, updated_at
		)
		SELECT $1, $2, $3, $4, 1, $5::bigint, $6
		WHERE $5::bigint <= $8::bigint
		ON CONFLICT (org_id, product_surface, area, window_started_at) DO UPDATE
		SET request_count = quota_windows.request_count + 1,
			unit_count = quota_windows.unit_count + EXCLUDED.unit_count,
			updated_at = EXCLUDED.updated_at
		WHERE quota_windows.request_count + 1 <= $7
		  AND quota_windows.unit_count + EXCLUDED.unit_count <= $8
		RETURNING request_count, unit_count
	`, key.OrgID, key.ProductSurface, key.Area, windowStart, request.Units, now,
		policy.RequestLimit, policy.UnitLimit).Scan(&decision.RequestsUsed, &decision.UnitsUsed)
	if errors.Is(err, pgx.ErrNoRows) {
		decision.Allowed = false
	} else if err != nil {
		return Decision{}, err
	} else {
		decision.Allowed = true
		decision.RetryAfterSeconds = 0
	}
	if err := insertLedger(ctx, tx, request, decision, metadata, "", 0); err != nil {
		return Decision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Decision{}, err
	}
	return decision, decisionError(key, decision)
}

func (r *Repository) RecordUsage(ctx context.Context, usage Usage) error {
	key, err := normalizeKey(usage.Key)
	if err != nil {
		return err
	}
	if usage.Units < 0 || usage.EstimatedCostMicroUSD < 0 || usage.IdempotencyKey == "" {
		return fmt.Errorf("usage record is invalid")
	}
	metadata, err := safeMetadata(usage.Metadata)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO quota_usage_ledger (
			id, org_id, product_surface, area, idempotency_key, subject_type, subject_id,
			units, allowed, policy_found, model, estimated_cost_microusd, metadata
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, true, false, $9, $10, $11::jsonb)
		ON CONFLICT (org_id, product_surface, area, idempotency_key) DO NOTHING
	`, uuid.NewString(), key.OrgID, key.ProductSurface, key.Area, usage.IdempotencyKey,
		usage.SubjectType, usage.SubjectID, usage.Units, usage.Model, usage.EstimatedCostMicroUSD, metadata)
	return err
}

func (r *Repository) UpsertPolicy(ctx context.Context, policy Policy) (Policy, error) {
	policy, err := validatePolicy(policy)
	if err != nil {
		return Policy{}, err
	}
	if !policy.Active {
		var inUse bool
		err := r.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM capabilities
				WHERE org_id = $1
				  AND promotion_state = 'active'
				  AND archived_at IS NULL AND trashed_at IS NULL
				  AND manifest->>'product_surface' = $2
				  AND (manifest->'quota_areas') ? $3
			)
		`, policy.OrgID, policy.ProductSurface, policy.Area).Scan(&inUse)
		if err != nil {
			return Policy{}, err
		}
		if inUse {
			return Policy{}, ErrPolicyInUse
		}
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO quota_policies (
			org_id, product_surface, area, window_seconds, request_limit, unit_limit, active
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (org_id, product_surface, area) DO UPDATE
		SET window_seconds = EXCLUDED.window_seconds,
			request_limit = EXCLUDED.request_limit,
			unit_limit = EXCLUDED.unit_limit,
			active = EXCLUDED.active,
			updated_at = now()
		RETURNING created_at, updated_at
	`, policy.OrgID, policy.ProductSurface, policy.Area, policy.WindowSeconds,
		policy.RequestLimit, policy.UnitLimit, policy.Active).Scan(&policy.CreatedAt, &policy.UpdatedAt)
	return policy, err
}

func (r *Repository) ListPolicies(ctx context.Context, orgID, productSurface string) ([]Policy, error) {
	key, err := normalizeKey(Key{OrgID: orgID, ProductSurface: productSurface, Area: "list"})
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT area, window_seconds, request_limit, unit_limit, active, created_at, updated_at
		FROM quota_policies
		WHERE org_id = $1 AND product_surface = $2
		ORDER BY area
	`, key.OrgID, key.ProductSurface)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var policies []Policy
	for rows.Next() {
		policy := Policy{Key: Key{OrgID: key.OrgID, ProductSurface: key.ProductSurface}}
		if err := rows.Scan(&policy.Area, &policy.WindowSeconds, &policy.RequestLimit, &policy.UnitLimit, &policy.Active, &policy.CreatedAt, &policy.UpdatedAt); err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *Repository) HasActivePolicies(ctx context.Context, orgID, productSurface string, areas []string) (bool, error) {
	if len(areas) == 0 {
		return false, nil
	}
	key, err := normalizeKey(Key{OrgID: orgID, ProductSurface: productSurface, Area: "check"})
	if err != nil {
		return false, err
	}
	var count int
	err = r.pool.QueryRow(ctx, `
		SELECT count(DISTINCT area)
		FROM quota_policies
		WHERE org_id = $1 AND product_surface = $2 AND active = true AND area = ANY($3::text[])
	`, key.OrgID, key.ProductSurface, areas).Scan(&count)
	return count == len(areas), err
}

func existingDecision(ctx context.Context, tx pgx.Tx, request ConsumeRequest) (Decision, bool, error) {
	var decision Decision
	err := tx.QueryRow(ctx, `
		SELECT allowed, policy_found, retry_after_seconds
		FROM quota_usage_ledger
		WHERE org_id = $1 AND product_surface = $2 AND area = $3 AND idempotency_key = $4
	`, request.OrgID, request.ProductSurface, request.Area, request.IdempotencyKey).Scan(
		&decision.Allowed, &decision.PolicyFound, &decision.RetryAfterSeconds,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Decision{}, false, nil
	}
	return decision, err == nil, err
}

func insertLedger(ctx context.Context, tx pgx.Tx, request ConsumeRequest, decision Decision, metadata []byte, model string, cost int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO quota_usage_ledger (
			id, org_id, product_surface, area, idempotency_key, subject_type, subject_id,
			units, allowed, policy_found, retry_after_seconds, model, estimated_cost_microusd, metadata
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
	`, uuid.NewString(), request.OrgID, request.ProductSurface, request.Area, request.IdempotencyKey,
		request.SubjectType, request.SubjectID, request.Units, decision.Allowed, decision.PolicyFound,
		decision.RetryAfterSeconds, model, cost, metadata)
	return err
}

func safeMetadata(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	if len(metadata) > 32 {
		return nil, fmt.Errorf("usage metadata has too many fields")
	}
	raw, err := json.Marshal(metadata)
	if err != nil || len(raw) > 16<<10 {
		return nil, fmt.Errorf("usage metadata is invalid")
	}
	return raw, nil
}

func decisionError(key Key, decision Decision) error {
	if decision.Allowed {
		return nil
	}
	return &ExceededError{Key: key, RetryAfter: decision.RetryAfterSeconds, Missing: !decision.PolicyFound}
}

var _ QuotaPort = (*Repository)(nil)
var _ UsageLedgerPort = (*Repository)(nil)
