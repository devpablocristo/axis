package jobroles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectJobRoleSQL = `
	SELECT id, job_role_id, org_id, product_surface, name, slug, description,
	       mission, responsibilities_json, recommended_capabilities,
	       default_autonomy_level, default_permission_bundle_id, success_criteria,
	       default_sla_policy_json, default_memory_policy_json, status,
	       metadata_json, created_by, created_at, updated_at, archived_at, version
	FROM companion_job_roles`

func (r *PostgresRepository) ListJobRoles(ctx context.Context, orgID, productSurface string, lifecycle LifecycleView) ([]JobRole, error) {
	query := selectJobRoleSQL + ` WHERE org_id = $1 AND product_surface = $2`
	switch lifecycle {
	case LifecycleArchived:
		query += ` AND status = 'archived'`
	case LifecycleAll:
	default:
		query += ` AND status = 'active'`
	}
	query += ` ORDER BY name, job_role_id`
	rows, err := r.db.Pool().Query(ctx, query, strings.TrimSpace(orgID), strings.TrimSpace(productSurface))
	if err != nil {
		return nil, fmt.Errorf("list job roles: %w", err)
	}
	defer rows.Close()
	out := make([]JobRole, 0)
	for rows.Next() {
		role, err := scanJobRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetJobRole(ctx context.Context, orgID, productSurface, jobRoleID string) (JobRole, error) {
	row := r.db.Pool().QueryRow(ctx, selectJobRoleSQL+`
		WHERE org_id = $1 AND product_surface = $2 AND job_role_id = $3
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(jobRoleID))
	role, err := scanJobRole(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return JobRole{}, ErrNotFound
		}
		return JobRole{}, fmt.Errorf("get job role: %w", err)
	}
	return role, nil
}

func (r *PostgresRepository) UpsertJobRole(ctx context.Context, role JobRole) (JobRole, error) {
	role = normalizeJobRole(role)
	responsibilities, err := json.Marshal(role.Responsibilities)
	if err != nil {
		return JobRole{}, fmt.Errorf("marshal responsibilities: %w", err)
	}
	slaPolicy, err := json.Marshal(nonNilMap(role.DefaultSLAPolicy))
	if err != nil {
		return JobRole{}, fmt.Errorf("marshal default sla policy: %w", err)
	}
	memoryPolicy, err := json.Marshal(nonNilMap(role.DefaultMemoryPolicy))
	if err != nil {
		return JobRole{}, fmt.Errorf("marshal default memory policy: %w", err)
	}
	metadata, err := json.Marshal(nonNilMap(role.Metadata))
	if err != nil {
		return JobRole{}, fmt.Errorf("marshal metadata: %w", err)
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return JobRole{}, fmt.Errorf("begin job role upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_job_roles (
			job_role_id, org_id, product_surface, name, slug, description, mission,
			responsibilities_json, recommended_capabilities, default_autonomy_level,
			default_permission_bundle_id, success_criteria, default_sla_policy_json,
			default_memory_policy_json, status, metadata_json, created_by, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,now())
		ON CONFLICT (org_id, product_surface, job_role_id)
		DO UPDATE SET
			name = EXCLUDED.name,
			slug = EXCLUDED.slug,
			description = EXCLUDED.description,
			mission = EXCLUDED.mission,
			responsibilities_json = EXCLUDED.responsibilities_json,
			recommended_capabilities = EXCLUDED.recommended_capabilities,
			default_autonomy_level = EXCLUDED.default_autonomy_level,
			default_permission_bundle_id = EXCLUDED.default_permission_bundle_id,
			success_criteria = EXCLUDED.success_criteria,
			default_sla_policy_json = EXCLUDED.default_sla_policy_json,
			default_memory_policy_json = EXCLUDED.default_memory_policy_json,
			status = EXCLUDED.status,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = EXCLUDED.updated_at,
			archived_at = CASE WHEN EXCLUDED.status = 'archived' THEN COALESCE(companion_job_roles.archived_at, now()) ELSE NULL END,
			version = companion_job_roles.version + 1
		RETURNING id, job_role_id, org_id, product_surface, name, slug, description,
		          mission, responsibilities_json, recommended_capabilities,
		          default_autonomy_level, default_permission_bundle_id, success_criteria,
		          default_sla_policy_json, default_memory_policy_json, status,
		          metadata_json, created_by, created_at, updated_at, archived_at, version
	`, role.JobRoleID, role.OrgID, role.ProductSurface, role.Name, role.Slug, role.Description, role.Mission,
		responsibilities, role.RecommendedCapabilities, role.DefaultAutonomyLevel, role.DefaultPermissionBundleID,
		role.SuccessCriteria, slaPolicy, memoryPolicy, role.Status, metadata, role.CreatedBy)
	out, err := scanJobRole(row)
	if err != nil {
		return JobRole{}, mapConflict(err, "upsert job role")
	}
	if err := insertAudit(ctx, tx, out, "upsert", role.CreatedBy); err != nil {
		return JobRole{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobRole{}, fmt.Errorf("commit job role upsert: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) ArchiveJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	return r.updateLifecycle(ctx, orgID, productSurface, jobRoleID, actorID, "archived", "archive")
}

func (r *PostgresRepository) RestoreJobRole(ctx context.Context, orgID, productSurface, jobRoleID, actorID string) (JobRole, error) {
	return r.updateLifecycle(ctx, orgID, productSurface, jobRoleID, actorID, "active", "restore")
}

func (r *PostgresRepository) updateLifecycle(ctx context.Context, orgID, productSurface, jobRoleID, actorID, status, action string) (JobRole, error) {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return JobRole{}, fmt.Errorf("begin job role lifecycle: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanJobRole(tx.QueryRow(ctx, selectJobRoleSQL+`
		WHERE org_id = $1 AND product_surface = $2 AND job_role_id = $3
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(jobRoleID)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return JobRole{}, ErrNotFound
		}
		return JobRole{}, fmt.Errorf("%s job role: %w", action, err)
	}
	if current.Status == status {
		if err := tx.Commit(ctx); err != nil {
			return JobRole{}, fmt.Errorf("commit job role lifecycle no-op: %w", err)
		}
		return current, nil
	}
	row := tx.QueryRow(ctx, `
		UPDATE companion_job_roles
		SET status = $4,
		    archived_at = CASE WHEN $4 = 'archived' THEN now() ELSE NULL END,
		    updated_at = now(),
		    version = version + 1
		WHERE org_id = $1 AND product_surface = $2 AND job_role_id = $3
		RETURNING id, job_role_id, org_id, product_surface, name, slug, description,
		          mission, responsibilities_json, recommended_capabilities,
		          default_autonomy_level, default_permission_bundle_id, success_criteria,
		          default_sla_policy_json, default_memory_policy_json, status,
		          metadata_json, created_by, created_at, updated_at, archived_at, version
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(jobRoleID), status)
	out, err := scanJobRole(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return JobRole{}, ErrNotFound
		}
		return JobRole{}, fmt.Errorf("%s job role: %w", action, err)
	}
	if err := insertAudit(ctx, tx, out, action, actorID); err != nil {
		return JobRole{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobRole{}, fmt.Errorf("commit job role lifecycle: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) ListVersions(ctx context.Context, orgID, productSurface, jobRoleID string, limit int) ([]Version, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, job_role_id, org_id, product_surface, version, action, changed_by, role_json, created_at
		FROM companion_job_role_audit
		WHERE org_id = $1 AND product_surface = $2 AND job_role_id = $3
		ORDER BY version DESC, created_at DESC
		LIMIT $4
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), strings.TrimSpace(jobRoleID), limit)
	if err != nil {
		return nil, fmt.Errorf("list job role versions: %w", err)
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		var version Version
		var raw []byte
		if err := rows.Scan(&version.ID, &version.JobRoleID, &version.OrgID, &version.ProductSurface, &version.Version, &version.Action, &version.ChangedBy, &raw, &version.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &version.Role); err != nil {
				return nil, fmt.Errorf("unmarshal job role version: %w", err)
			}
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

type txExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func insertAudit(ctx context.Context, tx txExecutor, role JobRole, action, actorID string) error {
	raw, err := json.Marshal(role)
	if err != nil {
		return fmt.Errorf("marshal job role audit: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO companion_job_role_audit (
			job_role_id, org_id, product_surface, version, action, changed_by, role_json
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, role.JobRoleID, role.OrgID, role.ProductSurface, role.Version, action, strings.TrimSpace(actorID), raw); err != nil {
		return fmt.Errorf("insert job role audit: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobRole(row rowScanner) (JobRole, error) {
	var role JobRole
	var responsibilitiesRaw, slaRaw, memoryRaw, metadataRaw []byte
	err := row.Scan(
		&role.ID, &role.JobRoleID, &role.OrgID, &role.ProductSurface, &role.Name, &role.Slug,
		&role.Description, &role.Mission, &responsibilitiesRaw, &role.RecommendedCapabilities,
		&role.DefaultAutonomyLevel, &role.DefaultPermissionBundleID, &role.SuccessCriteria,
		&slaRaw, &memoryRaw, &role.Status, &metadataRaw, &role.CreatedBy,
		&role.CreatedAt, &role.UpdatedAt, &role.ArchivedAt, &role.Version,
	)
	if err != nil {
		return JobRole{}, err
	}
	if len(responsibilitiesRaw) > 0 {
		if err := json.Unmarshal(responsibilitiesRaw, &role.Responsibilities); err != nil {
			return JobRole{}, fmt.Errorf("unmarshal responsibilities: %w", err)
		}
	}
	if len(slaRaw) > 0 {
		if err := json.Unmarshal(slaRaw, &role.DefaultSLAPolicy); err != nil {
			return JobRole{}, fmt.Errorf("unmarshal default sla policy: %w", err)
		}
	}
	if len(memoryRaw) > 0 {
		if err := json.Unmarshal(memoryRaw, &role.DefaultMemoryPolicy); err != nil {
			return JobRole{}, fmt.Errorf("unmarshal default memory policy: %w", err)
		}
	}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &role.Metadata); err != nil {
			return JobRole{}, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return role, nil
}

func mapConflict(err error, op string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "companion_job_roles_org_id_product_surface_slug_key") {
		return fmt.Errorf("%w: slug already exists", ErrConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
