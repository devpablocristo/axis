package governancepolicies

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) CreateArtifact(ctx context.Context, item Artifact) (Artifact, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO governance_policy_artifacts
		(id,tenant_id,policy_key,name,description,created_by,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`, item.ID, item.TenantID, item.PolicyKey, item.Name, item.Description, item.CreatedBy, item.CreatedAt)
	if err != nil {
		return Artifact{}, err
	}
	_ = r.appendChange(ctx, item.TenantID, item.ID, nil, item.CreatedBy, "artifact.created", "policy artifact created", map[string]any{"policy_key": item.PolicyKey})
	return item, nil
}

func (r *Repository) ListArtifacts(ctx context.Context, tenantID string) ([]Artifact, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,policy_key,name,description,created_by,created_at,updated_at
		FROM governance_policy_artifacts WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Artifact, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetArtifact(ctx context.Context, tenantID string, id uuid.UUID) (Artifact, error) {
	item, err := scanArtifact(r.pool.QueryRow(ctx, `SELECT id,tenant_id,policy_key,name,description,created_by,created_at,updated_at
		FROM governance_policy_artifacts WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, domainerr.NotFound("governance policy not found")
	}
	if err != nil {
		return Artifact{}, err
	}
	versions, err := r.ListVersions(ctx, tenantID, id)
	if err != nil {
		return Artifact{}, err
	}
	item.Versions = versions
	return item, nil
}

func (r *Repository) CreateVersion(ctx context.Context, tenantID string, policyID uuid.UUID, actorID string, in CreateVersionInput, contentHash string, now time.Time) (Version, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedPolicyID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM governance_policy_artifacts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, policyID).Scan(&lockedPolicyID); errors.Is(err, pgx.ErrNoRows) {
		return Version{}, domainerr.NotFound("governance policy not found")
	} else if err != nil {
		return Version{}, err
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM governance_policy_versions
		WHERE tenant_id=$1 AND policy_id=$2`, tenantID, policyID).Scan(&version); err != nil {
		return Version{}, err
	}
	item := Version{ID: uuid.New(), TenantID: tenantID, PolicyID: policyID, Version: version, State: StateDraft,
		ProductSurface: in.ProductSurface, ActionTypePattern: in.ActionTypePattern, TargetSystem: in.TargetSystem,
		RequesterType: in.RequesterType, Expression: in.Expression, Effect: in.Effect, RiskOverride: in.RiskOverride,
		Priority: in.Priority, ContentHash: contentHash, CreatedBy: actorID, CreatedAt: now}
	var risk any
	if item.RiskOverride != "" {
		risk = item.RiskOverride
	}
	if _, err := tx.Exec(ctx, `INSERT INTO governance_policy_versions
		(id,tenant_id,policy_id,version,state,product_surface,action_type_pattern,target_system,requester_type,expression,effect,risk_override,priority,content_hash,created_by,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, item.ID, item.TenantID, item.PolicyID, item.Version, item.State,
		item.ProductSurface, item.ActionTypePattern, item.TargetSystem, item.RequesterType, item.Expression, item.Effect, risk, item.Priority, item.ContentHash, item.CreatedBy, item.CreatedAt); err != nil {
		return Version{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE governance_policy_artifacts SET updated_at=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, policyID, now); err != nil {
		return Version{}, err
	}
	if err := insertChange(ctx, tx, tenantID, policyID, &item.ID, actorID, "version.created", "immutable policy version created", map[string]any{"version": item.Version, "content_hash": item.ContentHash}); err != nil {
		return Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, err
	}
	return item, nil
}

func (r *Repository) ListVersions(ctx context.Context, tenantID string, policyID uuid.UUID) ([]Version, error) {
	rows, err := r.pool.Query(ctx, versionSelect+` WHERE tenant_id=$1 AND policy_id=$2 ORDER BY version DESC`, tenantID, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		item, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetVersion(ctx context.Context, tenantID string, id uuid.UUID) (Version, error) {
	item, err := scanVersion(r.pool.QueryRow(ctx, versionSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, domainerr.NotFound("governance policy version not found")
	}
	return item, err
}

func (r *Repository) ListEvaluatable(ctx context.Context, tenantID string) ([]Version, error) {
	rows, err := r.pool.Query(ctx, versionSelect+` WHERE tenant_id=$1 AND state IN ('shadow','active') ORDER BY priority,id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		item, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) HistoricalInputs(ctx context.Context, tenantID string, limit int) ([]SafeInput, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `SELECT action_type,target_system,target_resource,risk_level,requester_id,
		authority_binding_hash,policy_revision_hash,created_at FROM governance_checks
		WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SafeInput, 0)
	for rows.Next() {
		var in SafeInput
		var authorityBindingHash, policyRevisionHash string
		if err := rows.Scan(&in.ActionType, &in.TargetSystem, &in.ResourceReference, &in.RiskClass, &in.RequesterID, &authorityBindingHash, &policyRevisionHash, &in.Now); err != nil {
			return nil, err
		}
		in.RequesterType = "virployee"
		in.AuthorityHashes = map[string]string{"authority_binding_hash": authorityBindingHash, "professional_policy_hash": policyRevisionHash}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (r *Repository) CreateSimulation(ctx context.Context, item Simulation) (Simulation, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO governance_policy_simulations
		(id,tenant_id,policy_version_id,requested_by,total_evaluated,would_match,would_allow,would_deny,would_require_approval,report_hash,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, item.ID, item.TenantID, item.PolicyVersionID, item.RequestedBy,
		item.TotalEvaluated, item.WouldMatch, item.WouldAllow, item.WouldDeny, item.WouldRequireApproval, item.ReportHash, item.CreatedAt)
	return item, err
}

func (r *Repository) GetSimulation(ctx context.Context, tenantID string, id uuid.UUID) (Simulation, error) {
	var item Simulation
	err := r.pool.QueryRow(ctx, `SELECT id,tenant_id,policy_version_id,requested_by,total_evaluated,would_match,would_allow,would_deny,would_require_approval,report_hash,created_at
		FROM governance_policy_simulations WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&item.ID, &item.TenantID, &item.PolicyVersionID, &item.RequestedBy,
		&item.TotalEvaluated, &item.WouldMatch, &item.WouldAllow, &item.WouldDeny, &item.WouldRequireApproval, &item.ReportHash, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Simulation{}, domainerr.NotFound("policy simulation not found")
	}
	return item, err
}

func (r *Repository) CreatePromotion(ctx context.Context, item Promotion) (Promotion, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO governance_policy_promotions
		(id,tenant_id,policy_version_id,simulation_id,target_state,status,requested_by,created_at)
		VALUES ($1,$2,$3,$4,$5,'pending',$6,$7)`, item.ID, item.TenantID, item.PolicyVersionID, item.SimulationID, item.TargetState, item.RequestedBy, item.CreatedAt)
	return item, err
}

func (r *Repository) GetPromotion(ctx context.Context, tenantID string, id uuid.UUID) (Promotion, error) {
	var item Promotion
	var decidedAt *time.Time
	err := r.pool.QueryRow(ctx, `SELECT id,tenant_id,policy_version_id,simulation_id,target_state,status,requested_by,decided_by,decision_reason,created_at,decided_at
		FROM governance_policy_promotions WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&item.ID, &item.TenantID, &item.PolicyVersionID, &item.SimulationID,
		&item.TargetState, &item.Status, &item.RequestedBy, &item.DecidedBy, &item.DecisionReason, &item.CreatedAt, &decidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Promotion{}, domainerr.NotFound("policy promotion not found")
	}
	item.DecidedAt = decidedAt
	return item, err
}

func (r *Repository) ListPromotions(ctx context.Context, tenantID string, limit int) ([]Promotion, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,policy_version_id,simulation_id,target_state,status,requested_by,decided_by,decision_reason,created_at,decided_at
		FROM governance_policy_promotions WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Promotion, 0)
	for rows.Next() {
		var item Promotion
		if err := rows.Scan(&item.ID, &item.TenantID, &item.PolicyVersionID, &item.SimulationID, &item.TargetState, &item.Status,
			&item.RequestedBy, &item.DecidedBy, &item.DecisionReason, &item.CreatedAt, &item.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) DecidePromotion(ctx context.Context, tenantID string, id uuid.UUID, actorID, decision, reason string, now time.Time) (Promotion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Promotion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Promotion
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,policy_version_id,simulation_id,target_state,status,requested_by,created_at
		FROM governance_policy_promotions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&item.ID, &item.TenantID, &item.PolicyVersionID,
		&item.SimulationID, &item.TargetState, &item.Status, &item.RequestedBy, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Promotion{}, domainerr.NotFound("policy promotion not found")
	}
	if err != nil {
		return Promotion{}, err
	}
	if item.Status != "pending" {
		return Promotion{}, domainerr.Conflict("policy promotion is already decided")
	}
	var version Version
	version, err = scanVersion(tx.QueryRow(ctx, versionSelect+` WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, item.PolicyVersionID))
	if err != nil {
		return Promotion{}, err
	}
	// Serialize promotions for every version of the same artifact. Locking only the
	// selected version would allow two concurrent approvals to leave two active
	// versions behind.
	var lockedPolicyID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM governance_policy_artifacts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, version.PolicyID).Scan(&lockedPolicyID); err != nil {
		return Promotion{}, err
	}
	if decision == "approved" {
		if item.TargetState == StateShadow && version.State != StateDraft {
			return Promotion{}, domainerr.Conflict("only draft versions can enter shadow")
		}
		if item.TargetState == StateActive && version.State != StateShadow && version.State != StateRetired {
			return Promotion{}, domainerr.Conflict("only shadow or retired versions can become active")
		}
		if item.TargetState == StateActive {
			if _, err := tx.Exec(ctx, `UPDATE governance_policy_versions SET state='retired',retired_at=$4
				WHERE tenant_id=$1 AND policy_id=$2 AND state='active' AND id<>$3`, tenantID, version.PolicyID, version.ID, now); err != nil {
				return Promotion{}, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE governance_policy_versions SET state=$3,retired_at=NULL WHERE tenant_id=$1 AND id=$2`, tenantID, version.ID, item.TargetState); err != nil {
			return Promotion{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE governance_policy_promotions SET status=$3,decided_by=$4,decision_reason=$5,decided_at=$6 WHERE tenant_id=$1 AND id=$2`, tenantID, id, decision, actorID, reason, now); err != nil {
		return Promotion{}, err
	}
	if err := insertChange(ctx, tx, tenantID, version.PolicyID, &version.ID, actorID, "promotion."+decision, "policy promotion "+decision, map[string]any{"target_state": item.TargetState, "promotion_id": item.ID}); err != nil {
		return Promotion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Promotion{}, err
	}
	item.Status, item.DecidedBy, item.DecisionReason, item.DecidedAt = decision, actorID, reason, &now
	return item, nil
}

func (r *Repository) RecordEvaluation(ctx context.Context, item Evaluation) error {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO governance_policy_evaluations
		(id,tenant_id,policy_version_id,mode,matched,effect,decision,error_code,input_hash,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, item.ID, item.TenantID, item.PolicyVersionID, item.Mode, item.Matched,
		item.Effect, item.Decision, item.ErrorCode, item.InputHash, item.CreatedAt)
	return err
}

func (r *Repository) ListEvaluations(ctx context.Context, tenantID string, limit int) ([]Evaluation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,policy_version_id,mode,matched,effect,decision,error_code,input_hash,created_at
		FROM governance_policy_evaluations WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Evaluation, 0)
	for rows.Next() {
		var item Evaluation
		if err := rows.Scan(&item.ID, &item.TenantID, &item.PolicyVersionID, &item.Mode, &item.Matched, &item.Effect, &item.Decision, &item.ErrorCode, &item.InputHash, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ListChanges(ctx context.Context, tenantID string, limit int) ([]Change, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,policy_id,policy_version_id,actor_id,action,summary,data,created_at
		FROM governance_policy_changelog WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Change, 0)
	for rows.Next() {
		var item Change
		var raw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.PolicyID, &item.PolicyVersionID, &item.ActorID, &item.Action, &item.Summary, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Data); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) appendChange(ctx context.Context, tenantID string, policyID uuid.UUID, versionID *uuid.UUID, actorID, action, summary string, data map[string]any) error {
	return insertChange(ctx, r.pool, tenantID, policyID, versionID, actorID, action, summary, data)
}

// changeExecer keeps changelog writes usable with both pgx.Tx and pgxpool.Pool.
type changeExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertChange(ctx context.Context, exec changeExecer, tenantID string, policyID uuid.UUID, versionID *uuid.UUID, actorID, action, summary string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = exec.Exec(ctx, `INSERT INTO governance_policy_changelog
		(id,tenant_id,policy_id,policy_version_id,actor_id,action,summary,data,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.New(), tenantID, policyID, versionID, actorID, action, summary, raw, time.Now().UTC())
	return err
}

const versionSelect = `SELECT id,tenant_id,policy_id,version,state,product_surface,action_type_pattern,target_system,requester_type,
	expression,effect,COALESCE(risk_override,''),priority,content_hash,created_by,created_at,retired_at FROM governance_policy_versions`

type scanner interface{ Scan(...any) error }

func scanArtifact(row scanner) (Artifact, error) {
	var item Artifact
	err := row.Scan(&item.ID, &item.TenantID, &item.PolicyKey, &item.Name, &item.Description, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanVersion(row scanner) (Version, error) {
	var item Version
	err := row.Scan(&item.ID, &item.TenantID, &item.PolicyID, &item.Version, &item.State, &item.ProductSurface, &item.ActionTypePattern,
		&item.TargetSystem, &item.RequesterType, &item.Expression, &item.Effect, &item.RiskOverride, &item.Priority, &item.ContentHash,
		&item.CreatedBy, &item.CreatedAt, &item.RetiredAt)
	return item, err
}
