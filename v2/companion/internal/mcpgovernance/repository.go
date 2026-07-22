package mcpgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) GetPolicy(ctx context.Context, orgID string) (Policy, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT org_id,enabled,kill_switch,allowed_capabilities,denied_capabilities,
		       capability_kill_switches,max_risk_class,max_calls_per_minute,max_concurrency,
		       product_rules,job_role_rules,version,changed_by,created_at,updated_at
		FROM companion_mcp_policies WHERE org_id=$1
	`, orgID)
	policy, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) || domainerr.IsNotFound(err) {
		return DefaultPolicy(orgID), nil
	}
	return policy, err
}

func (r *Repository) PutPolicy(ctx context.Context, orgID, actorID string, input PutPolicyInput) (Policy, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Policy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "mcp-policy:"+orgID); err != nil {
		return Policy{}, err
	}
	previous, err := policyInTx(ctx, tx, orgID)
	if err != nil {
		return Policy{}, err
	}
	if input.ExpectedVersion != previous.Version {
		return Policy{}, domainerr.Conflict("MCP policy revision changed")
	}
	killSwitches, _ := json.Marshal(input.CapabilityKillSwitches)
	productRules, _ := json.Marshal(input.ProductRules)
	jobRoleRules, _ := json.Marshal(input.JobRoleRules)
	now := time.Now().UTC()
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_mcp_policies (
			org_id,enabled,kill_switch,allowed_capabilities,denied_capabilities,
			capability_kill_switches,max_risk_class,max_calls_per_minute,max_concurrency,
			product_rules,job_role_rules,version,changed_by,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1,$12,$13,$13)
		ON CONFLICT (org_id) DO UPDATE SET
			enabled=EXCLUDED.enabled,kill_switch=EXCLUDED.kill_switch,
			allowed_capabilities=EXCLUDED.allowed_capabilities,denied_capabilities=EXCLUDED.denied_capabilities,
			capability_kill_switches=EXCLUDED.capability_kill_switches,max_risk_class=EXCLUDED.max_risk_class,
			max_calls_per_minute=EXCLUDED.max_calls_per_minute,max_concurrency=EXCLUDED.max_concurrency,
			product_rules=EXCLUDED.product_rules,job_role_rules=EXCLUDED.job_role_rules,
			version=companion_mcp_policies.version+1,changed_by=EXCLUDED.changed_by,updated_at=EXCLUDED.updated_at
		WHERE companion_mcp_policies.version=$14
		RETURNING org_id,enabled,kill_switch,allowed_capabilities,denied_capabilities,
		          capability_kill_switches,max_risk_class,max_calls_per_minute,max_concurrency,
		          product_rules,job_role_rules,version,changed_by,created_at,updated_at
	`, orgID, input.Enabled, input.KillSwitch, input.AllowedCapabilities, input.DeniedCapabilities,
		killSwitches, input.MaxRiskClass, input.MaxCallsPerMinute, input.MaxConcurrency,
		productRules, jobRoleRules, actorID, now, input.ExpectedVersion)
	saved, err := scanPolicy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || domainerr.IsNotFound(err) {
			return Policy{}, domainerr.Conflict("MCP policy revision changed")
		}
		return Policy{}, err
	}
	previousRaw, _ := json.Marshal(previous)
	savedRaw, _ := json.Marshal(saved)
	if _, err := tx.Exec(ctx, `
		INSERT INTO companion_mcp_policy_audit
			(id,org_id,actor_id,previous_version,new_version,previous_policy,new_policy,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, uuid.New(), orgID, actorID, previous.Version, saved.Version, previousRaw, savedRaw, now); err != nil {
		return Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Policy{}, err
	}
	return saved, nil
}

func (r *Repository) ListPolicyAudit(ctx context.Context, orgID string, limit int) ([]PolicyAudit, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id,org_id,actor_id,previous_version,new_version,previous_policy,new_policy,created_at
		FROM companion_mcp_policy_audit WHERE org_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PolicyAudit{}
	for rows.Next() {
		var item PolicyAudit
		var previousRaw, nextRaw []byte
		if err := rows.Scan(&item.ID, &item.OrgID, &item.ActorID, &item.PreviousVersion, &item.NewVersion, &previousRaw, &nextRaw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(previousRaw, &item.PreviousPolicy); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(nextRaw, &item.NewPolicy); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ResolveContext(ctx context.Context, request ContextRequest) (InvocationContext, error) {
	var out InvocationContext
	out.OrgID, out.ActorID, out.ActorRole = request.OrgID, request.ActorID, request.ActorRole
	out.VirployeeID, out.SubjectID, out.CaseID = request.VirployeeID, request.SubjectID, request.CaseID
	out.ProductSurface = strings.ToLower(strings.TrimSpace(request.ProductSurface))
	out.RepositoryGeneration = strings.TrimSpace(request.RepositoryGeneration)
	var subjectKind string
	err := r.pool.QueryRow(ctx, `
		SELECT a.id,a.version,s.kind
		FROM companion_continuity_assignments a
		JOIN companion_work_subjects s ON s.org_id=a.org_id AND s.id=a.subject_id AND s.archived_at IS NULL
		JOIN virployees v ON v.org_id=a.org_id AND v.id=a.virployee_id
		JOIN companion_routing_pools p ON p.org_id=a.org_id AND p.id=a.pool_id AND p.archived_at IS NULL
		JOIN companion_routing_pool_members m ON m.org_id=a.org_id AND m.pool_id=a.pool_id
			AND m.virployee_id=a.virployee_id AND m.enabled=true
		WHERE a.org_id=$1 AND a.subject_id=$2 AND a.virployee_id=$3 AND a.status='active'
		  AND v.archived_at IS NULL AND v.trashed_at IS NULL
		ORDER BY a.updated_at DESC,a.id LIMIT 1
	`, request.OrgID, request.SubjectID, request.VirployeeID).Scan(&out.AssignmentID, &out.AssignmentVersion, &subjectKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvocationContext{}, domainerr.Forbidden("the subject is not assigned to this Virployee")
	}
	if err != nil {
		return InvocationContext{}, err
	}
	if request.CaseID != uuid.Nil {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM companion_assist_cases
			WHERE org_id=$1 AND id=$2 AND subject_id=$3::text AND status IN ('open','needs_human')
		)`, request.OrgID, request.CaseID, request.SubjectID.String()).Scan(&exists); err != nil {
			return InvocationContext{}, err
		}
		if !exists {
			return InvocationContext{}, domainerr.Forbidden("case is not active for the selected subject")
		}
	}
	out.PrincipalID = request.SubjectID.String()
	switch subjectKind {
	case "organization":
		out.PrincipalType = "organization"
	case "team":
		out.PrincipalType = "team"
	case "case":
		out.PrincipalType = "case"
	default:
		out.PrincipalType = "person"
	}
	return out, nil
}

func (r *Repository) ReserveInvocation(ctx context.Context, audit InvocationAudit, maxCalls, maxConcurrency int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "mcp-invoke:"+audit.Context.OrgID); err != nil {
		return err
	}
	var recent, running int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE created_at >= now()-interval '1 minute'),
		       count(*) FILTER (WHERE status='running')
		FROM companion_mcp_invocations WHERE org_id=$1
	`, audit.Context.OrgID).Scan(&recent, &running); err != nil {
		return err
	}
	if recent >= maxCalls {
		return domainerr.Conflict("MCP rate limit exceeded")
	}
	if running >= maxConcurrency {
		return domainerr.Conflict("MCP concurrency limit exceeded")
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO companion_mcp_invocations (
			id,org_id,actor_id,actor_role,virployee_id,subject_id,case_id,assignment_id,assignment_version,
			method,capability_key,capability_version,manifest_hash,policy_version,context_hash,payload_hash,
			idempotency_hash,status,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'running',$18)
		ON CONFLICT DO NOTHING
	`, audit.ID, audit.Context.OrgID, audit.Context.ActorID, audit.Context.ActorRole,
		audit.Context.VirployeeID, audit.Context.SubjectID, nullableUUID(audit.Context.CaseID),
		audit.Context.AssignmentID, audit.Context.AssignmentVersion, audit.Method, audit.CapabilityKey,
		audit.CapabilityVersion, audit.ManifestHash, audit.PolicyVersion, audit.ContextHash,
		audit.PayloadHash, audit.IdempotencyHash, audit.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		prior, replayErr := invocationByIdempotency(ctx, tx, audit)
		if replayErr != nil {
			return replayErr
		}
		if prior.PayloadHash != audit.PayloadHash || prior.ContextHash != audit.ContextHash || prior.ManifestHash != audit.ManifestHash || prior.PolicyVersion != audit.PolicyVersion {
			return domainerr.Conflict("idempotency key was already used with different payload or authority context")
		}
		return &IdempotentReplayError{Prior: prior}
	}
	return tx.Commit(ctx)
}

func invocationByIdempotency(ctx context.Context, tx pgx.Tx, audit InvocationAudit) (InvocationAudit, error) {
	var prior InvocationAudit
	err := tx.QueryRow(ctx, `
		SELECT id,payload_hash,context_hash,manifest_hash,policy_version,status,
		       approval_id,binding_hash,decision_reason,blocked_by,error_code,created_at,completed_at
		FROM companion_mcp_invocations
		WHERE org_id=$1 AND virployee_id=$2 AND capability_key=$3 AND idempotency_hash=$4
		LIMIT 1
	`, audit.Context.OrgID, audit.Context.VirployeeID, audit.CapabilityKey, audit.IdempotencyHash).Scan(
		&prior.ID, &prior.PayloadHash, &prior.ContextHash, &prior.ManifestHash, &prior.PolicyVersion,
		&prior.Status, &prior.ApprovalID, &prior.BindingHash, &prior.DecisionReason, &prior.BlockedBy,
		&prior.ErrorCode, &prior.CreatedAt, &prior.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvocationAudit{}, domainerr.Conflict("MCP invocation uniqueness conflict")
	}
	return prior, err
}

func (r *Repository) SaveInvocationOutcome(ctx context.Context, orgID string, id uuid.UUID, approvalID, bindingHash, decisionReason string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE companion_mcp_invocations
		SET approval_id=$3,binding_hash=$4,decision_reason=$5
		WHERE org_id=$1 AND id=$2 AND status='running'
	`, orgID, id, approvalID, bindingHash, decisionReason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domainerr.Conflict("MCP invocation is not running")
	}
	return nil
}

func (r *Repository) CompleteInvocation(ctx context.Context, orgID string, id uuid.UUID, status, blockedBy, errorCode, resultHash string, durationMS int64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE companion_mcp_invocations SET status=$3,blocked_by=$4,error_code=$5,result_hash=$6,
			duration_ms=$7,completed_at=now()
		WHERE org_id=$1 AND id=$2 AND status='running'
	`, orgID, id, status, blockedBy, errorCode, resultHash, durationMS)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domainerr.Conflict("MCP invocation is not running")
	}
	return nil
}

func (r *Repository) ListInvocations(ctx context.Context, orgID string, virployeeID uuid.UUID, limit int) ([]InvocationAudit, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id,org_id,actor_id,actor_role,virployee_id,subject_id,case_id,assignment_id,assignment_version,
		       method,capability_key,capability_version,manifest_hash,policy_version,context_hash,payload_hash,
		       idempotency_hash,result_hash,status,blocked_by,error_code,approval_id,binding_hash,
		       decision_reason,duration_ms,created_at,completed_at
		FROM companion_mcp_invocations
		WHERE org_id=$1 AND ($2::uuid IS NULL OR virployee_id=$2)
		ORDER BY created_at DESC,id DESC LIMIT $3
	`, orgID, nullableUUID(virployeeID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvocationAudit{}
	for rows.Next() {
		var item InvocationAudit
		var caseID *uuid.UUID
		if err := rows.Scan(&item.ID, &item.Context.OrgID, &item.Context.ActorID, &item.Context.ActorRole,
			&item.Context.VirployeeID, &item.Context.SubjectID, &caseID, &item.Context.AssignmentID,
			&item.Context.AssignmentVersion, &item.Method, &item.CapabilityKey, &item.CapabilityVersion,
			&item.ManifestHash, &item.PolicyVersion, &item.ContextHash, &item.PayloadHash,
			&item.IdempotencyHash, &item.ResultHash, &item.Status, &item.BlockedBy, &item.ErrorCode,
			&item.ApprovalID, &item.BindingHash, &item.DecisionReason,
			&item.DurationMS, &item.CreatedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		if caseID != nil {
			item.Context.CaseID = *caseID
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanPolicy(row rowScanner) (Policy, error) {
	var out Policy
	var switchesRaw, productRaw, roleRaw []byte
	err := row.Scan(&out.OrgID, &out.Enabled, &out.KillSwitch, &out.AllowedCapabilities, &out.DeniedCapabilities,
		&switchesRaw, &out.MaxRiskClass, &out.MaxCallsPerMinute, &out.MaxConcurrency,
		&productRaw, &roleRaw, &out.Version, &out.ChangedBy, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, domainerr.NotFound("MCP policy not found")
	}
	if err != nil {
		return Policy{}, err
	}
	if err := json.Unmarshal(switchesRaw, &out.CapabilityKillSwitches); err != nil {
		return Policy{}, err
	}
	if err := json.Unmarshal(productRaw, &out.ProductRules); err != nil {
		return Policy{}, err
	}
	if err := json.Unmarshal(roleRaw, &out.JobRoleRules); err != nil {
		return Policy{}, err
	}
	return out, nil
}

func policyInTx(ctx context.Context, tx pgx.Tx, orgID string) (Policy, error) {
	row := tx.QueryRow(ctx, `
		SELECT org_id,enabled,kill_switch,allowed_capabilities,denied_capabilities,
		       capability_kill_switches,max_risk_class,max_calls_per_minute,max_concurrency,
		       product_rules,job_role_rules,version,changed_by,created_at,updated_at
		FROM companion_mcp_policies WHERE org_id=$1
	`, orgID)
	policy, err := scanPolicy(row)
	if domainerr.IsNotFound(err) {
		return DefaultPolicy(orgID), nil
	}
	return policy, err
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
