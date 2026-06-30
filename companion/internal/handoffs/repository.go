package handoffs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	commonaudit "github.com/devpablocristo/companion/internal/audit"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type Repository interface {
	List(ctx context.Context, tenantID, orgID, productSurface string, status Status, limit int) ([]Handoff, error)
	Get(ctx context.Context, tenantID, orgID, productSurface, handoffID string) (Handoff, error)
	Create(ctx context.Context, handoff Handoff) (Handoff, error)
	Update(ctx context.Context, handoff Handoff) (Handoff, error)
}

type PostgresRepository struct {
	db    *sharedpostgres.DB
	audit commonaudit.Recorder
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) SetAuditRecorder(recorder commonaudit.Recorder) {
	r.audit = recorder
}

const handoffColumns = `
	handoff_id, tenant_id, org_id, product_surface, task_id, from_employee_id,
	to_employee_id, reason, status, created_by, created_at, updated_at, resolved_at`

func (r *PostgresRepository) List(ctx context.Context, tenantID, orgID, productSurface string, status Status, limit int) ([]Handoff, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + handoffColumns + `
		FROM companion_employee_handoffs
		WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3`
	args := []any{tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface)}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d", len(args))
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list employee handoffs: %w", err)
	}
	defer rows.Close()
	out := make([]Handoff, 0)
	for rows.Next() {
		handoff, err := scanHandoff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, handoff)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) Get(ctx context.Context, tenantID, orgID, productSurface, handoffID string) (Handoff, error) {
	row := r.db.Pool().QueryRow(ctx, `SELECT `+handoffColumns+`
		FROM companion_employee_handoffs
		WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND handoff_id = $4
	`, tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(handoffID))
	handoff, err := scanHandoff(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Handoff{}, ErrNotFound
		}
		return Handoff{}, fmt.Errorf("get employee handoff: %w", err)
	}
	return handoff, nil
}

func (r *PostgresRepository) Create(ctx context.Context, handoff Handoff) (Handoff, error) {
	handoff = normalize(handoff)
	if err := validate(handoff); err != nil {
		return Handoff{}, err
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Handoff{}, fmt.Errorf("begin employee handoff create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := r.validateEmployees(ctx, tx, handoff); err != nil {
		return Handoff{}, err
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_employee_handoffs (
			tenant_id, org_id, product_surface, task_id, from_employee_id,
			to_employee_id, reason, status, created_by
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+handoffColumns+`
	`, handoff.TenantID, handoff.OrgID, handoff.ProductSurface, handoff.TaskID, handoff.FromEmployeeID,
		handoff.ToEmployeeID, handoff.Reason, handoff.Status, handoff.CreatedBy)
	created, err := scanHandoff(row)
	if err != nil {
		return Handoff{}, fmt.Errorf("create employee handoff: %w", err)
	}
	if err := r.recordAudit(ctx, tx, created, "created"); err != nil {
		return Handoff{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Handoff{}, fmt.Errorf("commit employee handoff create: %w", err)
	}
	return created, nil
}

func (r *PostgresRepository) Update(ctx context.Context, handoff Handoff) (Handoff, error) {
	handoff = normalize(handoff)
	if err := validate(handoff); err != nil {
		return Handoff{}, err
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Handoff{}, fmt.Errorf("begin employee handoff update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := r.validateEmployees(ctx, tx, handoff); err != nil {
		return Handoff{}, err
	}
	resolvedExpr := "resolved_at"
	if handoff.Status != StatusPending {
		resolvedExpr = "COALESCE(resolved_at, now())"
	}
	row := tx.QueryRow(ctx, `
		UPDATE companion_employee_handoffs
		SET task_id = $5,
		    from_employee_id = $6,
		    to_employee_id = $7,
		    reason = $8,
		    status = $9,
		    updated_at = now(),
		    resolved_at = `+resolvedExpr+`
		WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND handoff_id = $4
		RETURNING `+handoffColumns+`
	`, handoff.TenantID, handoff.OrgID, handoff.ProductSurface, handoff.HandoffID, handoff.TaskID,
		handoff.FromEmployeeID, handoff.ToEmployeeID, handoff.Reason, handoff.Status)
	updated, err := scanHandoff(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Handoff{}, ErrNotFound
		}
		return Handoff{}, fmt.Errorf("update employee handoff: %w", err)
	}
	if updated.Status == StatusAccepted && updated.TaskID != nil {
		if err := assignTask(ctx, tx, updated); err != nil {
			return Handoff{}, err
		}
	}
	if err := r.recordAudit(ctx, tx, updated, "status."+string(updated.Status)); err != nil {
		return Handoff{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Handoff{}, fmt.Errorf("commit employee handoff update: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) validateEmployees(ctx context.Context, tx pgx.Tx, handoff Handoff) error {
	for _, employeeID := range []*uuid.UUID{handoff.FromEmployeeID, &handoff.ToEmployeeID} {
		if employeeID == nil || *employeeID == uuid.Nil {
			continue
		}
		var ok bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM companion_virtual_employees
				WHERE id = $1 AND tenant_id = $2 AND org_id = $3 AND product_surface = $4
				  AND status NOT IN ('archived', 'trashed')
			)
		`, *employeeID, handoff.TenantID, handoff.OrgID, handoff.ProductSurface).Scan(&ok); err != nil {
			return fmt.Errorf("validate handoff employee: %w", err)
		}
		if !ok {
			return fmt.Errorf("%w: employee does not belong to this tenant", ErrValidation)
		}
	}
	return nil
}

func assignTask(ctx context.Context, tx pgx.Tx, handoff Handoff) error {
	tag, err := tx.Exec(ctx, `
		UPDATE companion_tasks
		SET assigned_to = $1,
		    context_json = jsonb_set(COALESCE(context_json, '{}'::jsonb), '{assignee_employee_id}', to_jsonb($1::text), true),
		    updated_at = now()
		WHERE id = $2 AND org_id = $3
	`, handoff.ToEmployeeID.String(), *handoff.TaskID, handoff.OrgID)
	if err != nil {
		return fmt.Errorf("assign handoff task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: task_id does not reference a task in this tenant", ErrValidation)
	}
	return nil
}

func (r *PostgresRepository) recordAudit(ctx context.Context, tx pgx.Tx, handoff Handoff, action string) error {
	if r.audit == nil {
		return nil
	}
	return r.audit.RecordTx(ctx, tx, commonaudit.Event{
		TenantID:     handoff.TenantID.String(),
		ResourceType: "handoff",
		ResourceID:   handoff.HandoffID,
		Action:       action,
		ActorUserID:  handoff.CreatedBy,
	})
}

type scanner interface {
	Scan(dest ...any) error
}

func scanHandoff(row scanner) (Handoff, error) {
	var handoff Handoff
	if err := row.Scan(
		&handoff.HandoffID,
		&handoff.TenantID,
		&handoff.OrgID,
		&handoff.ProductSurface,
		&handoff.TaskID,
		&handoff.FromEmployeeID,
		&handoff.ToEmployeeID,
		&handoff.Reason,
		&handoff.Status,
		&handoff.CreatedBy,
		&handoff.CreatedAt,
		&handoff.UpdatedAt,
		&handoff.ResolvedAt,
	); err != nil {
		return Handoff{}, err
	}
	return handoff, nil
}

var _ Repository = (*PostgresRepository)(nil)
