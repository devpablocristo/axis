package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	policyMatches, _ := json.Marshal(result.PolicyMatches)
	functionalRoles, _ := json.Marshal(input.FunctionalRoles)
	functionalScopes, _ := json.Marshal(input.FunctionalScopes)
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
			authority_binding_hash,
			scope_revision,
			policy_revision_hash,
			delegation_id,
			delegation_revision,
			policy_snapshot_hash,
			policy_matches,
			policy_input_hash,
			product_surface,
			requester_type,
			resource_type,
			membership_role,
			functional_roles,
			functional_scopes,
			created_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
	`, checkID.String(), tenantID, input.RequesterID, input.ActionType, input.TargetSystem, input.TargetResource,
		string(result.Decision), result.RiskLevel, string(result.Status), result.DecisionReason, result.BindingHash,
		input.AuthorityBindingHash, input.ScopeRevision, input.PolicyRevisionHash, input.DelegationID,
		input.DelegationRevision, result.PolicySnapshotHash, policyMatches, result.PolicyInputHash, input.ProductSurface,
		input.RequesterType, input.ResourceType, input.MembershipRole, functionalRoles, functionalScopes, now); err != nil {
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
				product_surface,
				supervisor_user_id,
				action_type,
				target_system,
				target_resource,
				resource_type,
				risk_level,
				reason,
				binding_hash,
				authority_binding_hash,
				scope_revision,
				policy_revision_hash,
				delegation_id,
				delegation_revision,
				governance_policy_snapshot_hash,
				status,
				approval_kind,
				quorum_required,
				post_review_required,
				expires_at,
				created_at,
				updated_at
			)
			VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, 'pending', $20, $21, $22, $23, $24, $24)
		`, approvalID.String(), tenantID, checkID.String(), input.RequesterID,
			input.ProductSurface, input.SupervisorUserID, input.ActionType, input.TargetSystem, input.TargetResource, input.ResourceType,
			result.RiskLevel, input.Reason, result.BindingHash,
			input.AuthorityBindingHash, input.ScopeRevision, input.PolicyRevisionHash, input.DelegationID,
			input.DelegationRevision, result.PolicySnapshotHash, approvalKind, quorum, postReview, expiresAt, now); err != nil {
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

func (r *Repository) GetCheckForRevalidation(ctx context.Context, tenantID, checkID string) (domain.RevalidationRecord, error) {
	var record domain.RevalidationRecord
	var decision string
	var functionalRoles, functionalScopes []byte
	err := r.pool.QueryRow(ctx, `SELECT requester_type,requester_id,product_surface,action_type,target_system,target_resource,resource_type,
		membership_role,functional_roles,functional_scopes,binding_hash,authority_binding_hash,scope_revision,policy_revision_hash,
		delegation_id,delegation_revision,decision,risk_level,policy_snapshot_hash
		FROM governance_checks WHERE tenant_id=$1 AND id=$2::uuid`, tenantID, checkID).Scan(
		&record.Input.RequesterType, &record.Input.RequesterID, &record.Input.ProductSurface, &record.Input.ActionType, &record.Input.TargetSystem,
		&record.Input.TargetResource, &record.Input.ResourceType, &record.Input.MembershipRole, &functionalRoles, &functionalScopes,
		&record.Input.BindingHash, &record.Input.AuthorityBindingHash, &record.Input.ScopeRevision, &record.Input.PolicyRevisionHash,
		&record.Input.DelegationID, &record.Input.DelegationRevision, &decision, &record.RiskLevel, &record.PolicySnapshotHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RevalidationRecord{}, domainerr.NotFound("governance check not found")
	}
	if err != nil {
		return domain.RevalidationRecord{}, err
	}
	record.Decision = domain.Decision(decision)
	_ = json.Unmarshal(functionalRoles, &record.Input.FunctionalRoles)
	_ = json.Unmarshal(functionalScopes, &record.Input.FunctionalScopes)
	return record, nil
}

var _ CheckRecorderPort = (*Repository)(nil)
