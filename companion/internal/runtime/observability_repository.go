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
	ListObservabilityEvents(ctx context.Context, filter ObservabilityEventFilter) ([]ObservabilityEvent, error)
	GetRunReplay(ctx context.Context, runID uuid.UUID) (RunReplay, error)
}

type ObservabilityEventFilter struct {
	OrgID          string
	ProductSurface string
	RunID          *uuid.UUID
	EventType      string
	EventName      string
	Severity       string
	CapabilityID   string
	ToolName       string
	AgentID        string
	TaskID         *uuid.UUID
	JobID          *uuid.UUID
	Limit          int
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
	event.ProductSurface = strings.TrimSpace(event.ProductSurface)
	event.EventType = strings.TrimSpace(event.EventType)
	event.EventName = strings.TrimSpace(event.EventName)
	if event.OrgID == "" || event.EventType == "" || event.EventName == "" {
		return nil
	}
	if event.ProductSurface == "" {
		event.ProductSurface = DefaultProductSurface
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
			(org_id, product_surface, run_id, task_id, job_id, agent_id, capability_id, event_type,
			 event_name, severity, trace_id, payload_json, redacted, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, event.OrgID, event.ProductSurface, event.RunID, event.TaskID, event.JobID, event.AgentID, event.CapabilityID,
		event.EventType, event.EventName, event.Severity, event.TraceID, event.Payload, event.Redacted, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("record observability event: %w", err)
	}
	return nil
}

func (r *PostgresObservabilityRepository) RecordCostEvent(ctx context.Context, event CostEvent) error {
	event.OrgID = strings.TrimSpace(event.OrgID)
	event.ProductSurface = strings.TrimSpace(event.ProductSurface)
	event.EventType = strings.TrimSpace(event.EventType)
	if event.OrgID == "" || event.EventType == "" {
		return nil
	}
	if event.ProductSurface == "" {
		event.ProductSurface = DefaultProductSurface
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
			(org_id, product_surface, run_id, task_id, job_id, agent_id, capability_id, model,
			 cost_class, event_type, estimated_tokens, estimated_cost_cents,
			 quantity, payload_json, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	`, event.OrgID, event.ProductSurface, event.RunID, event.TaskID, event.JobID, event.AgentID, event.CapabilityID,
		event.Model, event.CostClass, event.EventType, event.EstimatedTokens,
		event.EstimatedCostCents, event.Quantity, event.Payload, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("record cost event: %w", err)
	}
	return nil
}

func (r *PostgresObservabilityRepository) GetCostSummary(ctx context.Context, orgID, productSurface, period string, limit int) (CostSummary, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
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
	summary := CostSummary{OrgID: orgID, ProductSurface: productSurface, Period: period}
	where := `WHERE org_id = $1 AND occurred_at >= $2 AND occurred_at < $3`
	args := []any{orgID, start, end}
	if productSurface != "" {
		args = append(args, productSurface)
		where += fmt.Sprintf(" AND product_surface = $%d", len(args))
	}
	err = r.db.Pool().QueryRow(ctx, `
		SELECT
			COALESCE(SUM(estimated_tokens), 0),
			COALESCE(SUM(estimated_cost_cents), 0),
			COALESCE(SUM(CASE WHEN event_type = 'run' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'tool' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'job' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'embedding' THEN quantity ELSE 0 END), 0)
		FROM companion_cost_events
		`+where, args...).Scan(&summary.EstimatedTokens, &summary.EstimatedCostCents, &summary.LLMCalls, &summary.ToolCalls, &summary.JobEvents, &summary.EmbeddingEvents)
	if err != nil {
		return CostSummary{}, fmt.Errorf("summarize cost events: %w", err)
	}
	if summary.ByProduct, err = r.listCostBreakdown(ctx, where, args, "product", `COALESCE(NULLIF(product_surface, ''), 'companion')`); err != nil {
		return CostSummary{}, err
	}
	if summary.ByCapability, err = r.listCostBreakdown(ctx, where, args, "capability", `COALESCE(NULLIF(capability_id, ''), 'unattributed')`); err != nil {
		return CostSummary{}, err
	}
	if summary.ByModel, err = r.listCostBreakdown(ctx, where, args, "model", `COALESCE(NULLIF(model, ''), 'unattributed')`); err != nil {
		return CostSummary{}, err
	}
	if summary.ByAgent, err = r.listCostBreakdown(ctx, where, args, "agent", `COALESCE(NULLIF(agent_id, ''), 'unattributed')`); err != nil {
		return CostSummary{}, err
	}
	eventArgs := append([]any(nil), args...)
	eventArgs = append(eventArgs, limit)
	rows, err := r.db.Pool().Query(ctx, selectCostEvent+`
		`+where+fmt.Sprintf(`
		ORDER BY occurred_at DESC
		LIMIT $%d
	`, len(eventArgs)), eventArgs...)
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

func (r *PostgresObservabilityRepository) listCostBreakdown(ctx context.Context, where string, args []any, dimension, groupExpr string) ([]CostBreakdown, error) {
	rows, err := r.db.Pool().Query(ctx, fmt.Sprintf(`
		SELECT
			%s AS key,
			COALESCE(SUM(estimated_tokens), 0),
			COALESCE(SUM(estimated_cost_cents), 0),
			COALESCE(SUM(CASE WHEN event_type = 'run' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'tool' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'job' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_type = 'embedding' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(quantity), 0)
		FROM companion_cost_events
		%s
		GROUP BY %s
		ORDER BY COALESCE(SUM(estimated_cost_cents), 0) DESC, COALESCE(SUM(quantity), 0) DESC
		LIMIT 100
	`, groupExpr, where, groupExpr), args...)
	if err != nil {
		return nil, fmt.Errorf("list %s cost breakdown: %w", dimension, err)
	}
	defer rows.Close()
	out := make([]CostBreakdown, 0)
	for rows.Next() {
		item := CostBreakdown{Dimension: dimension}
		if err := rows.Scan(&item.Key, &item.EstimatedTokens, &item.EstimatedCostCents, &item.LLMCalls, &item.ToolCalls, &item.JobEvents, &item.EmbeddingEvents, &item.Quantity); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PostgresObservabilityRepository) ListObservabilityEvents(ctx context.Context, filter ObservabilityEventFilter) ([]ObservabilityEvent, error) {
	query, args := observabilityEventQuery(filter)
	rows, err := r.db.Pool().Query(ctx, query, args...)
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
	events, err := r.ListObservabilityEvents(ctx, ObservabilityEventFilter{
		OrgID:          trace.OrgID,
		ProductSurface: trace.ProductSurface,
		RunID:          &runID,
		Limit:          500,
	})
	if err != nil {
		return RunReplay{}, err
	}
	return RunReplay{Trace: trace, Events: events}, nil
}

func observabilityEventQuery(filter ObservabilityEventFilter) (string, []any) {
	filter = normalizeObservabilityEventFilter(filter)
	query := selectObservabilityEvent + ` WHERE true`
	args := []any{}
	addStringFilter := func(column, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		query += fmt.Sprintf(` AND %s = $%d`, column, len(args))
	}
	addUUIDFilter := func(column string, value *uuid.UUID) {
		if value == nil || *value == uuid.Nil {
			return
		}
		args = append(args, *value)
		query += fmt.Sprintf(` AND %s = $%d`, column, len(args))
	}
	addUUIDFilter("run_id", filter.RunID)
	addStringFilter("org_id", filter.OrgID)
	addStringFilter("product_surface", filter.ProductSurface)
	addStringFilter("event_type", filter.EventType)
	addStringFilter("event_name", filter.EventName)
	addStringFilter("severity", filter.Severity)
	addStringFilter("capability_id", filter.CapabilityID)
	addStringFilter("agent_id", filter.AgentID)
	addUUIDFilter("task_id", filter.TaskID)
	addUUIDFilter("job_id", filter.JobID)
	args = append(args, filter.Limit)
	if filter.RunID != nil && *filter.RunID != uuid.Nil {
		query += fmt.Sprintf(` ORDER BY occurred_at ASC LIMIT $%d`, len(args))
	} else {
		query += fmt.Sprintf(` ORDER BY occurred_at DESC LIMIT $%d`, len(args))
	}
	return query, args
}

func normalizeObservabilityEventFilter(filter ObservabilityEventFilter) ObservabilityEventFilter {
	filter.OrgID = strings.TrimSpace(filter.OrgID)
	filter.ProductSurface = strings.TrimSpace(filter.ProductSurface)
	filter.EventType = strings.TrimSpace(filter.EventType)
	filter.EventName = strings.TrimSpace(filter.EventName)
	filter.Severity = strings.TrimSpace(filter.Severity)
	filter.CapabilityID = strings.TrimSpace(filter.CapabilityID)
	filter.ToolName = strings.TrimSpace(filter.ToolName)
	filter.AgentID = strings.TrimSpace(filter.AgentID)
	if filter.CapabilityID == "" {
		filter.CapabilityID = filter.ToolName
	}
	if filter.RunID != nil && *filter.RunID == uuid.Nil {
		filter.RunID = nil
	}
	if filter.TaskID != nil && *filter.TaskID == uuid.Nil {
		filter.TaskID = nil
	}
	if filter.JobID != nil && *filter.JobID == uuid.Nil {
		filter.JobID = nil
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	return filter
}

const selectObservabilityEvent = `
	SELECT id, org_id, product_surface, run_id, task_id, job_id, agent_id, capability_id,
	       event_type, event_name, severity, trace_id, payload_json, redacted, occurred_at
	FROM companion_observability_events`

const selectCostEvent = `
	SELECT id, org_id, product_surface, run_id, task_id, job_id, agent_id, capability_id, model,
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
	err := row.Scan(&event.ID, &event.OrgID, &event.ProductSurface, &runID, &taskID, &jobID, &event.AgentID,
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
	err := row.Scan(&event.ID, &event.OrgID, &event.ProductSurface, &runID, &taskID, &jobID, &event.AgentID,
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
