package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// Repository define el port de persistencia para audit trail (append-only).
type Repository interface {
	Append(ctx context.Context, e auditdomain.RequestEvent) error
	ListByRequestID(ctx context.Context, requestID uuid.UUID) ([]auditdomain.RequestEvent, error)
}

// --- Implementación PostgreSQL ---

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Append(ctx context.Context, e auditdomain.RequestEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO request_events (id, request_id, event_type, actor_type, actor_id, summary, data, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, e.ID, e.RequestID, e.EventType, e.ActorType, e.ActorID, e.Summary, e.Data, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListByRequestID(ctx context.Context, requestID uuid.UUID) ([]auditdomain.RequestEvent, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, request_id, event_type, actor_type, actor_id, summary, data, created_at
		FROM request_events WHERE request_id = $1
		ORDER BY created_at ASC
	`, requestID)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	out := make([]auditdomain.RequestEvent, 0)
	for rows.Next() {
		var e auditdomain.RequestEvent
		if err := rows.Scan(
			&e.ID, &e.RequestID, &e.EventType, &e.ActorType, &e.ActorID, &e.Summary, &e.Data, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Compilar verifica que PostgresRepository implementa Repository.
var _ Repository = (*PostgresRepository)(nil)
