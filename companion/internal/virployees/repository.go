package virployees

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

const virployeeColumnsSQL = `
	id, tenant_id, org_id, product_surface, name, supervisor_user_id, status,
	job_role_id, profile_id, autonomy, memory_id, created_by, version`

const selectVirployeeSQL = `
	SELECT ` + virployeeColumnsSQL + `
	FROM companion_virployees`

func (r *PostgresRepository) ListVirployees(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]Virployee, error) {
	query := selectVirployeeSQL + ` WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3`
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
		return nil, fmt.Errorf("list virployees: %w", err)
	}
	defer rows.Close()
	out := make([]Virployee, 0)
	for rows.Next() {
		virployee, err := scanVirployee(rows)
		if err != nil {
			return nil, err
		}
		caps, err := r.virployeeCapabilities(ctx, virployee.VirployeeID)
		if err != nil {
			return nil, err
		}
		virployee.CapabilityIDs = caps
		out = append(out, virployee)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetVirployee(ctx context.Context, tenantID, orgID, productSurface, virployeeID string) (Virployee, error) {
	row := r.db.Pool().QueryRow(ctx, selectVirployeeSQL+`
		WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
	`, tenantID, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(virployeeID))
	virployee, err := scanVirployee(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Virployee{}, ErrNotFound
		}
		return Virployee{}, fmt.Errorf("get virployee: %w", err)
	}
	virployee.CapabilityIDs, err = r.virployeeCapabilities(ctx, virployee.VirployeeID)
	if err != nil {
		return Virployee{}, err
	}
	return virployee, nil
}

func (r *PostgresRepository) CreateVirployee(ctx context.Context, virployee Virployee, actorID string) (Virployee, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Virployee{}, fmt.Errorf("begin create virployee: %w", err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO companion_virployees
				(tenant_id, org_id, product_surface, name, supervisor_user_id, status,
				 job_role_id, profile_id, autonomy, memory_id, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING `+virployeeColumnsSQL+`
		)
		SELECT * FROM inserted
	`, virployee.TenantID, virployee.OrgID, virployee.ProductSurface, virployee.Name, virployee.SupervisorUserID,
		virployee.Status, virployee.JobRoleID, virployee.ProfileID, virployee.Autonomy, virployee.MemoryID, strings.TrimSpace(actorID))
	created, err := scanVirployee(row)
	if err != nil {
		return Virployee{}, fmt.Errorf("create virployee: %w", err)
	}
	created.CapabilityIDs = virployee.CapabilityIDs
	if err := replaceVirployeeCapabilities(ctx, tx, created.VirployeeID, created.CapabilityIDs); err != nil {
		return Virployee{}, err
	}
	if err := r.recordVirployeeAudit(ctx, tx, created, actorID, "created"); err != nil {
		return Virployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Virployee{}, fmt.Errorf("commit create virployee: %w", err)
	}
	return created, nil
}

func (r *PostgresRepository) UpdateVirployee(ctx context.Context, virployee Virployee, actorID string) (Virployee, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Virployee{}, fmt.Errorf("begin update virployee: %w", err)
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		WITH updated AS (
			UPDATE companion_virployees
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
			RETURNING `+virployeeColumnsSQL+`
		)
		SELECT * FROM updated
	`, virployee.TenantID, virployee.OrgID, virployee.ProductSurface, virployee.VirployeeID, virployee.Name,
		virployee.SupervisorUserID, virployee.Status, virployee.JobRoleID, virployee.ProfileID, virployee.Autonomy, virployee.MemoryID)
	updated, err := scanVirployee(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := r.GetVirployee(ctx, virployee.TenantID.String(), virployee.OrgID, virployee.ProductSurface, virployee.VirployeeID.String())
			if getErr == nil && (existing.Status == VirployeeStatusArchived || existing.Status == VirployeeStatusTrashed) {
				return Virployee{}, fmt.Errorf("%w: virployee is not editable in current status", ErrConflict)
			}
			if errors.Is(getErr, ErrNotFound) {
				return Virployee{}, ErrNotFound
			}
			if getErr != nil {
				return Virployee{}, getErr
			}
		}
		return Virployee{}, fmt.Errorf("update virployee: %w", err)
	}
	updated.CapabilityIDs = virployee.CapabilityIDs
	if err := replaceVirployeeCapabilities(ctx, tx, updated.VirployeeID, updated.CapabilityIDs); err != nil {
		return Virployee{}, err
	}
	if err := r.recordVirployeeAudit(ctx, tx, updated, actorID, "updated"); err != nil {
		return Virployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Virployee{}, fmt.Errorf("commit update virployee: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) SetVirployeeStatus(ctx context.Context, tenantID, orgID, productSurface, virployeeID string, status VirployeeStatus, actorID string) (Virployee, error) {
	current, err := r.GetVirployee(ctx, tenantID, orgID, productSurface, virployeeID)
	if err != nil {
		return Virployee{}, err
	}
	if current.Status == status {
		return current, nil
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Virployee{}, fmt.Errorf("begin status virployee: %w", err)
	}
	defer tx.Rollback(ctx)
	archivedExpr := "NULL"
	trashedExpr := "NULL"
	if status == VirployeeStatusArchived {
		archivedExpr = "now()"
	}
	if status == VirployeeStatusTrashed {
		trashedExpr = "now()"
	}
	row := tx.QueryRow(ctx, `
		WITH updated AS (
			UPDATE companion_virployees
			SET status = $5,
			    archived_at = `+archivedExpr+`,
			    trashed_at = `+trashedExpr+`,
			    updated_at = now(),
			    version = version + 1
			WHERE tenant_id = $1 AND org_id = $2 AND product_surface = $3 AND id = $4
			RETURNING `+virployeeColumnsSQL+`
		)
		SELECT * FROM updated
	`, tenantID, orgID, productSurface, virployeeID, status)
	updated, err := scanVirployee(row)
	if err != nil {
		return Virployee{}, fmt.Errorf("set virployee status: %w", err)
	}
	updated.CapabilityIDs = current.CapabilityIDs
	if err := r.recordVirployeeAudit(ctx, tx, updated, actorID, "status."+string(status)); err != nil {
		return Virployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Virployee{}, fmt.Errorf("commit status virployee: %w", err)
	}
	return updated, nil
}

func (r *PostgresRepository) ValidateReferences(ctx context.Context, virployee Virployee) error {
	if ok, err := exists(ctx, r.db, `
		SELECT EXISTS (
			SELECT 1 FROM companion_job_roles
			WHERE id = $1 AND tenant_id = $2 AND org_id = $3 AND product_surface = $4 AND status = 'active'
		)
	`, virployee.JobRoleID, virployee.TenantID.String(), virployee.OrgID, virployee.ProductSurface); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: job_role_id does not reference an active JobRole", ErrValidation)
	}
	if ok, err := exists(ctx, r.db, `SELECT EXISTS (SELECT 1 FROM agent_profiles WHERE id = $1 AND enabled = true AND archived_at IS NULL)`, virployee.ProfileID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: profile_id does not reference an active VirployeeProfile", ErrValidation)
	}
	for _, capabilityID := range virployee.CapabilityIDs {
		if ok, err := exists(ctx, r.db, `SELECT EXISTS (SELECT 1 FROM companion_capability_manifests WHERE id = $1 AND status = 'active')`, capabilityID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: capability_id does not reference an active Capability", ErrValidation)
		}
	}
	if virployee.MemoryID != nil {
		if ok, err := exists(ctx, r.db, `
			SELECT EXISTS (
				SELECT 1 FROM companion_memories
				WHERE id = $1 AND tenant_id = $2 AND org_id = $3 AND product_surface = $4 AND status = 'active'
			)
		`, *virployee.MemoryID, virployee.TenantID, virployee.OrgID, virployee.ProductSurface); err != nil {
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

func scanVirployee(row scanner) (Virployee, error) {
	var virployee Virployee
	if err := row.Scan(
		&virployee.VirployeeID,
		&virployee.TenantID,
		&virployee.OrgID,
		&virployee.ProductSurface,
		&virployee.Name,
		&virployee.SupervisorUserID,
		&virployee.Status,
		&virployee.JobRoleID,
		&virployee.ProfileID,
		&virployee.Autonomy,
		&virployee.MemoryID,
		&virployee.createdBy,
		&virployee.version,
	); err != nil {
		return Virployee{}, err
	}
	return virployee, nil
}

func (r *PostgresRepository) virployeeCapabilities(ctx context.Context, virployeeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT capability_id
		FROM companion_virployee_capabilities
		WHERE virployee_id = $1
		ORDER BY created_at, capability_id
	`, virployeeID)
	if err != nil {
		return nil, fmt.Errorf("list virployee capabilities: %w", err)
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

func replaceVirployeeCapabilities(ctx context.Context, tx pgx.Tx, virployeeID uuid.UUID, capabilityIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM companion_virployee_capabilities WHERE virployee_id = $1`, virployeeID); err != nil {
		return fmt.Errorf("clear virployee capabilities: %w", err)
	}
	for _, capabilityID := range capabilityIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO companion_virployee_capabilities (virployee_id, capability_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, virployeeID, capabilityID); err != nil {
			return fmt.Errorf("insert virployee capability: %w", err)
		}
	}
	return nil
}

func (r *PostgresRepository) recordVirployeeAudit(ctx context.Context, tx pgx.Tx, virployee Virployee, actorID string, action string) error {
	snapshot, err := json.Marshal(virployee)
	if err != nil {
		return fmt.Errorf("marshal virployee audit snapshot: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_virployee_audit
			(virployee_id, tenant_id, actor_id, action, status, snapshot)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, virployee.VirployeeID, virployee.TenantID, strings.TrimSpace(actorID), action, virployee.Status, snapshot)
	if err != nil {
		return fmt.Errorf("record virployee audit: %w", err)
	}
	if r.audit != nil {
		if err := r.audit.RecordTx(ctx, tx, commonaudit.Event{
			TenantID:     virployee.TenantID.String(),
			ResourceType: "virployee",
			ResourceID:   virployee.VirployeeID,
			Action:       action,
			ActorUserID:  strings.TrimSpace(actorID),
		}); err != nil {
			return fmt.Errorf("record virployee lifecycle audit: %w", err)
		}
	}
	return nil
}

func exists(ctx context.Context, db *sharedpostgres.DB, query string, args ...any) (bool, error) {
	var ok bool
	if err := db.Pool().QueryRow(ctx, query, args...).Scan(&ok); err != nil {
		return false, fmt.Errorf("validate virployee reference: %w", err)
	}
	return ok, nil
}

var _ Repository = (*PostgresRepository)(nil)
