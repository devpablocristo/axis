package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
)

type Recorder interface {
	Record(ctx context.Context, event Event) error
	RecordTx(ctx context.Context, tx TxExecutor, event Event) error
}

type TxExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Append(ctx context.Context, entry lifecycle.ArchiveAudit) error {
	// Platform lifecycle v0.2.0 uses this port after the resource mutation has
	// already happened. Keep the prior best-effort behavior for that legacy port;
	// new Workforce modules use Record/RecordTx when audit must be transactional.
	_ = r.Record(ctx, Event{
		AuditEventID:    entry.ID,
		TenantID:        entry.TenantID,
		ResourceType:    entry.ResourceType,
		ResourceID:      entry.ResourceID,
		Action:          string(entry.Action),
		OccurredAt:      entry.OccurredAt,
		ActorUserID:     entry.Actor,
		Reason:          entry.Reason,
		BatchID:         entry.BatchID,
		RetentionExpiry: entry.RetentionExpires,
	})
	return nil
}

func (r *PostgresRepository) Record(ctx context.Context, event Event) error {
	normalized, err := normalizeEvent(event)
	if err != nil {
		return err
	}
	_, err = r.db.Pool().Exec(ctx, insertAuditSQL,
		normalized.AuditEventID,
		normalized.TenantID,
		normalized.ResourceType,
		normalized.ResourceID,
		normalized.Action,
		normalized.OccurredAt,
		normalized.ActorUserID,
		normalized.Reason,
		normalized.BatchID,
		normalized.RetentionExpiry,
	)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func (r *PostgresRepository) RecordTx(ctx context.Context, tx TxExecutor, event Event) error {
	normalized, err := normalizeEvent(event)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertAuditSQL,
		normalized.AuditEventID,
		normalized.TenantID,
		normalized.ResourceType,
		normalized.ResourceID,
		normalized.Action,
		normalized.OccurredAt,
		normalized.ActorUserID,
		normalized.Reason,
		normalized.BatchID,
		normalized.RetentionExpiry,
	); err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func (r *PostgresRepository) List(ctx context.Context, filter Filter) ([]Event, error) {
	filter.TenantID = strings.TrimSpace(filter.TenantID)
	filter.ResourceType = strings.TrimSpace(filter.ResourceType)
	if filter.TenantID == "" {
		return nil, ErrValidation
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	query := `
		SELECT id, tenant_id, resource_type, resource_id, action, occurred_at,
		       actor, reason, batch_id, retention_expires
		FROM lifecycle_audit
		WHERE tenant_id = $1`
	args := []any{filter.TenantID}
	if filter.ResourceType != "" {
		args = append(args, filter.ResourceType)
		query += fmt.Sprintf(" AND resource_type = $%d", len(args))
	}
	if filter.ResourceID != uuid.Nil {
		args = append(args, filter.ResourceID)
		query += fmt.Sprintf(" AND resource_id = $%d", len(args))
	}
	args = append(args, filter.Limit)
	query += fmt.Sprintf(" ORDER BY occurred_at DESC LIMIT $%d", len(args))

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.AuditEventID,
			&event.TenantID,
			&event.ResourceType,
			&event.ResourceID,
			&event.Action,
			&event.OccurredAt,
			&event.ActorUserID,
			&event.Reason,
			&event.BatchID,
			&event.RetentionExpiry,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

const insertAuditSQL = `
	INSERT INTO lifecycle_audit (
		id, tenant_id, resource_type, resource_id, action, occurred_at,
		actor, reason, batch_id, retention_expires
	)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	ON CONFLICT (id) DO NOTHING`

var _ Recorder = (*PostgresRepository)(nil)
