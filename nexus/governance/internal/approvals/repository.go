package approvals

import (
	"context"
	"encoding/json"
	"errors"

	"fmt"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	approvaldomain "github.com/devpablocristo/nexus/governance/internal/approvals/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

// Sentinel errors
var (
	ErrNotFound       = domainerr.NotFound("not found")
	ErrNotPending     = domainerr.Conflict("approval is not pending")
	ErrAlreadyDecided = domainerr.Conflict("approver already decided on this approval")
	ErrExpired        = domainerr.Conflict("approval is expired")
	ErrActorRequired  = domainerr.Conflict("approval actor is required")
)

// Repository define el port de persistencia para approvals.
type Repository interface {
	Create(ctx context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error)
	GetByID(ctx context.Context, id uuid.UUID) (approvaldomain.Approval, error)
	GetByRequestID(ctx context.Context, requestID uuid.UUID) (*approvaldomain.Approval, error)
	// ListPending devuelve approvals pendientes con filtro de tenancy aplicado
	// en SQL (ver semántica de orgID/allowAll en requests.Repository.List).
	ListPending(ctx context.Context, limit int, orgID *string, allowAll bool) ([]approvaldomain.Approval, error)
	Update(ctx context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error)
}

// --- Implementación PostgreSQL ---

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(ctx context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	decisionsJSON, jsonErr := json.Marshal(a.Decisions)
	if jsonErr != nil {
		return approvaldomain.Approval{}, fmt.Errorf("marshal decisions: %w", jsonErr)
	}
	if a.Decisions == nil {
		decisionsJSON = []byte("[]")
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO approvals (id, org_id, request_id, status, decided_by, decision_note, decided_at, expires_at, created_at, break_glass, required_approvals, decisions)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, a.ID, a.OrgID, a.RequestID, a.Status, a.DecidedBy, a.DecisionNote, a.DecidedAt, a.ExpiresAt, a.CreatedAt, a.BreakGlass, a.RequiredApprovals, decisionsJSON)
	if err != nil {
		return approvaldomain.Approval{}, fmt.Errorf("insert approval: %w", err)
	}
	return a, nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id uuid.UUID) (approvaldomain.Approval, error) {
	row := r.db.Pool().QueryRow(ctx, selectApprovalSQL+` WHERE id = $1`, id)
	a, err := scanApproval(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return approvaldomain.Approval{}, ErrNotFound
		}
		return approvaldomain.Approval{}, err
	}
	return a, nil
}

func (r *PostgresRepository) GetByRequestID(ctx context.Context, requestID uuid.UUID) (*approvaldomain.Approval, error) {
	row := r.db.Pool().QueryRow(ctx, selectApprovalSQL+` WHERE request_id = $1`, requestID)
	a, err := scanApproval(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *PostgresRepository) ListPending(ctx context.Context, limit int, orgID *string, allowAll bool) ([]approvaldomain.Approval, error) {
	// Expira en query time: excluir approvals vencidos. Tenancy filter en SQL
	// para evitar el bug "post-filter después del LIMIT".
	query := selectApprovalSQL + ` WHERE status = 'pending' AND expires_at > now()`
	args := []any{}
	argN := 1
	if !allowAll {
		if orgID != nil {
			query += fmt.Sprintf(` AND org_id = $%d`, argN)
			args = append(args, *orgID)
			argN++
		} else {
			query += ` AND FALSE`
		}
	}
	query += ` ORDER BY created_at ASC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT $%d`, argN)
		args = append(args, limit)
	}
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals: %w", err)
	}
	defer rows.Close()

	out := make([]approvaldomain.Approval, 0)
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) Update(ctx context.Context, a approvaldomain.Approval) (approvaldomain.Approval, error) {
	decisionsJSON, jsonErr := json.Marshal(a.Decisions)
	if jsonErr != nil {
		return approvaldomain.Approval{}, fmt.Errorf("marshal decisions: %w", jsonErr)
	}
	if a.Decisions == nil {
		decisionsJSON = []byte("[]")
	}
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE approvals SET status = $2, decided_by = $3, decision_note = $4, decided_at = $5, decisions = $6
		WHERE id = $1
	`, a.ID, a.Status, a.DecidedBy, a.DecisionNote, a.DecidedAt, decisionsJSON)
	if err != nil {
		return approvaldomain.Approval{}, fmt.Errorf("update approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return approvaldomain.Approval{}, ErrNotFound
	}
	return a, nil
}

// --- Scanner ---

const selectApprovalSQL = `SELECT id, org_id, request_id, status, decided_by, decision_note, decided_at, expires_at, created_at, break_glass, required_approvals, decisions FROM approvals`

type approvalScanRow interface {
	Scan(dest ...any) error
}

func scanApproval(row approvalScanRow) (approvaldomain.Approval, error) {
	var a approvaldomain.Approval
	var decisionsJSON []byte
	if err := row.Scan(
		&a.ID, &a.OrgID, &a.RequestID, &a.Status, &a.DecidedBy, &a.DecisionNote, &a.DecidedAt, &a.ExpiresAt, &a.CreatedAt,
		&a.BreakGlass, &a.RequiredApprovals, &decisionsJSON,
	); err != nil {
		return approvaldomain.Approval{}, fmt.Errorf("scan approval: %w", err)
	}
	if len(decisionsJSON) > 0 {
		if err := json.Unmarshal(decisionsJSON, &a.Decisions); err != nil {
			return approvaldomain.Approval{}, fmt.Errorf("unmarshal decisions: %w", err)
		}
	}
	if a.RequiredApprovals == 0 {
		a.RequiredApprovals = 1
	}
	return a, nil
}
