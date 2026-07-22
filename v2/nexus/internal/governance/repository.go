package governance

import (
	"context"
	"fmt"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool        *pgxpool.Pool
	approvalTTL time.Duration
}

func NewRepository(pool *pgxpool.Pool, approvalTTL ...time.Duration) *Repository {
	ttl := time.Hour
	if len(approvalTTL) > 0 && approvalTTL[0] > 0 {
		ttl = approvalTTL[0]
	}
	return &Repository{pool: pool, approvalTTL: ttl}
}

func (r *Repository) RecordCheck(ctx context.Context, tenantID string, input domain.NormalizedCheckInput, result domain.CheckResult) (domain.RecordedCheck, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RecordedCheck{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	checkID := uuid.New()
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		INSERT INTO governance_checks (
			id,
			tenant_id,
			requester_id,
			action_type,
			target_system,
			target_resource,
			decision,
			risk_level,
			status,
			decision_reason,
			binding_hash,
			created_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, checkID.String(), tenantID, input.RequesterID, input.ActionType, input.TargetSystem, input.TargetResource, string(result.Decision), result.RiskLevel, string(result.Status), result.DecisionReason, result.BindingHash, now); err != nil {
		return domain.RecordedCheck{}, err
	}

	recorded := domain.RecordedCheck{CheckID: checkID.String()}
	if result.Decision == domain.DecisionRequireApproval {
		approvalID := uuid.New()
		expiresAt := now.Add(r.approvalTTL)
		approvalKind, quorum, postReview := "normal", 1, false
		if result.RiskLevel == "critical" {
			approvalKind, quorum, postReview = "break_glass", 2, true
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO approvals (
				id,
				tenant_id,
				governance_check_id,
				requester_id,
				supervisor_user_id,
				action_type,
				target_system,
				target_resource,
				risk_level,
				reason,
				binding_hash,
				status,
				approval_kind,
				quorum_required,
				post_review_required,
				expires_at,
				created_at,
				updated_at
			)
			VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, 'pending', $12, $13, $14, $15, $16, $16)
		`, approvalID.String(), tenantID, checkID.String(), input.RequesterID, input.SupervisorUserID, input.ActionType, input.TargetSystem, input.TargetResource, result.RiskLevel, input.Reason, result.BindingHash, approvalKind, quorum, postReview, expiresAt, now); err != nil {
			return domain.RecordedCheck{}, fmt.Errorf("create approval: %w", err)
		}
		recorded.ApprovalID = approvalID.String()
		recorded.ApprovalStatus = "pending"
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.RecordedCheck{}, err
	}
	return recorded, nil
}

var _ CheckRecorderPort = (*Repository)(nil)
