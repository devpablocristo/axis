package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// ErrTraceNotFound se devuelve cuando un run trace no existe.
var ErrTraceNotFound = errors.New("run trace not found")

// TraceRepository persiste y recupera RunTraces.
type TraceRepository interface {
	Save(ctx context.Context, trace RunTrace, orgID, userID string, taskID *uuid.UUID, errMsg string) error
	GetByID(ctx context.Context, runID uuid.UUID) (StoredTrace, error)
	ListByOrg(ctx context.Context, orgID string, limit int) ([]StoredTrace, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]StoredTrace, error)
}

// StoredTrace es la representación persistida de un RunTrace, incluyendo metadatos de tenancy.
type StoredTrace struct {
	RunTrace
	OrgID  string     `json:"org_id"`
	UserID string     `json:"user_id"`
	TaskID *uuid.UUID `json:"task_id,omitempty"`
	Error  string     `json:"error,omitempty"`
}

// PostgresTraceRepository implementación PostgreSQL del repositorio.
type PostgresTraceRepository struct {
	db *sharedpostgres.DB
}

// NewPostgresTraceRepository crea un nuevo repositorio de run traces.
func NewPostgresTraceRepository(db *sharedpostgres.DB) *PostgresTraceRepository {
	return &PostgresTraceRepository{db: db}
}

const selectRunTrace = `
	SELECT run_id, org_id, user_id, task_id, product_surface, intent, autonomy_level,
	prompt_version, model,
	       identity_chain_json, usage_json, guardrail_events_json, tool_calls_json, error,
	       started_at, completed_at
	FROM companion_run_traces`

// Save persiste un run trace. Idempotente sobre run_id (UPSERT).
func (r *PostgresTraceRepository) Save(ctx context.Context, trace RunTrace, orgID, userID string, taskID *uuid.UUID, errMsg string) error {
	runID, err := uuid.Parse(trace.RunID)
	if err != nil {
		return fmt.Errorf("invalid run_id %q: %w", trace.RunID, err)
	}

	identityJSON, err := json.Marshal(trace.IdentityChain)
	if err != nil {
		return fmt.Errorf("marshal identity_chain: %w", err)
	}
	guardrailJSON, err := json.Marshal(emptyArrayIfNil(trace.GuardrailEvents))
	if err != nil {
		return fmt.Errorf("marshal guardrail_events: %w", err)
	}
	usageJSON, err := json.Marshal(trace.Usage)
	if err != nil {
		return fmt.Errorf("marshal usage: %w", err)
	}
	toolCallsJSON, err := json.Marshal(emptyArrayIfNilTools(trace.ToolCalls))
	if err != nil {
		return fmt.Errorf("marshal tool_calls: %w", err)
	}

	var completedAt *time.Time
	if !trace.CompletedAt.IsZero() {
		c := trace.CompletedAt
		completedAt = &c
	}

	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO companion_run_traces
			(run_id, org_id, user_id, task_id, product_surface, intent, autonomy_level,
			 prompt_version, model, identity_chain_json, usage_json, guardrail_events_json, tool_calls_json, error,
			 started_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (run_id) DO UPDATE SET
			prompt_version        = EXCLUDED.prompt_version,
			model                 = EXCLUDED.model,
			usage_json            = EXCLUDED.usage_json,
			guardrail_events_json = EXCLUDED.guardrail_events_json,
			tool_calls_json       = EXCLUDED.tool_calls_json,
			error                 = EXCLUDED.error,
			completed_at          = EXCLUDED.completed_at
	`, runID, orgID, userID, taskID, trace.ProductSurface, trace.Intent, string(trace.AutonomyLevel),
		firstNonEmpty(trace.PromptVersion, SystemPromptVersion), trace.Model,
		identityJSON, usageJSON, guardrailJSON, toolCallsJSON, errMsg,
		trace.StartedAt, completedAt)
	if err != nil {
		return fmt.Errorf("save run trace: %w", err)
	}
	return nil
}

// GetByID recupera un trace por run_id.
func (r *PostgresTraceRepository) GetByID(ctx context.Context, runID uuid.UUID) (StoredTrace, error) {
	row := r.db.Pool().QueryRow(ctx, selectRunTrace+` WHERE run_id = $1`, runID)
	trace, err := scanRunTrace(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredTrace{}, ErrTraceNotFound
		}
		return StoredTrace{}, fmt.Errorf("get run trace: %w", err)
	}
	return trace, nil
}

// ListByOrg lista traces de un org ordenados por started_at desc.
func (r *PostgresTraceRepository) ListByOrg(ctx context.Context, orgID string, limit int) ([]StoredTrace, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, selectRunTrace+` WHERE org_id = $1 ORDER BY started_at DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("list run traces by org: %w", err)
	}
	defer rows.Close()

	var out []StoredTrace
	for rows.Next() {
		trace, err := scanRunTrace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	return out, rows.Err()
}

// ListByTask lista traces asociados a una task.
func (r *PostgresTraceRepository) ListByTask(ctx context.Context, taskID uuid.UUID) ([]StoredTrace, error) {
	rows, err := r.db.Pool().Query(ctx, selectRunTrace+` WHERE task_id = $1 ORDER BY started_at DESC LIMIT 100`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list run traces by task: %w", err)
	}
	defer rows.Close()

	var out []StoredTrace
	for rows.Next() {
		trace, err := scanRunTrace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunTrace(row rowScanner) (StoredTrace, error) {
	var (
		st            StoredTrace
		runID         uuid.UUID
		taskID        *uuid.UUID
		identityRaw   []byte
		usageRaw      []byte
		guardrailRaw  []byte
		toolCallsRaw  []byte
		autonomyLevel string
		promptVersion string
		model         string
		completedAt   *time.Time
	)
	err := row.Scan(
		&runID, &st.OrgID, &st.UserID, &taskID, &st.ProductSurface, &st.Intent, &autonomyLevel,
		&promptVersion, &model,
		&identityRaw, &usageRaw, &guardrailRaw, &toolCallsRaw, &st.Error,
		&st.StartedAt, &completedAt,
	)
	if err != nil {
		return StoredTrace{}, err
	}
	st.RunID = runID.String()
	st.TaskID = taskID
	st.AutonomyLevel = AutonomyLevel(autonomyLevel)
	st.PromptVersion = promptVersion
	st.Model = model
	if completedAt != nil {
		st.CompletedAt = *completedAt
	}
	if len(identityRaw) > 0 {
		if err := json.Unmarshal(identityRaw, &st.IdentityChain); err != nil {
			return StoredTrace{}, fmt.Errorf("unmarshal identity_chain: %w", err)
		}
	}
	if len(usageRaw) > 0 {
		if err := json.Unmarshal(usageRaw, &st.Usage); err != nil {
			return StoredTrace{}, fmt.Errorf("unmarshal usage: %w", err)
		}
	}
	if len(guardrailRaw) > 0 {
		if err := json.Unmarshal(guardrailRaw, &st.GuardrailEvents); err != nil {
			return StoredTrace{}, fmt.Errorf("unmarshal guardrail_events: %w", err)
		}
	}
	if len(toolCallsRaw) > 0 {
		if err := json.Unmarshal(toolCallsRaw, &st.ToolCalls); err != nil {
			return StoredTrace{}, fmt.Errorf("unmarshal tool_calls: %w", err)
		}
	}
	return st, nil
}

func emptyArrayIfNil(events []GuardrailEvent) []GuardrailEvent {
	if events == nil {
		return []GuardrailEvent{}
	}
	return events
}

func emptyArrayIfNilTools(calls []ToolTrace) []ToolTrace {
	if calls == nil {
		return []ToolTrace{}
	}
	return calls
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
