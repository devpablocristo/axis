package approvals

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, tenantID string, status domain.Status, limit int) ([]domain.Approval, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			tenant_id,
			governance_check_id::text,
			requester_id,
			action_type,
			target_system,
			target_resource,
			risk_level,
			reason,
			binding_hash,
			status,
			decided_by,
			decision_note,
			decided_at,
			created_at,
			updated_at
		FROM approvals
		WHERE tenant_id = $1
			AND status = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, tenantID, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Approval{}
	for rows.Next() {
		item, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	return r.get(ctx, tenantID, id)
}

func (r *Repository) Decide(ctx context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID string, note string) (domain.Approval, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		UPDATE approvals
		SET status = $3,
			decided_by = $4,
			decision_note = $5,
			decided_at = $6,
			updated_at = $6
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND status = 'pending'
		RETURNING
			id::text,
			tenant_id,
			governance_check_id::text,
			requester_id,
			action_type,
			target_system,
			target_resource,
			risk_level,
			reason,
			binding_hash,
			status,
			decided_by,
			decision_note,
			decided_at,
			created_at,
			updated_at
	`, tenantID, id.String(), string(status), actorID, note, now)
	item, err := scanApproval(row)
	if err == nil {
		return item, nil
	}
	if !domainerr.IsNotFound(err) {
		return domain.Approval{}, err
	}
	existing, getErr := r.get(ctx, tenantID, id)
	if getErr != nil {
		return domain.Approval{}, getErr
	}
	if existing.Status != domain.StatusPending {
		return domain.Approval{}, domainerr.Conflict("approval is already decided")
	}
	return domain.Approval{}, err
}

func (r *Repository) get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			tenant_id,
			governance_check_id::text,
			requester_id,
			action_type,
			target_system,
			target_resource,
			risk_level,
			reason,
			binding_hash,
			status,
			decided_by,
			decision_note,
			decided_at,
			created_at,
			updated_at
		FROM approvals
		WHERE tenant_id = $1
			AND id = $2::uuid
	`, tenantID, id.String())
	return scanApproval(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApproval(row scanner) (domain.Approval, error) {
	var idText string
	var governanceCheckIDText string
	var status string
	var decidedAt sql.NullTime
	var item domain.Approval
	err := row.Scan(
		&idText,
		&item.TenantID,
		&governanceCheckIDText,
		&item.RequesterID,
		&item.ActionType,
		&item.TargetSystem,
		&item.TargetResource,
		&item.RiskLevel,
		&item.Reason,
		&item.BindingHash,
		&status,
		&item.DecidedBy,
		&item.DecisionNote,
		&decidedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	if err != nil {
		return domain.Approval{}, err
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.Approval{}, err
	}
	governanceCheckID, err := uuid.Parse(governanceCheckIDText)
	if err != nil {
		return domain.Approval{}, err
	}
	item.ID = id
	item.GovernanceCheckID = governanceCheckID
	item.Status = domain.Status(status)
	if decidedAt.Valid {
		item.DecidedAt = &decidedAt.Time
	}
	return item, nil
}

var _ RepositoryPort = (*Repository)(nil)
