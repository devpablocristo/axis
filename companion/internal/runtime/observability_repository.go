package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type ObservabilityRepository interface {
	ObservabilityRecorder
	ListObservabilityEvents(ctx context.Context, orgID string, runID *uuid.UUID, limit int) ([]ObservabilityEvent, error)
	GetRunReplay(ctx context.Context, runID uuid.UUID) (RunReplay, error)
}

type PostgresObservabilityRepository struct {
	db     *sharedpostgres.DB
	traces *PostgresTraceRepository
}

func NewPostgresObservabilityRepository(db *sharedpostgres.DB, traces *PostgresTraceRepository) *PostgresObservabilityRepository {
	return &PostgresObservabilityRepository{db: db, traces: traces}
}

func (r *PostgresObservabilityRepository) RecordObservabilityEvent(ctx context.Context, event ObservabilityEvent) error {
	event.OrgID = strings.TrimSpace(event.OrgID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.EventName = strings.TrimSpace(event.EventName)
	if event.OrgID == "" || event.EventType == "" || event.EventName == "" {
		return nil
	}
	if event.Severity == "" {
		event.Severity = "info"
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage(`{}`)
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_observability_events
			(org_id, run_id, task_id, job_id, agent_id, capability_id, event_type,
			 event_name, severity, trace_id, payload_json, redacted, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, event.OrgID, event.RunID, event.TaskID, event.JobID, event.AgentID, event.CapabilityID,
		event.EventType, event.EventName, event.Severity, event.TraceID, event.Payload, event.Redacted, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("record observability event: %w", err)
	}
	return nil
}

func (r *PostgresObservabilityRepository) RecordCostEvent(ctx context.Context, event CostEvent) error {
	event.OrgID = strings.TrimSpace(event.OrgID)
	event.EventType = strings.TrimSpace(event.EventType)
	if event.OrgID == "" || event.EventType == "" {
		return nil
	}
	if event.Quantity <= 0 {
		event.Quantity = 1
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage(`{}`)
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_cost_events
			(org_id, run_id, task_id, job_id, agent_id, capability_id, model,
			 cost_class, event_type, estimated_tokens, estimated_cost_cents,
			 quantity, payload_json, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, event.OrgID, event.RunID, event.TaskID, event.JobID, event.AgentID, event.CapabilityID,
		event.Model, event.CostClass, event.EventType, event.EstimatedTokens,
		event.EstimatedCostCents, event.Quantity, event.Payload, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("record cost event: %w", err)
	}
	return nil
}

func (r *PostgresObservabilityRepository) GetCostSummary(ctx context.Context, orgID, period string, limit int) (CostSummary, error) {
	orgID = strings.TrimSpace(orgID)
	period = strings.TrimSpace(period)
	if period == "" {
		period = time.Now().UTC().Format("2006-01")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	start, err := time.Parse("2006-01", period)
	if err != nil {
		return CostSummary{}, fmt.Errorf("invalid period: %w", err)
	}
	end := start.AddDate(0, 1, 0)
	summary := CostSummary{OrgID: orgID, Period: period}
	err = r.db.Pool().QueryRow(ctx, `
		SELECT
			COALESCE(SUM(estimated_tokens), 0),
			COALESCE(SUM(estimated_cost_cents), 0),
			COALESCE(SUM(CASE WHEN event_type = 'run' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'tool' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'job' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'embedding' THEN quantity ELSE 0 END), 0)
		FROM companion_cost_events
		WHERE org_id = $1 AND occurred_at >= $2 AND occurred_at < $3
	`, orgID, start, end).Scan(&summary.EstimatedTokens, &summary.EstimatedCostCents, &summary.LLMCalls, &summary.ToolCalls, &summary.JobEvents, &summary.EmbeddingEvents)
	if err != nil {
		return CostSummary{}, fmt.Errorf("summarize cost events: %w", err)
	}
	rows, err := r.db.Pool().Query(ctx, selectCostEvent+`
		WHERE org_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		ORDER BY occurred_at DESC
		LIMIT $4
	`, orgID, start, end, limit)
	if err != nil {
		return CostSummary{}, fmt.Errorf("list cost events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		event, err := scanCostEvent(rows)
		if err != nil {
			return CostSummary{}, err
		}
		summary.Events = append(summary.Events, event)
	}
	return summary, rows.Err()
}

func (r *PostgresObservabilityRepository) ListObservabilityEvents(ctx context.Context, orgID string, runID *uuid.UUID, limit int) ([]ObservabilityEvent, error) {
	orgID = strings.TrimSpace(orgID)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		rows pgx.Rows
		err  error
	)
	if runID != nil && *runID != uuid.Nil {
		rows, err = r.db.Pool().Query(ctx, selectObservabilityEvent+` WHERE run_id = $1 ORDER BY occurred_at ASC LIMIT $2`, *runID, limit)
	} else {
		rows, err = r.db.Pool().Query(ctx, selectObservabilityEvent+` WHERE org_id = $1 ORDER BY occurred_at DESC LIMIT $2`, orgID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list observability events: %w", err)
	}
	defer rows.Close()
	out := make([]ObservabilityEvent, 0)
	for rows.Next() {
		event, err := scanObservabilityEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgresObservabilityRepository) GetRunReplay(ctx context.Context, runID uuid.UUID) (RunReplay, error) {
	if r.traces == nil {
		return RunReplay{}, ErrTraceNotFound
	}
	trace, err := r.traces.GetByID(ctx, runID)
	if err != nil {
		return RunReplay{}, err
	}
	events, err := r.ListObservabilityEvents(ctx, trace.OrgID, &runID, 500)
	if err != nil {
		return RunReplay{}, err
	}
	return RunReplay{Trace: trace, Events: events}, nil
}

const selectObservabilityEvent = `
	SELECT id, org_id, run_id, task_id, job_id, agent_id, capability_id,
	       event_type, event_name, severity, trace_id, payload_json, redacted, occurred_at
	FROM companion_observability_events`

const selectCostEvent = `
	SELECT id, org_id, run_id, task_id, job_id, agent_id, capability_id, model,
	       cost_class, event_type, estimated_tokens, estimated_cost_cents,
	       quantity, payload_json, occurred_at
	FROM companion_cost_events`

func scanObservabilityEvent(row rowScanner) (ObservabilityEvent, error) {
	var (
		event   ObservabilityEvent
		payload []byte
		runID   *uuid.UUID
		taskID  *uuid.UUID
		jobID   *uuid.UUID
	)
	err := row.Scan(&event.ID, &event.OrgID, &runID, &taskID, &jobID, &event.AgentID,
		&event.CapabilityID, &event.EventType, &event.EventName, &event.Severity,
		&event.TraceID, &payload, &event.Redacted, &event.OccurredAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ObservabilityEvent{}, ErrTraceNotFound
		}
		return ObservabilityEvent{}, err
	}
	event.RunID = runID
	event.TaskID = taskID
	event.JobID = jobID
	event.Payload = json.RawMessage(payload)
	return event, nil
}

func scanCostEvent(row rowScanner) (CostEvent, error) {
	var (
		event   CostEvent
		payload []byte
		runID   *uuid.UUID
		taskID  *uuid.UUID
		jobID   *uuid.UUID
	)
	err := row.Scan(&event.ID, &event.OrgID, &runID, &taskID, &jobID, &event.AgentID,
		&event.CapabilityID, &event.Model, &event.CostClass, &event.EventType,
		&event.EstimatedTokens, &event.EstimatedCostCents, &event.Quantity,
		&payload, &event.OccurredAt)
	if err != nil {
		return CostEvent{}, err
	}
	event.RunID = runID
	event.TaskID = taskID
	event.JobID = jobID
	event.Payload = json.RawMessage(payload)
	return event, nil
}
