package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRuntimeControlsRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRuntimeControlsRepository(db *sharedpostgres.DB) *PostgresRuntimeControlsRepository {
	return &PostgresRuntimeControlsRepository{db: db}
}

func (r *PostgresRuntimeControlsRepository) GetRuntimePolicy(ctx context.Context, orgID string) (TenantRuntimePolicy, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return TenantRuntimePolicy{}, ErrRuntimePolicyNotFound
	}
	row := r.db.Pool().QueryRow(ctx, `
		SELECT org_id, enabled, kill_switch, max_autonomy,
		       allowed_product_surfaces, allowed_models,
		       monthly_token_budget, monthly_tool_call_budget,
		       settings_version, control_plane_json, metadata_json, created_at, updated_at
		FROM companion_tenant_runtime_policies
		WHERE org_id = $1
	`, orgID)
	policy, err := scanRuntimePolicy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TenantRuntimePolicy{}, ErrRuntimePolicyNotFound
		}
		return TenantRuntimePolicy{}, fmt.Errorf("get runtime policy: %w", err)
	}
	return policy, nil
}

func (r *PostgresRuntimeControlsRepository) UpsertRuntimePolicy(ctx context.Context, policy TenantRuntimePolicy) (TenantRuntimePolicy, error) {
	policy = normalizeRuntimePolicy(policy)
	if policy.OrgID == "" {
		return TenantRuntimePolicy{}, fmt.Errorf("org_id is required")
	}
	metadataJSON, err := json.Marshal(policy.Metadata)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("marshal metadata: %w", err)
	}
	controlPlaneJSON, err := json.Marshal(policy.ControlPlane)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("marshal control plane: %w", err)
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("begin runtime policy upsert: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("runtime_policy_rollback_failed", "error", rollbackErr)
		}
	}()
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_tenant_runtime_policies
			(org_id, enabled, kill_switch, max_autonomy,
			 allowed_product_surfaces, allowed_models,
			 monthly_token_budget, monthly_tool_call_budget,
			 settings_version, control_plane_json, metadata_json)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10)
		ON CONFLICT (org_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			kill_switch = EXCLUDED.kill_switch,
			max_autonomy = EXCLUDED.max_autonomy,
			allowed_product_surfaces = EXCLUDED.allowed_product_surfaces,
			allowed_models = EXCLUDED.allowed_models,
			monthly_token_budget = EXCLUDED.monthly_token_budget,
			monthly_tool_call_budget = EXCLUDED.monthly_tool_call_budget,
			settings_version = companion_tenant_runtime_policies.settings_version + 1,
			control_plane_json = EXCLUDED.control_plane_json,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()
		RETURNING org_id, enabled, kill_switch, max_autonomy,
		          allowed_product_surfaces, allowed_models,
		          monthly_token_budget, monthly_tool_call_budget,
		          settings_version, control_plane_json, metadata_json, created_at, updated_at
	`, policy.OrgID, policy.Enabled, policy.KillSwitch, string(policy.MaxAutonomy),
		policy.AllowedProductSurfaces, policy.AllowedModels,
		policy.MonthlyTokenBudget, policy.MonthlyToolCallBudget, controlPlaneJSON, metadataJSON)
	result, err := scanRuntimePolicy(row)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("upsert runtime policy: %w", err)
	}
	policyJSON, err := json.Marshal(result)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("marshal runtime policy audit: %w", err)
	}
	changedBy, reason := runtimePolicyAuditMetadata(policy)
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_runtime_policy_audit
			(org_id, settings_version, changed_by, reason, policy_json)
		VALUES ($1,$2,$3,$4,$5)
	`, result.OrgID, result.SettingsVersion, changedBy, reason, policyJSON)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("insert runtime policy audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("commit runtime policy upsert: %w", err)
	}
	committed = true
	return result, nil
}

func (r *PostgresRuntimeControlsRepository) ListRuntimePolicyAudit(ctx context.Context, orgID string, limit int) ([]RuntimePolicyAuditEntry, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, ErrRuntimePolicyNotFound
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id::text, org_id, settings_version, changed_by, reason, policy_json, created_at
		FROM companion_runtime_policy_audit
		WHERE org_id = $1
		ORDER BY settings_version DESC
		LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runtime policy audit: %w", err)
	}
	defer rows.Close()
	out := make([]RuntimePolicyAuditEntry, 0)
	for rows.Next() {
		var (
			entry     RuntimePolicyAuditEntry
			policyRaw []byte
		)
		if err := rows.Scan(&entry.ID, &entry.OrgID, &entry.SettingsVersion, &entry.ChangedBy, &entry.Reason, &policyRaw, &entry.CreatedAt); err != nil {
			return nil, err
		}
		if len(policyRaw) > 0 {
			if err := json.Unmarshal(policyRaw, &entry.Policy); err != nil {
				return nil, fmt.Errorf("unmarshal runtime policy audit: %w", err)
			}
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PostgresRuntimeControlsRepository) GetRuntimeUsage(ctx context.Context, orgID, period string) (TenantRuntimeUsage, error) {
	orgID = strings.TrimSpace(orgID)
	period = strings.TrimSpace(period)
	if orgID == "" || period == "" {
		return TenantRuntimeUsage{}, ErrRuntimePolicyNotFound
	}
	row := r.db.Pool().QueryRow(ctx, `
		SELECT org_id, period, estimated_tokens, llm_calls, tool_calls, tool_errors, last_run_at, updated_at
		FROM companion_runtime_usage_monthly
		WHERE org_id = $1 AND period = $2
	`, orgID, period)
	usage, err := scanRuntimeUsage(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TenantRuntimeUsage{OrgID: orgID, Period: period}, nil
		}
		return TenantRuntimeUsage{}, fmt.Errorf("get runtime usage: %w", err)
	}
	return usage, nil
}

func (r *PostgresRuntimeControlsRepository) AddRuntimeUsage(ctx context.Context, orgID, period string, usage RunUsage) error {
	orgID = strings.TrimSpace(orgID)
	period = strings.TrimSpace(period)
	if orgID == "" || period == "" {
		return nil
	}
	if usage.EstimatedTotalTokens == 0 && usage.LLMCalls == 0 && usage.ToolCalls == 0 && usage.ToolErrors == 0 {
		return nil
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_runtime_usage_monthly
			(org_id, period, estimated_tokens, llm_calls, tool_calls, tool_errors, last_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (org_id, period) DO UPDATE SET
			estimated_tokens = companion_runtime_usage_monthly.estimated_tokens + EXCLUDED.estimated_tokens,
			llm_calls = companion_runtime_usage_monthly.llm_calls + EXCLUDED.llm_calls,
			tool_calls = companion_runtime_usage_monthly.tool_calls + EXCLUDED.tool_calls,
			tool_errors = companion_runtime_usage_monthly.tool_errors + EXCLUDED.tool_errors,
			last_run_at = now(),
			updated_at = now()
	`, orgID, period, usage.EstimatedTotalTokens, usage.LLMCalls, usage.ToolCalls, usage.ToolErrors)
	if err != nil {
		return fmt.Errorf("record runtime usage: %w", err)
	}
	return nil
}

func scanRuntimePolicy(row rowScanner) (TenantRuntimePolicy, error) {
	var (
		policy          TenantRuntimePolicy
		maxAutonomy     string
		controlPlaneRaw []byte
		metadataRaw     []byte
	)
	err := row.Scan(
		&policy.OrgID, &policy.Enabled, &policy.KillSwitch, &maxAutonomy,
		&policy.AllowedProductSurfaces, &policy.AllowedModels,
		&policy.MonthlyTokenBudget, &policy.MonthlyToolCallBudget,
		&policy.SettingsVersion, &controlPlaneRaw, &metadataRaw, &policy.CreatedAt, &policy.UpdatedAt,
	)
	if err != nil {
		return TenantRuntimePolicy{}, err
	}
	policy.MaxAutonomy = AutonomyLevel(maxAutonomy)
	if len(controlPlaneRaw) > 0 {
		if err := json.Unmarshal(controlPlaneRaw, &policy.ControlPlane); err != nil {
			return TenantRuntimePolicy{}, fmt.Errorf("unmarshal runtime control plane: %w", err)
		}
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &policy.Metadata); err != nil {
			return TenantRuntimePolicy{}, fmt.Errorf("unmarshal runtime policy metadata: %w", err)
		}
	}
	return normalizeRuntimePolicy(policy), nil
}

func runtimePolicyAuditMetadata(policy TenantRuntimePolicy) (string, string) {
	changedBy := "companion.runtime_controls"
	reason := ""
	if policy.Metadata != nil {
		if value, ok := policy.Metadata["changed_by"].(string); ok && strings.TrimSpace(value) != "" {
			changedBy = strings.TrimSpace(value)
		}
		if value, ok := policy.Metadata["change_reason"].(string); ok {
			reason = strings.TrimSpace(value)
		}
	}
	return changedBy, reason
}

func scanRuntimeUsage(row rowScanner) (TenantRuntimeUsage, error) {
	var (
		usage   TenantRuntimeUsage
		lastRun *time.Time
		updated *time.Time
	)
	err := row.Scan(
		&usage.OrgID, &usage.Period, &usage.EstimatedTokens, &usage.LLMCalls,
		&usage.ToolCalls, &usage.ToolErrors, &lastRun, &updated,
	)
	if err != nil {
		return TenantRuntimeUsage{}, err
	}
	if lastRun != nil {
		usage.LastRunAt = *lastRun
	}
	if updated != nil {
		usage.UpdatedAt = *updated
	}
	return usage, nil
}
