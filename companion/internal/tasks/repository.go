package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

var ErrNotFound = domainerr.NotFound("not found")

// Repository port de persistencia para tareas y entidades relacionadas.
type Repository interface {
	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	GetTaskByID(ctx context.Context, id uuid.UUID) (domain.Task, error)
	// ListTasks devuelve tareas. Si orgID es no-vacío, filtra a nivel SQL
	// por (org_id = orgID OR org_id IS NULL OR org_id = '') para preservar
	// aislamiento multi-tenant. Si orgID es vacío, devuelve todas las tareas.
	ListTasks(ctx context.Context, orgID string, limit int) ([]domain.Task, error)
	UpdateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	ListTasksByStatus(ctx context.Context, status string, limit int) ([]domain.Task, error)
	ListTasksPendingGovernanceSync(ctx context.Context, now time.Time, limit int) ([]domain.Task, error)
	LatestProposeGovernanceRequestID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error)
	GetGovernanceSyncState(ctx context.Context, taskID uuid.UUID) (domain.TaskGovernanceSyncState, error)
	UpsertGovernanceSyncState(ctx context.Context, s domain.TaskGovernanceSyncState) (domain.TaskGovernanceSyncState, error)
	GetExecutionPlan(ctx context.Context, taskID uuid.UUID) (domain.TaskExecutionPlan, error)
	UpsertExecutionPlan(ctx context.Context, plan domain.TaskExecutionPlan) (domain.TaskExecutionPlan, error)
	GetExecutionState(ctx context.Context, taskID uuid.UUID) (domain.TaskExecutionState, error)
	UpsertExecutionState(ctx context.Context, state domain.TaskExecutionState) (domain.TaskExecutionState, error)

	InsertMessage(ctx context.Context, m domain.TaskMessage) (domain.TaskMessage, error)
	ListMessagesByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskMessage, error)

	InsertAction(ctx context.Context, a domain.TaskAction) (domain.TaskAction, error)
	UpdateActionGovernanceResult(ctx context.Context, actionID uuid.UUID, governanceRequestID *uuid.UUID, errMsg string) error
	ListActionsByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskAction, error)

	InsertArtifact(ctx context.Context, ar domain.TaskArtifact) (domain.TaskArtifact, error)
	ListArtifactsByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskArtifact, error)
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectTask = `
	SELECT t.id, t.org_id, t.title, t.goal, t.status, t.priority, t.created_by, t.assigned_to, t.channel, t.summary,
	       t.context_json, rs.last_governance_status, rs.last_checked_at, rs.last_error, t.created_at, t.updated_at, t.closed_at
	FROM companion_tasks t
	LEFT JOIN companion_task_governance_sync_state rs ON rs.task_id = t.id`

func (r *PostgresRepository) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	now := time.Now().UTC()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = domain.TaskStatusNew
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if len(t.ContextJSON) == 0 {
		t.ContextJSON = json.RawMessage(`{}`)
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_tasks (
			id, org_id, title, goal, status, priority, created_by, assigned_to, channel, summary,
			context_json, created_at, updated_at, closed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, t.ID, t.OrgID, t.Title, t.Goal, t.Status, t.Priority, t.CreatedBy, t.AssignedTo, t.Channel, t.Summary,
		t.ContextJSON, t.CreatedAt, t.UpdatedAt, t.ClosedAt)
	if err != nil {
		return domain.Task{}, fmt.Errorf("insert task: %w", err)
	}
	return t, nil
}

func (r *PostgresRepository) GetTaskByID(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	row := r.db.Pool().QueryRow(ctx, selectTask+` WHERE t.id = $1`, id)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

func (r *PostgresRepository) ListTasks(ctx context.Context, orgID string, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows pgx.Rows
		err  error
	)
	if orgID != "" {
		rows, err = r.db.Pool().Query(ctx,
			selectTask+` WHERE t.org_id = $1
				ORDER BY t.updated_at DESC LIMIT $2`,
			orgID, limit)
	} else {
		rows, err = r.db.Pool().Query(ctx,
			selectTask+` ORDER BY t.updated_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	t.UpdatedAt = time.Now().UTC()
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_tasks SET
			title = $2, goal = $3, status = $4, priority = $5,
			created_by = $6, assigned_to = $7, channel = $8, summary = $9,
			context_json = $10, org_id = $11, updated_at = $12, closed_at = $13
		WHERE id = $1
	`, t.ID, t.Title, t.Goal, t.Status, t.Priority, t.CreatedBy, t.AssignedTo, t.Channel, t.Summary,
		t.ContextJSON, t.OrgID, t.UpdatedAt, t.ClosedAt)
	if err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Task{}, ErrNotFound
	}
	return t, nil
}

func (r *PostgresRepository) ListTasksByStatus(ctx context.Context, status string, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, selectTask+` WHERE t.status = $1 ORDER BY t.updated_at ASC LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks by status: %w", err)
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListTasksPendingGovernanceSync(ctx context.Context, now time.Time, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, selectTask+`
		WHERE t.status = $1
		  AND (rs.next_check_at IS NULL OR rs.next_check_at <= $2)
		ORDER BY COALESCE(rs.next_check_at, t.updated_at) ASC, t.updated_at ASC
		LIMIT $3
	`, domain.TaskStatusWaitingForApproval, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks pending governance sync: %w", err)
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) LatestProposeGovernanceRequestID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	var rid uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		SELECT governance_request_id FROM companion_task_actions
		WHERE task_id = $1 AND action_type = $2 AND governance_request_id IS NOT NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, taskID, TaskActionPropose).Scan(&rid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("latest propose governance id: %w", err)
	}
	return rid, nil
}

func (r *PostgresRepository) GetGovernanceSyncState(ctx context.Context, taskID uuid.UUID) (domain.TaskGovernanceSyncState, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT task_id, governance_request_id, last_governance_status, last_governance_http_status,
		       last_checked_at, last_error, consecutive_failures, next_check_at, created_at, updated_at
		FROM companion_task_governance_sync_state
		WHERE task_id = $1
	`, taskID)
	state, err := scanGovernanceSyncState(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskGovernanceSyncState{}, ErrNotFound
		}
		return domain.TaskGovernanceSyncState{}, fmt.Errorf("get governance sync state: %w", err)
	}
	return state, nil
}

func (r *PostgresRepository) UpsertGovernanceSyncState(ctx context.Context, s domain.TaskGovernanceSyncState) (domain.TaskGovernanceSyncState, error) {
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_task_governance_sync_state (
			task_id, governance_request_id, last_governance_status, last_governance_http_status,
			last_checked_at, last_error, consecutive_failures, next_check_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (task_id) DO UPDATE SET
			governance_request_id = EXCLUDED.governance_request_id,
			last_governance_status = EXCLUDED.last_governance_status,
			last_governance_http_status = EXCLUDED.last_governance_http_status,
			last_checked_at = EXCLUDED.last_checked_at,
			last_error = EXCLUDED.last_error,
			consecutive_failures = EXCLUDED.consecutive_failures,
			next_check_at = EXCLUDED.next_check_at,
			updated_at = EXCLUDED.updated_at
	`, s.TaskID, s.GovernanceRequestID, s.LastGovernanceStatus, s.LastGovernanceHTTPStatus,
		s.LastCheckedAt, s.LastError, s.ConsecutiveFailures, s.NextCheckAt, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return domain.TaskGovernanceSyncState{}, fmt.Errorf("upsert governance sync state: %w", err)
	}
	return s, nil
}

func (r *PostgresRepository) GetExecutionPlan(ctx context.Context, taskID uuid.UUID) (domain.TaskExecutionPlan, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT task_id, connector_id, operation, payload, idempotency_key, created_at, updated_at
		FROM companion_task_execution_plans
		WHERE task_id = $1
	`, taskID)
	plan, err := scanExecutionPlan(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskExecutionPlan{}, ErrNotFound
		}
		return domain.TaskExecutionPlan{}, fmt.Errorf("get execution plan: %w", err)
	}
	return plan, nil
}

func (r *PostgresRepository) UpsertExecutionPlan(ctx context.Context, plan domain.TaskExecutionPlan) (domain.TaskExecutionPlan, error) {
	now := time.Now().UTC()
	if len(plan.Payload) == 0 {
		plan.Payload = json.RawMessage(`{}`)
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_task_execution_plans (
			task_id, connector_id, operation, payload, idempotency_key, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (task_id) DO UPDATE SET
			connector_id = EXCLUDED.connector_id,
			operation = EXCLUDED.operation,
			payload = EXCLUDED.payload,
			idempotency_key = EXCLUDED.idempotency_key,
			updated_at = EXCLUDED.updated_at
	`, plan.TaskID, plan.ConnectorID, plan.Operation, plan.Payload, plan.IdempotencyKey, plan.CreatedAt, plan.UpdatedAt)
	if err != nil {
		return domain.TaskExecutionPlan{}, fmt.Errorf("upsert execution plan: %w", err)
	}
	return plan, nil
}

func (r *PostgresRepository) GetExecutionState(ctx context.Context, taskID uuid.UUID) (domain.TaskExecutionState, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT task_id, last_execution_id, last_execution_status, retryable, retry_count,
		       last_error, last_attempted_at, verification_result, created_at, updated_at
		FROM companion_task_execution_state
		WHERE task_id = $1
	`, taskID)
	state, err := scanExecutionState(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskExecutionState{}, ErrNotFound
		}
		return domain.TaskExecutionState{}, fmt.Errorf("get execution state: %w", err)
	}
	return state, nil
}

func (r *PostgresRepository) UpsertExecutionState(ctx context.Context, state domain.TaskExecutionState) (domain.TaskExecutionState, error) {
	now := time.Now().UTC()
	if len(state.VerificationResult.Details) == 0 {
		state.VerificationResult.Details = json.RawMessage(`{}`)
	}
	if state.VerificationResult.CheckedAt.IsZero() {
		state.VerificationResult.CheckedAt = now
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now
	verificationJSON, err := marshalVerificationResult(state.VerificationResult)
	if err != nil {
		return domain.TaskExecutionState{}, fmt.Errorf("marshal verification result: %w", err)
	}
	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO companion_task_execution_state (
			task_id, last_execution_id, last_execution_status, retryable, retry_count,
			last_error, last_attempted_at, verification_result, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (task_id) DO UPDATE SET
			last_execution_id = EXCLUDED.last_execution_id,
			last_execution_status = EXCLUDED.last_execution_status,
			retryable = EXCLUDED.retryable,
			retry_count = EXCLUDED.retry_count,
			last_error = EXCLUDED.last_error,
			last_attempted_at = EXCLUDED.last_attempted_at,
			verification_result = EXCLUDED.verification_result,
			updated_at = EXCLUDED.updated_at
	`, state.TaskID, state.LastExecutionID, state.LastExecutionStatus, state.Retryable, state.RetryCount,
		state.LastError, state.LastAttemptedAt, verificationJSON, state.CreatedAt, state.UpdatedAt)
	if err != nil {
		return domain.TaskExecutionState{}, fmt.Errorf("upsert execution state: %w", err)
	}
	return state, nil
}

func (r *PostgresRepository) InsertMessage(ctx context.Context, m domain.TaskMessage) (domain.TaskMessage, error) {
	now := time.Now().UTC()
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.CreatedAt = now
	if len(m.Metadata) == 0 {
		m.Metadata = json.RawMessage(`{}`)
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_task_messages (id, task_id, author_type, author_id, body, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, m.ID, m.TaskID, m.AuthorType, m.AuthorID, m.Body, m.Metadata, m.CreatedAt)
	if err != nil {
		return domain.TaskMessage{}, fmt.Errorf("insert message: %w", err)
	}
	return m, nil
}

func (r *PostgresRepository) ListMessagesByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskMessage, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, task_id, author_type, author_id, body, metadata, created_at
		FROM companion_task_messages WHERE task_id = $1 ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) InsertAction(ctx context.Context, a domain.TaskAction) (domain.TaskAction, error) {
	now := time.Now().UTC()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	a.CreatedAt = now
	if len(a.Payload) == 0 {
		a.Payload = json.RawMessage(`{}`)
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_task_actions (id, task_id, action_type, payload, governance_request_id, error_message, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, a.ID, a.TaskID, a.ActionType, a.Payload, a.GovernanceRequestID, nullIfEmpty(a.ErrorMessage), a.CreatedAt)
	if err != nil {
		return domain.TaskAction{}, fmt.Errorf("insert action: %w", err)
	}
	return a, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *PostgresRepository) UpdateActionGovernanceResult(ctx context.Context, actionID uuid.UUID, governanceRequestID *uuid.UUID, errMsg string) error {
	var rid any
	if governanceRequestID != nil {
		rid = *governanceRequestID
	}
	var em any
	if errMsg != "" {
		em = errMsg
	}
	_, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_task_actions SET governance_request_id = $2, error_message = $3 WHERE id = $1
	`, actionID, rid, em)
	if err != nil {
		return fmt.Errorf("update action: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListActionsByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskAction, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, task_id, action_type, payload, governance_request_id, error_message, created_at
		FROM companion_task_actions WHERE task_id = $1 ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskAction
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) InsertArtifact(ctx context.Context, ar domain.TaskArtifact) (domain.TaskArtifact, error) {
	now := time.Now().UTC()
	if ar.ID == uuid.Nil {
		ar.ID = uuid.New()
	}
	if len(ar.Payload) == 0 {
		ar.Payload = json.RawMessage(`{}`)
	}
	ar.CreatedAt = now
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_task_artifacts (id, task_id, kind, uri, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, ar.ID, ar.TaskID, ar.Kind, ar.URI, ar.Payload, ar.CreatedAt)
	if err != nil {
		return domain.TaskArtifact{}, fmt.Errorf("insert artifact: %w", err)
	}
	return ar, nil
}

func (r *PostgresRepository) ListArtifactsByTaskID(ctx context.Context, taskID uuid.UUID) ([]domain.TaskArtifact, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, task_id, kind, uri, payload, created_at
		FROM companion_task_artifacts WHERE task_id = $1 ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskArtifact
	for rows.Next() {
		var ar domain.TaskArtifact
		if err := rows.Scan(&ar.ID, &ar.TaskID, &ar.Kind, &ar.URI, &ar.Payload, &ar.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ar)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (domain.Task, error) {
	var t domain.Task
	var governanceStatus *string
	var governanceLastChecked *time.Time
	var governanceErr *string
	var closed *time.Time
	err := row.Scan(
		&t.ID, &t.OrgID, &t.Title, &t.Goal, &t.Status, &t.Priority, &t.CreatedBy, &t.AssignedTo, &t.Channel, &t.Summary,
		&t.ContextJSON, &governanceStatus, &governanceLastChecked, &governanceErr, &t.CreatedAt, &t.UpdatedAt, &closed,
	)
	if err != nil {
		return domain.Task{}, err
	}
	if governanceStatus != nil {
		t.GovernanceStatus = *governanceStatus
	}
	t.GovernanceLastCheckedAt = governanceLastChecked
	if governanceErr != nil {
		t.GovernanceSyncError = *governanceErr
	}
	t.ClosedAt = closed
	return t, nil
}

func scanMessage(row rowScanner) (domain.TaskMessage, error) {
	var m domain.TaskMessage
	err := row.Scan(&m.ID, &m.TaskID, &m.AuthorType, &m.AuthorID, &m.Body, &m.Metadata, &m.CreatedAt)
	return m, err
}

func scanAction(row rowScanner) (domain.TaskAction, error) {
	var a domain.TaskAction
	var rid *uuid.UUID
	var errMsg *string
	err := row.Scan(&a.ID, &a.TaskID, &a.ActionType, &a.Payload, &rid, &errMsg, &a.CreatedAt)
	if err != nil {
		return domain.TaskAction{}, err
	}
	a.GovernanceRequestID = rid
	if errMsg != nil {
		a.ErrorMessage = *errMsg
	}
	return a, nil
}

func scanGovernanceSyncState(row rowScanner) (domain.TaskGovernanceSyncState, error) {
	var s domain.TaskGovernanceSyncState
	err := row.Scan(
		&s.TaskID,
		&s.GovernanceRequestID,
		&s.LastGovernanceStatus,
		&s.LastGovernanceHTTPStatus,
		&s.LastCheckedAt,
		&s.LastError,
		&s.ConsecutiveFailures,
		&s.NextCheckAt,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return domain.TaskGovernanceSyncState{}, err
	}
	return s, nil
}

func scanExecutionPlan(row rowScanner) (domain.TaskExecutionPlan, error) {
	var plan domain.TaskExecutionPlan
	var payloadRaw []byte
	err := row.Scan(
		&plan.TaskID,
		&plan.ConnectorID,
		&plan.Operation,
		&payloadRaw,
		&plan.IdempotencyKey,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}
	if payloadRaw != nil {
		plan.Payload = json.RawMessage(payloadRaw)
	}
	return plan, nil
}

func scanExecutionState(row rowScanner) (domain.TaskExecutionState, error) {
	var state domain.TaskExecutionState
	var verificationRaw []byte
	err := row.Scan(
		&state.TaskID,
		&state.LastExecutionID,
		&state.LastExecutionStatus,
		&state.Retryable,
		&state.RetryCount,
		&state.LastError,
		&state.LastAttemptedAt,
		&verificationRaw,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	if err != nil {
		return domain.TaskExecutionState{}, err
	}
	if len(verificationRaw) > 0 {
		verification, unmarshalErr := unmarshalVerificationResult(verificationRaw)
		if unmarshalErr != nil {
			return domain.TaskExecutionState{}, unmarshalErr
		}
		state.VerificationResult = verification
	}
	return state, nil
}

func marshalVerificationResult(result domain.TaskVerificationResult) ([]byte, error) {
	if len(result.Details) == 0 {
		result.Details = json.RawMessage(`{}`)
	}
	return json.Marshal(map[string]any{
		"status":     result.Status,
		"summary":    result.Summary,
		"checked_at": result.CheckedAt,
		"details":    json.RawMessage(result.Details),
	})
}

func unmarshalVerificationResult(raw []byte) (domain.TaskVerificationResult, error) {
	var payload struct {
		Status    string          `json:"status"`
		Summary   string          `json:"summary"`
		CheckedAt time.Time       `json:"checked_at"`
		Details   json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return domain.TaskVerificationResult{}, fmt.Errorf("unmarshal verification result: %w", err)
	}
	if len(payload.Details) == 0 {
		payload.Details = json.RawMessage(`{}`)
	}
	return domain.TaskVerificationResult{
		Status:    payload.Status,
		Summary:   payload.Summary,
		CheckedAt: payload.CheckedAt,
		Details:   payload.Details,
	}, nil
}
