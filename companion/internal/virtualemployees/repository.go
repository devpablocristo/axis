package virtualemployees

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	commonaudit "github.com/devpablocristo/companion/internal/audit"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

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

const employeeColumnsSQL = `
	id, tenant_id, org_id, product_surface, name, supervisor_user_id, status,
	job_role_id, profile_id, autonomy, memory_id, created_by, version`

const selectEmployeeSQL = `
	SELECT ` + employeeColumnsSQL + `
	FROM companion_virtual_employees`

func (r *PostgresRepository) ListEmployees(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]VirtualEmployee, error) {
	query := selectEmployeeSQL + ` WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3`
	switch lifecycle {
	case "archived":
		query += ` AND status = 'archived'`
	case "trashed":
		query += ` AND status = 'trashed'`
	case "all":
	default:
		query += ` AND status NOT IN ('archived', 'trashed')`
	}
	query += ` ORDER BY name`
	rows, err := r.db.Pool().Query(ctx, query, tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface))
	if err != nil {
		return nil, fmt.Errorf("list virtual employees: %w", err)
	}
	defer rows.Close()
	out := make([]VirtualEmployee, 0)
	for rows.Next() {
		employee, err := scanEmployee(rows)
		if err != nil {
			return nil, err
		}
		caps, err := r.employeeCapabilities(ctx, employee.EmployeeID)
		if err != nil {
			return nil, err
		}
		employee.CapabilityIDs = caps
		out = append(out, employee)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetEmployee(ctx context.Context, tenantID, orgID, productSurface, employeeID string) (VirtualEmployee, error) {
	row := r.db.Pool().QueryRow(ctx, selectEmployeeSQL+`
		WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
	`, tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(employeeID))
	employee, err := scanEmployee(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VirtualEmployee{}, ErrNotFound
		}
		return VirtualEmployee{}, fmt.Errorf("get virtual employee: %w", err)
	}
	employee.CapabilityIDs, err = r.employeeCapabilities(ctx, employee.EmployeeID)
	if err != nil {
		return VirtualEmployee{}, err
	}
	return employee, nil
}

func (r *PostgresRepository) CreateEmployee(ctx context.Context, employee VirtualEmployee, actorID string) (VirtualEmployee, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return VirtualEmployee{}, fmt.Errorf("begin create virtual employee: %w", err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO companion_virtual_employees
				(tenant_id, org_id, product_surface, name, supervisor_user_id, status,
				 job_role_id, profile_id, autonomy, memory_id, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING `+employeeColumnsSQL+`
		)
		SELECT * FROM inserted
	`, employee.TenantID, employee.OrgID, employee.ProductSurface, employee.Name, employee.SupervisorUserID,
		employee.Status, employee.JobRoleID, employee.ProfileID, employee.Autonomy, employee.MemoryID, strings.TrimSpace(actorID))
	created, err := scanEmployee(row)
	if err != nil {
		return VirtualEmployee{}, fmt.Errorf("create virtual employee: %w", err)
	}
	created.CapabilityIDs = employee.CapabilityIDs
	if err := replaceCapabilities(ctx, tx, created.EmployeeID, created.CapabilityIDs); err != nil {
		return VirtualEmployee{}, err
	}
	if err := r.recordEmployeeAudit(ctx, tx, created, actorID, "created"); err != nil {
		return VirtualEmployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VirtualEmployee{}, fmt.Errorf("commit create virtual employee: %w", err)
	}
	return created, nil
}

func (r *PostgresRepository) UpdateEmployee(ctx context.Context, employee VirtualEmployee, actorID string) (VirtualEmployee, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return VirtualEmployee{}, fmt.Errorf("begin update virtual employee: %w", err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		WITH updated AS (
			UPDATE companion_virtual_employees
			SET name = $5,
			    supervisor_user_id = $6,
			    status = $7,
			    job_role_id = $8,
			    profile_id = $9,
			    autonomy = $10,
			    memory_id = $11,
			    updated_at = now(),
			    version = version + 1
			WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
			  AND status NOT IN ('archived', 'trashed')
			RETURNING `+employeeColumnsSQL+`
		)
		SELECT * FROM updated
	`, employee.TenantID, employee.OrgID, employee.ProductSurface, employee.EmployeeID, employee.Name,
		employee.SupervisorUserID, employee.Status, employee.JobRoleID, employee.ProfileID, employee.Autonomy, employee.MemoryID)
	updated, err := scanEmployee(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := r.GetEmployee(ctx, employee.TenantID.String(), employee.OrgID, employee.ProductSurface, employee.EmployeeID.String())
			if getErr == nil && (existing.Status == EmployeeStatusArchived || existing.Status == EmployeeStatusTrashed) {
				return VirtualEmployee{}, fmt.Errorf("%w: virtual employee is not editable in current status", ErrConflict)
			}
			if errors.Is(getErr, ErrNotFound) {
				return VirtualEmployee{}, ErrNotFound
			}
			if getErr != nil {
				return VirtualEmployee{}, getErr
			}
		}
		return VirtualEmployee{}, fmt.Errorf("update virtual employee: %w", err)
	}
	updated.CapabilityIDs = employee.CapabilityIDs
	if err := replaceCapabilities(ctx, tx, updated.EmployeeID, updated.CapabilityIDs); err != nil {
		return VirtualEmployee{}, err
	}
	if err := r.recordEmployeeAudit(ctx, tx, updated, actorID, "updated"); err != nil {
		return VirtualEmployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VirtualEmployee{}, fmt.Errorf("commit update virtual employee: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) SetEmployeeStatus(ctx context.Context, tenantID, orgID, productSurface, employeeID string, status EmployeeStatus, actorID string) (VirtualEmployee, error) {
	current, err := r.GetEmployee(ctx, tenantID, orgID, productSurface, employeeID)
	if err != nil {
		return VirtualEmployee{}, err
	}
	if current.Status == status {
		return current, nil
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return VirtualEmployee{}, fmt.Errorf("begin status virtual employee: %w", err)
	}
	defer tx.Rollback(ctx)
	archivedExpr := "NULL"
	trashedExpr := "NULL"
	if status == EmployeeStatusArchived {
		archivedExpr = "now()"
	}
	if status == EmployeeStatusTrashed {
		trashedExpr = "now()"
	}
	row := tx.QueryRow(ctx, `
		WITH updated AS (
			UPDATE companion_virtual_employees
			SET status = $5,
			    archived_at = `+archivedExpr+`,
			    trashed_at = `+trashedExpr+`,
			    updated_at = now(),
			    version = version + 1
			WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
			RETURNING `+employeeColumnsSQL+`
		)
		SELECT * FROM updated
	`, tenantID, orgID, productSurface, employeeID, status)
	updated, err := scanEmployee(row)
	if err != nil {
		return VirtualEmployee{}, fmt.Errorf("set virtual employee status: %w", err)
	}
	updated.CapabilityIDs = current.CapabilityIDs
	if err := r.recordEmployeeAudit(ctx, tx, updated, actorID, "status."+string(status)); err != nil {
		return VirtualEmployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VirtualEmployee{}, fmt.Errorf("commit status virtual employee: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) ValidateReferences(ctx context.Context, employee VirtualEmployee) error {
	if ok, err := exists(ctx, r.db, `
		SELECT EXISTS (
			SELECT 1 FROM companion_job_roles
			WHERE id = $1 AND tenant_id = $2 AND org_id = $3 AND product_surface = $4 AND status = 'active'
		)
	`, employee.JobRoleID, employee.TenantID.String(), employee.OrgID, employee.ProductSurface); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: job_role_id does not reference an active JobRole", ErrValidation)
	}
	if ok, err := exists(ctx, r.db, `SELECT EXISTS (SELECT 1 FROM agent_profiles WHERE id = $1 AND enabled = true AND archived_at IS NULL)`, employee.ProfileID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: profile_id does not reference an active EmployeeProfile", ErrValidation)
	}
	for _, capabilityID := range employee.CapabilityIDs {
		if ok, err := exists(ctx, r.db, `SELECT EXISTS (SELECT 1 FROM companion_capability_manifests WHERE id = $1 AND status = 'active')`, capabilityID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: capability_id does not reference an active Capability", ErrValidation)
		}
	}
	if employee.MemoryID != nil {
		if ok, err := exists(ctx, r.db, `
			SELECT EXISTS (
				SELECT 1 FROM companion_memories
				WHERE id = $1 AND tenant_id = $2 AND org_id = $3 AND product_surface = $4 AND status = 'active'
			)
		`, *employee.MemoryID, employee.TenantID, employee.OrgID, employee.ProductSurface); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: memory_id does not reference an active Memory", ErrValidation)
		}
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEmployee(row scanner) (VirtualEmployee, error) {
	var employee VirtualEmployee
	if err := row.Scan(
		&employee.EmployeeID,
		&employee.TenantID,
		&employee.OrgID,
		&employee.ProductSurface,
		&employee.Name,
		&employee.SupervisorUserID,
		&employee.Status,
		&employee.JobRoleID,
		&employee.ProfileID,
		&employee.Autonomy,
		&employee.MemoryID,
		&employee.createdBy,
		&employee.version,
	); err != nil {
		return VirtualEmployee{}, err
	}
	return employee, nil
}

func (r *PostgresRepository) employeeCapabilities(ctx context.Context, employeeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT capability_id
		FROM companion_virtual_employee_capabilities
		WHERE employee_id = $1
		ORDER BY created_at, capability_id
	`, employeeID)
	if err != nil {
		return nil, fmt.Errorf("list virtual employee capabilities: %w", err)
	}
	defer rows.Close()
	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func replaceCapabilities(ctx context.Context, tx pgx.Tx, employeeID uuid.UUID, capabilityIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM companion_virtual_employee_capabilities WHERE employee_id = $1`, employeeID); err != nil {
		return fmt.Errorf("clear virtual employee capabilities: %w", err)
	}
	for _, capabilityID := range capabilityIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO companion_virtual_employee_capabilities (employee_id, capability_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, employeeID, capabilityID); err != nil {
			return fmt.Errorf("insert virtual employee capability: %w", err)
		}
	}
	return nil
}

func (r *PostgresRepository) recordEmployeeAudit(ctx context.Context, tx pgx.Tx, employee VirtualEmployee, actorID string, action string) error {
	snapshot, err := json.Marshal(employee)
	if err != nil {
		return fmt.Errorf("marshal virtual employee audit snapshot: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_virtual_employee_audit
			(employee_id, tenant_id, actor_id, action, status, snapshot)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, employee.EmployeeID, employee.TenantID, strings.TrimSpace(actorID), action, employee.Status, snapshot)
	if err != nil {
		return fmt.Errorf("record virtual employee audit: %w", err)
	}
	if r.audit != nil {
		if err := r.audit.RecordTx(ctx, tx, commonaudit.Event{
			TenantID:     employee.TenantID.String(),
			ResourceType: "virtual_employee",
			ResourceID:   employee.EmployeeID,
			Action:       action,
			ActorUserID:  strings.TrimSpace(actorID),
		}); err != nil {
			return fmt.Errorf("record virtual employee lifecycle audit: %w", err)
		}
	}
	return nil
}

func exists(ctx context.Context, db *sharedpostgres.DB, query string, args ...any) (bool, error) {
	var ok bool
	if err := db.Pool().QueryRow(ctx, query, args...).Scan(&ok); err != nil {
		return false, fmt.Errorf("validate virtual employee reference: %w", err)
	}
	return ok, nil
}

var _ Repository = (*PostgresRepository)(nil)
