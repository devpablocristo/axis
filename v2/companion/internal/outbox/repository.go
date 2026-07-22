package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const messageColumns = `
    id, tenant_id, aggregate_type, aggregate_id, kind, dedupe_key, payload_json,
    status, attempts, max_attempts, available_at, lease_owner, lease_until,
    heartbeat_at, last_error_code, created_at, updated_at, delivered_at`

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) EnqueueTx(ctx context.Context, tx pgx.Tx, input EnqueueInput) (Message, bool, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.AggregateType = strings.TrimSpace(input.AggregateType)
	input.Kind = strings.TrimSpace(input.Kind)
	input.DedupeKey = strings.TrimSpace(input.DedupeKey)
	if input.ID == uuid.Nil {
		input.ID = uuid.New()
	}
	if input.TenantID == "" || input.AggregateID == uuid.Nil || !validMessageType(input.AggregateType, input.Kind) || input.DedupeKey == "" || !json.Valid(input.Payload) {
		return Message{}, false, fmt.Errorf("invalid Nexus outbox message")
	}
	if input.Kind == KindAuditEvent {
		if _, err := ParseNexusAuditEvent(input.Payload, input.AggregateID); err != nil {
			return Message{}, false, fmt.Errorf("invalid Nexus outbox audit payload: %w", err)
		}
	}
	message, err := scanMessage(tx.QueryRow(ctx, `
        INSERT INTO companion_nexus_outbox
            (id, tenant_id, aggregate_type, aggregate_id, kind, dedupe_key, payload_json)
        VALUES ($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT (tenant_id, kind, dedupe_key) DO NOTHING
        RETURNING `+messageColumns,
		input.ID, input.TenantID, input.AggregateType, input.AggregateID, input.Kind, input.DedupeKey, input.Payload))
	inserted := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		message, err = scanMessage(tx.QueryRow(ctx, `
            SELECT `+messageColumns+`
            FROM companion_nexus_outbox
            WHERE tenant_id=$1 AND kind=$2 AND dedupe_key=$3
        `, input.TenantID, input.Kind, input.DedupeKey))
	}
	if err != nil {
		return Message{}, false, fmt.Errorf("enqueue Nexus outbox message: %w", err)
	}
	if !inserted && (message.AggregateType != input.AggregateType || message.AggregateID != input.AggregateID || message.Kind != input.Kind || !sameJSON(message.Payload, input.Payload)) {
		return Message{}, false, fmt.Errorf("nexus outbox dedupe key conflicts with a different message")
	}
	if inserted {
		if err := recordEvent(ctx, tx, message.ID, "pending", json.RawMessage(`{}`)); err != nil {
			return Message{}, false, err
		}
	}
	return message, inserted, nil
}

func (r *Repository) Claim(ctx context.Context, options ClaimOptions) ([]Message, error) {
	options.WorkerID = strings.TrimSpace(options.WorkerID)
	if options.WorkerID == "" {
		return nil, fmt.Errorf("outbox worker id is required")
	}
	if options.Batch <= 0 {
		options.Batch = 1
	}
	if options.Lease <= 0 {
		options.Lease = 30 * time.Second
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim outbox: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
        WITH picked AS (
            SELECT id
            FROM companion_nexus_outbox
            WHERE status='pending' AND attempts < max_attempts AND available_at <= now()
            ORDER BY available_at, created_at, id
            LIMIT $2
            FOR UPDATE SKIP LOCKED
        )
        UPDATE companion_nexus_outbox AS message
        SET status='processing', attempts=attempts+1, lease_owner=$1,
            lease_until=now()+make_interval(secs => $3), heartbeat_at=now(), updated_at=now()
        FROM picked
        WHERE message.id=picked.id
        RETURNING `+prefixedColumns("message"),
		options.WorkerID, options.Batch, durationSeconds(options.Lease))
	if err != nil {
		return nil, fmt.Errorf("claim outbox: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0, options.Batch)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	metadata, _ := json.Marshal(map[string]any{"worker_id": options.WorkerID})
	for _, message := range messages {
		if err := recordEvent(ctx, tx, message.ID, "claimed", metadata); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim outbox: %w", err)
	}
	return messages, nil
}

func (r *Repository) Heartbeat(ctx context.Context, id uuid.UUID, workerID string, lease time.Duration) error {
	tag, err := r.pool.Exec(ctx, `
        UPDATE companion_nexus_outbox
        SET heartbeat_at=now(), lease_until=now()+make_interval(secs => $3), updated_at=now()
        WHERE id=$1 AND lease_owner=$2 AND status='processing'
    `, id, strings.TrimSpace(workerID), durationSeconds(lease))
	if err != nil {
		return fmt.Errorf("heartbeat outbox: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMessageNotFound
	}
	return nil
}

func (r *Repository) MarkDelivered(ctx context.Context, id uuid.UUID, workerID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deliver outbox: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var aggregateID uuid.UUID
	var tenantID string
	var aggregateType string
	var kind string
	var attempts int
	err = tx.QueryRow(ctx, `
        UPDATE companion_nexus_outbox
        SET status='delivered', lease_owner='', lease_until=NULL, last_error_code='',
            delivered_at=now(), updated_at=now()
        WHERE id=$1 AND lease_owner=$2 AND status='processing'
		RETURNING aggregate_id, tenant_id, aggregate_type, kind, attempts
	`, id, strings.TrimSpace(workerID)).Scan(&aggregateID, &tenantID, &aggregateType, &kind, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("mark outbox delivered: %w", err)
	}
	if aggregateType == AggregateTypeExecutionAttempt && kind == KindExecutionResult {
		if _, err := tx.Exec(ctx, `
        UPDATE companion_execution_attempts
        SET nexus_report_status='reported', nexus_report_attempts=$2,
            report_lease_owner='', report_lease_until=NULL, last_watcher_error='', updated_at=now()
		WHERE id=$1 AND tenant_id=$3
	`, aggregateID, attempts, tenantID); err != nil {
			return fmt.Errorf("project delivered outbox: %w", err)
		}
	}
	if err := recordEvent(ctx, tx, id, "delivered", json.RawMessage(`{}`)); err != nil {
		return err
	}
	return commit(ctx, tx, "deliver outbox")
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, workerID, errorCode string, retryable bool, backoff time.Duration) (Message, error) {
	errorCode = normalizeErrorCode(errorCode)
	if backoff <= 0 {
		backoff = time.Second
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("begin fail outbox: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := scanMessage(tx.QueryRow(ctx, `
        UPDATE companion_nexus_outbox
        SET status=CASE WHEN $4 AND attempts < max_attempts THEN 'pending' ELSE 'dead' END,
            available_at=CASE WHEN $4 AND attempts < max_attempts
                              THEN now()+make_interval(secs => $5) ELSE available_at END,
            lease_owner='', lease_until=NULL, last_error_code=$3, updated_at=now()
        WHERE id=$1 AND lease_owner=$2 AND status='processing'
        RETURNING `+messageColumns,
		id, strings.TrimSpace(workerID), errorCode, retryable, durationSeconds(backoff)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("mark outbox failed: %w", err)
	}
	projection := "failed"
	event := "retry_scheduled"
	if message.Status == StatusDead {
		projection = "dead"
		event = "dead"
	}
	if message.AggregateType == AggregateTypeExecutionAttempt && message.Kind == KindExecutionResult {
		if _, err := tx.Exec(ctx, `
        UPDATE companion_execution_attempts
        SET nexus_report_status=$2, nexus_report_attempts=$3, nexus_report_next_at=$4,
            report_lease_owner='', report_lease_until=NULL, last_watcher_error=$5, updated_at=now()
		WHERE id=$1 AND tenant_id=$6
	`, message.AggregateID, projection, message.Attempts, message.AvailableAt, errorCode, message.TenantID); err != nil {
			return Message{}, fmt.Errorf("project failed outbox: %w", err)
		}
	}
	metadata, _ := json.Marshal(map[string]any{"attempt": message.Attempts, "error_code": errorCode})
	if err := recordEvent(ctx, tx, message.ID, event, metadata); err != nil {
		return Message{}, err
	}
	if err := commit(ctx, tx, "fail outbox"); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (r *Repository) RecoverExpiredLeases(ctx context.Context, limit int) (RecoveryResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("begin recover outbox: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
        WITH picked AS (
            SELECT id
            FROM companion_nexus_outbox
            WHERE status='processing' AND lease_until < now()
            ORDER BY lease_until, id
            LIMIT $1
            FOR UPDATE SKIP LOCKED
        )
        UPDATE companion_nexus_outbox AS message
        SET status=CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'dead' END,
            available_at=CASE WHEN attempts < max_attempts THEN now() ELSE available_at END,
            lease_owner='', lease_until=NULL, heartbeat_at=NULL,
            last_error_code='lease_expired', updated_at=now()
        FROM picked
        WHERE message.id=picked.id
		RETURNING message.id, message.tenant_id, message.aggregate_type, message.kind, message.aggregate_id, message.status, message.attempts, message.available_at
    `, limit)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("recover outbox leases: %w", err)
	}
	type recovered struct {
		id            uuid.UUID
		tenantID      string
		aggregateType string
		kind          string
		aggregateID   uuid.UUID
		status        Status
		attempts      int
		availableAt   time.Time
	}
	items := make([]recovered, 0)
	var result RecoveryResult
	for rows.Next() {
		var item recovered
		if err := rows.Scan(&item.id, &item.tenantID, &item.aggregateType, &item.kind, &item.aggregateID, &item.status, &item.attempts, &item.availableAt); err != nil {
			return RecoveryResult{}, err
		}
		if item.status == StatusPending {
			result.Pending++
		} else {
			result.Dead++
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RecoveryResult{}, err
	}
	rows.Close()
	for _, item := range items {
		projection, event := "failed", "lease_recovered"
		if item.status == StatusDead {
			projection, event = "dead", "dead"
		}
		if item.aggregateType == AggregateTypeExecutionAttempt && item.kind == KindExecutionResult {
			if _, err := tx.Exec(ctx, `
            UPDATE companion_execution_attempts
            SET nexus_report_status=$2, nexus_report_attempts=$3, nexus_report_next_at=$4,
                last_watcher_error='lease_expired', updated_at=now()
			WHERE id=$1 AND tenant_id=$5
		`, item.aggregateID, projection, item.attempts, item.availableAt, item.tenantID); err != nil {
				return RecoveryResult{}, err
			}
		}
		if err := recordEvent(ctx, tx, item.id, event, json.RawMessage(`{"error_code":"lease_expired"}`)); err != nil {
			return RecoveryResult{}, err
		}
	}
	if err := commit(ctx, tx, "recover outbox"); err != nil {
		return RecoveryResult{}, err
	}
	return result, nil
}

func (r *Repository) Replay(ctx context.Context, tenantID string, id uuid.UUID, availableAt time.Time) (Message, error) {
	if availableAt.IsZero() {
		availableAt = time.Now().UTC()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("begin replay outbox: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := scanMessage(tx.QueryRow(ctx, `
        UPDATE companion_nexus_outbox
        SET status='pending', attempts=0, available_at=$3, lease_owner='', lease_until=NULL,
            heartbeat_at=NULL, last_error_code='', delivered_at=NULL, updated_at=now()
        WHERE tenant_id=$1 AND id=$2 AND status='dead'
        RETURNING `+messageColumns,
		strings.TrimSpace(tenantID), id, availableAt.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("replay outbox: %w", err)
	}
	if message.AggregateType == AggregateTypeExecutionAttempt && message.Kind == KindExecutionResult {
		if _, err := tx.Exec(ctx, `
        UPDATE companion_execution_attempts
        SET nexus_report_status='pending', nexus_report_attempts=0, nexus_report_next_at=$2,
            last_watcher_error='', updated_at=now()
		WHERE id=$1 AND tenant_id=$3
	`, message.AggregateID, message.AvailableAt, message.TenantID); err != nil {
			return Message{}, err
		}
	}
	if err := recordEvent(ctx, tx, id, "replayed", json.RawMessage(`{}`)); err != nil {
		return Message{}, err
	}
	if err := commit(ctx, tx, "replay outbox"); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (Message, error) {
	message, err := scanMessage(r.pool.QueryRow(ctx, `
        SELECT `+messageColumns+` FROM companion_nexus_outbox WHERE tenant_id=$1 AND id=$2
    `, strings.TrimSpace(tenantID), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	return message, err
}

type rowScanner interface{ Scan(...any) error }

func scanMessage(row rowScanner) (Message, error) {
	var message Message
	var payload []byte
	var status string
	err := row.Scan(
		&message.ID, &message.TenantID, &message.AggregateType, &message.AggregateID,
		&message.Kind, &message.DedupeKey, &payload, &status, &message.Attempts,
		&message.MaxAttempts, &message.AvailableAt, &message.LeaseOwner, &message.LeaseUntil,
		&message.HeartbeatAt, &message.LastErrorCode, &message.CreatedAt, &message.UpdatedAt,
		&message.DeliveredAt,
	)
	message.Payload = json.RawMessage(payload)
	message.Status = Status(status)
	return message, err
}

func recordEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID, event string, metadata json.RawMessage) error {
	if len(metadata) == 0 || !json.Valid(metadata) {
		metadata = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `
        INSERT INTO companion_nexus_outbox_events (outbox_id, event, metadata_json)
        VALUES ($1,$2,$3)
    `, id, event, metadata)
	if err != nil {
		return fmt.Errorf("record outbox event: %w", err)
	}
	return nil
}

func commit(ctx context.Context, tx pgx.Tx, action string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", action, err)
	}
	return nil
}

func durationSeconds(value time.Duration) int {
	seconds := int(value / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func prefixedColumns(prefix string) string {
	columns := strings.Split(strings.ReplaceAll(messageColumns, "\n", " "), ",")
	for index, column := range columns {
		columns[index] = prefix + "." + strings.TrimSpace(column)
	}
	return strings.Join(columns, ", ")
}

func validMessageType(aggregateType, kind string) bool {
	return (aggregateType == AggregateTypeExecutionAttempt && kind == KindExecutionResult) ||
		(aggregateType == AggregateTypeProfessionalAuthority && kind == KindAuditEvent)
}

func sameJSON(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}

var _ RepositoryPort = (*Repository)(nil)
