package agentfleet

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

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) ListAgents(ctx context.Context, orgID, productSurface string) ([]Agent, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = defaultSurface(productSurface)
	query := `
		SELECT org_id, product_surface, agent_id, display_name, role, profile_id, status,
		       lifecycle_status, origin_kind, review_status,
		       max_autonomy, allowed_tools, allowed_capabilities,
		       memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
		       version, created_by, created_at, updated_at
		FROM companion_agents
		WHERE product_surface = $2 AND ($1 = '*' OR org_id = $1)
		ORDER BY agent_id
	`
	rows, err := r.db.Pool().Query(ctx, query, orgID, productSurface)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agents rows: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) GetAgent(ctx context.Context, orgID, productSurface, agentID string) (Agent, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT org_id, product_surface, agent_id, display_name, role, profile_id, status,
		       lifecycle_status, origin_kind, review_status,
		       max_autonomy, allowed_tools, allowed_capabilities,
		       memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
		       version, created_by, created_at, updated_at
		FROM companion_agents
		WHERE org_id = $1 AND product_surface = $2 AND agent_id = $3
	`, strings.TrimSpace(orgID), defaultSurface(productSurface), strings.TrimSpace(agentID))
	agent, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		return Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return agent, nil
}

func (r *PostgresRepository) SaveAgent(ctx context.Context, agent Agent) (Agent, error) {
	agent = normalizeAgent(agent)
	sharedMemory, limits, sla, metadata, err := marshalAgentJSON(agent)
	if err != nil {
		return Agent{}, err
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Agent{}, fmt.Errorf("begin save agent: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("agent_fleet_rollback_failed", "error", rollbackErr)
		}
	}()
	now := time.Now().UTC()
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_agents
			(org_id, product_surface, agent_id, display_name, role, profile_id, status,
			 lifecycle_status, origin_kind, review_status,
			 max_autonomy, allowed_tools, allowed_capabilities,
			 memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
			 version, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,1,$19,$20,$20)
		ON CONFLICT (org_id, product_surface, agent_id)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			role = EXCLUDED.role,
			profile_id = EXCLUDED.profile_id,
			status = EXCLUDED.status,
			lifecycle_status = EXCLUDED.lifecycle_status,
			origin_kind = EXCLUDED.origin_kind,
			review_status = EXCLUDED.review_status,
			max_autonomy = EXCLUDED.max_autonomy,
			allowed_tools = EXCLUDED.allowed_tools,
			allowed_capabilities = EXCLUDED.allowed_capabilities,
			memory_scope_id = EXCLUDED.memory_scope_id,
			shared_memory_policy = EXCLUDED.shared_memory_policy,
			limits_json = EXCLUDED.limits_json,
			sla_json = EXCLUDED.sla_json,
			metadata_json = EXCLUDED.metadata_json,
			version = companion_agents.version + 1,
			updated_at = EXCLUDED.updated_at
		RETURNING org_id, product_surface, agent_id, display_name, role, profile_id, status,
		          lifecycle_status, origin_kind, review_status,
		          max_autonomy, allowed_tools, allowed_capabilities,
		          memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
		          version, created_by, created_at, updated_at
	`, agent.OrgID, agent.ProductSurface, agent.AgentID, agent.DisplayName, agent.Role, agent.ProfileID,
		agent.Status, agent.LifecycleStatus, agent.OriginKind, agent.ReviewStatus,
		agent.MaxAutonomy, agent.AllowedTools, agent.AllowedCapabilities,
		agent.MemoryScopeID, sharedMemory, limits, sla, metadata, agent.CreatedBy, now)
	saved, err := scanAgent(row)
	if err != nil {
		return Agent{}, fmt.Errorf("save agent: %w", err)
	}
	auditPayload, _ := json.Marshal(saved)
	if err := insertAgentAudit(ctx, tx, saved.OrgID, saved.ProductSurface, saved.AgentID, "save", agent.CreatedBy, auditPayload); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, fmt.Errorf("commit save agent: %w", err)
	}
	committed = true
	return saved, nil
}

func (r *PostgresRepository) DisableAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Agent{}, fmt.Errorf("begin disable agent: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("agent_fleet_rollback_failed", "error", rollbackErr)
		}
	}()
	row := tx.QueryRow(ctx, `
		UPDATE companion_agents
		SET status = 'disabled', lifecycle_status = 'archived', version = version + 1, updated_at = now()
		WHERE org_id = $1 AND product_surface = $2 AND agent_id = $3
		RETURNING org_id, product_surface, agent_id, display_name, role, profile_id, status,
		          lifecycle_status, origin_kind, review_status,
		          max_autonomy, allowed_tools, allowed_capabilities,
		          memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
		          version, created_by, created_at, updated_at
	`, strings.TrimSpace(orgID), defaultSurface(productSurface), strings.TrimSpace(agentID))
	agent, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		return Agent{}, fmt.Errorf("disable agent: %w", err)
	}
	payload, _ := json.Marshal(agent)
	if err := insertAgentAudit(ctx, tx, agent.OrgID, agent.ProductSurface, agent.AgentID, "disable", changedBy, payload); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, fmt.Errorf("commit disable agent: %w", err)
	}
	committed = true
	return agent, nil
}

func (r *PostgresRepository) SetAgentLifecycle(ctx context.Context, orgID, productSurface, agentID, lifecycleStatus, status, reviewStatus, changedBy string) (Agent, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Agent{}, fmt.Errorf("begin set agent lifecycle: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("agent_lifecycle_rollback_failed", "error", rollbackErr)
		}
	}()
	row := tx.QueryRow(ctx, `
		UPDATE companion_agents
		SET lifecycle_status = COALESCE(NULLIF($4, ''), lifecycle_status),
		    status = COALESCE(NULLIF($5, ''), status),
		    review_status = COALESCE(NULLIF($6, ''), review_status),
		    version = version + 1,
		    updated_at = now()
		WHERE org_id = $1 AND product_surface = $2 AND agent_id = $3
		RETURNING org_id, product_surface, agent_id, display_name, role, profile_id, status,
		          lifecycle_status, origin_kind, review_status,
		          max_autonomy, allowed_tools, allowed_capabilities,
		          memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
		          version, created_by, created_at, updated_at
	`, strings.TrimSpace(orgID), defaultSurface(productSurface), strings.TrimSpace(agentID), strings.TrimSpace(lifecycleStatus), strings.TrimSpace(status), strings.TrimSpace(reviewStatus))
	agent, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		return Agent{}, fmt.Errorf("set agent lifecycle: %w", err)
	}
	payload, _ := json.Marshal(agent)
	action := "lifecycle_" + strings.TrimSpace(agent.LifecycleStatus)
	if strings.TrimSpace(reviewStatus) != "" {
		action = "review_" + strings.TrimSpace(agent.ReviewStatus)
	}
	if err := insertAgentAudit(ctx, tx, agent.OrgID, agent.ProductSurface, agent.AgentID, action, changedBy, payload); err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, fmt.Errorf("commit set agent lifecycle: %w", err)
	}
	committed = true
	return agent, nil
}

func (r *PostgresRepository) DeleteAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) error {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete agent: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("agent_delete_rollback_failed", "error", rollbackErr)
		}
	}()
	agent, err := scanAgent(tx.QueryRow(ctx, `
		SELECT org_id, product_surface, agent_id, display_name, role, profile_id, status,
		       lifecycle_status, origin_kind, review_status,
		       max_autonomy, allowed_tools, allowed_capabilities,
		       memory_scope_id, shared_memory_policy, limits_json, sla_json, metadata_json,
		       version, created_by, created_at, updated_at
		FROM companion_agents
		WHERE org_id = $1 AND product_surface = $2 AND agent_id = $3
	`, strings.TrimSpace(orgID), defaultSurface(productSurface), strings.TrimSpace(agentID)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("load agent for delete: %w", err)
	}
	payload, _ := json.Marshal(agent)
	if err := insertAgentAudit(ctx, tx, agent.OrgID, agent.ProductSurface, agent.AgentID, "delete", changedBy, payload); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM companion_agents
		WHERE org_id = $1 AND product_surface = $2 AND agent_id = $3
	`, agent.OrgID, agent.ProductSurface, agent.AgentID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete agent: %w", err)
	}
	committed = true
	return nil
}

func (r *PostgresRepository) CreateHandoff(ctx context.Context, handoff Handoff) (Handoff, error) {
	handoff = normalizeHandoff(handoff)
	raw, err := json.Marshal(handoff.Context)
	if err != nil {
		return Handoff{}, fmt.Errorf("marshal handoff context: %w", err)
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Handoff{}, fmt.Errorf("begin create handoff: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("agent_handoff_rollback_failed", "error", rollbackErr)
		}
	}()
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_agent_handoffs
			(org_id, product_surface, task_id, from_agent_id, to_agent_id, status, reason, context_json, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id::text, org_id, product_surface, task_id, from_agent_id, to_agent_id,
		          status, reason, context_json, created_by, created_at, updated_at
	`, handoff.OrgID, handoff.ProductSurface, handoff.TaskID, handoff.FromAgentID, handoff.ToAgentID,
		handoff.Status, handoff.Reason, raw, handoff.CreatedBy)
	saved, err := scanHandoff(row)
	if err != nil {
		return Handoff{}, fmt.Errorf("create handoff: %w", err)
	}
	if err := insertAgentAudit(ctx, tx, saved.OrgID, saved.ProductSurface, saved.FromAgentID, "handoff_create", handoff.CreatedBy, raw); err != nil {
		return Handoff{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Handoff{}, fmt.Errorf("commit create handoff: %w", err)
	}
	committed = true
	return saved, nil
}

func (r *PostgresRepository) ListHandoffs(ctx context.Context, orgID, productSurface string, limit int) ([]Handoff, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id::text, org_id, product_surface, task_id, from_agent_id, to_agent_id,
		       status, reason, context_json, created_by, created_at, updated_at
		FROM companion_agent_handoffs
		WHERE org_id = $1 AND product_surface = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, strings.TrimSpace(orgID), defaultSurface(productSurface), limit)
	if err != nil {
		return nil, fmt.Errorf("list handoffs: %w", err)
	}
	defer rows.Close()
	var out []Handoff
	for rows.Next() {
		handoff, err := scanHandoff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, handoff)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list handoffs rows: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) UpdateHandoffStatus(ctx context.Context, orgID, productSurface, handoffID, status, changedBy string) (Handoff, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Handoff{}, fmt.Errorf("begin update handoff: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("agent_handoff_rollback_failed", "error", rollbackErr)
		}
	}()
	row := tx.QueryRow(ctx, `
		UPDATE companion_agent_handoffs
		SET status = $4, updated_at = now()
		WHERE org_id = $1 AND product_surface = $2 AND id = $3::uuid
		RETURNING id::text, org_id, product_surface, task_id, from_agent_id, to_agent_id,
		          status, reason, context_json, created_by, created_at, updated_at
	`, strings.TrimSpace(orgID), defaultSurface(productSurface), strings.TrimSpace(handoffID), strings.TrimSpace(status))
	handoff, err := scanHandoff(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Handoff{}, ErrNotFound
		}
		return Handoff{}, fmt.Errorf("update handoff: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"handoff_id": handoff.ID, "status": handoff.Status})
	if err := insertAgentAudit(ctx, tx, handoff.OrgID, handoff.ProductSurface, handoff.ToAgentID, "handoff_"+handoff.Status, changedBy, payload); err != nil {
		return Handoff{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Handoff{}, fmt.Errorf("commit update handoff: %w", err)
	}
	committed = true
	return handoff, nil
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanAgent(row scanRow) (Agent, error) {
	var (
		agent                        Agent
		sharedRaw, limitsRaw, slaRaw []byte
		metadataRaw                  []byte
		createdAt, updatedAt         time.Time
		allowedTools                 []string
		allowedCapabilities          []string
	)
	if err := row.Scan(&agent.OrgID, &agent.ProductSurface, &agent.AgentID, &agent.DisplayName, &agent.Role, &agent.ProfileID,
		&agent.Status, &agent.LifecycleStatus, &agent.OriginKind, &agent.ReviewStatus,
		&agent.MaxAutonomy, &allowedTools, &allowedCapabilities,
		&agent.MemoryScopeID, &sharedRaw, &limitsRaw, &slaRaw, &metadataRaw, &agent.Version, &agent.CreatedBy,
		&createdAt, &updatedAt); err != nil {
		return Agent{}, err
	}
	agent.AllowedTools = normalizeList(allowedTools)
	agent.AllowedCapabilities = normalizeList(allowedCapabilities)
	agent.SharedMemoryPolicy = unmarshalMap(sharedRaw)
	agent.Limits = unmarshalMap(limitsRaw)
	agent.SLA = unmarshalMap(slaRaw)
	agent.Metadata = unmarshalMap(metadataRaw)
	agent.CreatedAt = createdAt
	agent.UpdatedAt = updatedAt
	return normalizeAgent(agent), nil
}

func scanHandoff(row scanRow) (Handoff, error) {
	var (
		handoff              Handoff
		raw                  []byte
		createdAt, updatedAt time.Time
	)
	if err := row.Scan(&handoff.ID, &handoff.OrgID, &handoff.ProductSurface, &handoff.TaskID,
		&handoff.FromAgentID, &handoff.ToAgentID, &handoff.Status, &handoff.Reason,
		&raw, &handoff.CreatedBy, &createdAt, &updatedAt); err != nil {
		return Handoff{}, err
	}
	handoff.Context = unmarshalMap(raw)
	handoff.CreatedAt = createdAt
	handoff.UpdatedAt = updatedAt
	return normalizeHandoff(handoff), nil
}

func marshalAgentJSON(agent Agent) (json.RawMessage, json.RawMessage, json.RawMessage, json.RawMessage, error) {
	sharedMemory, err := json.Marshal(agent.SharedMemoryPolicy)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal shared memory policy: %w", err)
	}
	limits, err := json.Marshal(agent.Limits)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal agent limits: %w", err)
	}
	sla, err := json.Marshal(agent.SLA)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal agent sla: %w", err)
	}
	metadata, err := json.Marshal(agent.Metadata)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal agent metadata: %w", err)
	}
	return sharedMemory, limits, sla, metadata, nil
}

func unmarshalMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func insertAgentAudit(ctx context.Context, tx pgx.Tx, orgID, productSurface, agentID, action, changedBy string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO companion_agent_audit
			(org_id, product_surface, agent_id, action, changed_by, payload_json)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, orgID, productSurface, agentID, action, changedBy, payload)
	if err != nil {
		return fmt.Errorf("insert agent audit: %w", err)
	}
	return nil
}

func defaultSurface(surface string) string {
	surface = strings.TrimSpace(surface)
	if surface == "" {
		return "companion"
	}
	return surface
}
