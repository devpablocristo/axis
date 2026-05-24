package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		       metadata_json, created_at, updated_at
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
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_tenant_runtime_policies
			(org_id, enabled, kill_switch, max_autonomy,
			 allowed_product_surfaces, allowed_models,
			 monthly_token_budget, monthly_tool_call_budget, metadata_json)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (org_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			kill_switch = EXCLUDED.kill_switch,
			max_autonomy = EXCLUDED.max_autonomy,
			allowed_product_surfaces = EXCLUDED.allowed_product_surfaces,
			allowed_models = EXCLUDED.allowed_models,
			monthly_token_budget = EXCLUDED.monthly_token_budget,
			monthly_tool_call_budget = EXCLUDED.monthly_tool_call_budget,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()
		RETURNING org_id, enabled, kill_switch, max_autonomy,
		          allowed_product_surfaces, allowed_models,
		          monthly_token_budget, monthly_tool_call_budget,
		          metadata_json, created_at, updated_at
	`, policy.OrgID, policy.Enabled, policy.KillSwitch, string(policy.MaxAutonomy),
		policy.AllowedProductSurfaces, policy.AllowedModels,
		policy.MonthlyTokenBudget, policy.MonthlyToolCallBudget, metadataJSON)
	result, err := scanRuntimePolicy(row)
	if err != nil {
		return TenantRuntimePolicy{}, fmt.Errorf("upsert runtime policy: %w", err)
	}
	return result, nil
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
		policy      TenantRuntimePolicy
		maxAutonomy string
		metadataRaw []byte
	)
	err := row.Scan(
		&policy.OrgID, &policy.Enabled, &policy.KillSwitch, &maxAutonomy,
		&policy.AllowedProductSurfaces, &policy.AllowedModels,
		&policy.MonthlyTokenBudget, &policy.MonthlyToolCallBudget,
		&metadataRaw, &policy.CreatedAt, &policy.UpdatedAt,
	)
	if err != nil {
		return TenantRuntimePolicy{}, err
	}
	policy.MaxAutonomy = AutonomyLevel(maxAutonomy)
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &policy.Metadata); err != nil {
			return TenantRuntimePolicy{}, fmt.Errorf("unmarshal runtime policy metadata: %w", err)
		}
	}
	return normalizeRuntimePolicy(policy), nil
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
