package jobroles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

const selectJobRoleSQL = `
	SELECT id, job_role_id, tenant_id, org_id, product_surface, name, slug, description,
	       mission, responsibilities_json, recommended_capability_ids, recommended_capabilities,
	       default_autonomy_level, default_permission_bundle_id, success_criteria_json, success_criteria,
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
	predicate, arg := jobRoleLookupPredicate(jobRoleID, "$3")
	row := r.db.Pool().QueryRow(ctx, selectJobRoleSQL+`
		WHERE org_id = $1 AND product_surface = $2 AND `+predicate+`
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), arg)
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
	successCriteria, err := json.Marshal(role.SuccessCriteria)
	if err != nil {
		return JobRole{}, fmt.Errorf("marshal success criteria: %w", err)
	}
	recommendedCapabilityIDs, err := parseUUIDList(role.RecommendedCapabilityIDs)
	if err != nil {
		return JobRole{}, fmt.Errorf("parse recommended capability ids: %w", err)
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
	storageKey := storageJobRoleKey(role)
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_job_roles (
			job_role_id, tenant_id, org_id, product_surface, name, slug, description, mission,
			responsibilities_json, recommended_capability_ids, recommended_capabilities, default_autonomy_level,
			default_permission_bundle_id, success_criteria_json, success_criteria, default_sla_policy_json,
			default_memory_policy_json, status, metadata_json, created_by, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,now())
		ON CONFLICT (org_id, product_surface, job_role_id)
		DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			name = EXCLUDED.name,
			slug = EXCLUDED.slug,
			description = EXCLUDED.description,
			mission = EXCLUDED.mission,
			responsibilities_json = EXCLUDED.responsibilities_json,
			recommended_capability_ids = EXCLUDED.recommended_capability_ids,
			recommended_capabilities = EXCLUDED.recommended_capabilities,
			default_autonomy_level = EXCLUDED.default_autonomy_level,
			default_permission_bundle_id = EXCLUDED.default_permission_bundle_id,
			success_criteria_json = EXCLUDED.success_criteria_json,
			success_criteria = EXCLUDED.success_criteria,
			default_sla_policy_json = EXCLUDED.default_sla_policy_json,
			default_memory_policy_json = EXCLUDED.default_memory_policy_json,
			status = EXCLUDED.status,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = EXCLUDED.updated_at,
			archived_at = CASE WHEN EXCLUDED.status = 'archived' THEN COALESCE(companion_job_roles.archived_at, now()) ELSE NULL END,
			version = companion_job_roles.version + 1
		RETURNING id, job_role_id, tenant_id, org_id, product_surface, name, slug, description,
		          mission, responsibilities_json, recommended_capability_ids, recommended_capabilities,
		          default_autonomy_level, default_permission_bundle_id, success_criteria_json, success_criteria,
		          default_sla_policy_json, default_memory_policy_json, status,
		          metadata_json, created_by, created_at, updated_at, archived_at, version
	`, storageKey, role.TenantID, role.OrgID, role.ProductSurface, role.Name, role.Slug, role.Description, role.Mission,
		responsibilities, recommendedCapabilityIDs, role.RecommendedCapabilities, role.DefaultAutonomyLevel, role.DefaultPermissionBundleID,
		successCriteria, legacySuccessCriteria(role.SuccessCriteria), slaPolicy, memoryPolicy, role.Status, metadata, role.CreatedBy)
	out, err := scanJobRole(row)
	if err != nil {
		return JobRole{}, mapConflict(err, "upsert job role")
	}
	if err := r.insertAudit(ctx, tx, out, "upsert", role.CreatedBy); err != nil {
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
	predicate, arg := jobRoleLookupPredicate(jobRoleID, "$3")
	current, err := scanJobRole(tx.QueryRow(ctx, selectJobRoleSQL+`
		WHERE org_id = $1 AND product_surface = $2 AND `+predicate+`
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), arg))
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
		WHERE org_id = $1 AND product_surface = $2 AND `+predicate+`
		RETURNING id, job_role_id, tenant_id, org_id, product_surface, name, slug, description,
		          mission, responsibilities_json, recommended_capability_ids, recommended_capabilities,
		          default_autonomy_level, default_permission_bundle_id, success_criteria_json, success_criteria,
		          default_sla_policy_json, default_memory_policy_json, status,
		          metadata_json, created_by, created_at, updated_at, archived_at, version
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), arg, status)
	out, err := scanJobRole(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return JobRole{}, ErrNotFound
		}
		return JobRole{}, fmt.Errorf("%s job role: %w", action, err)
	}
	if err := r.insertAudit(ctx, tx, out, action, actorID); err != nil {
		return JobRole{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return JobRole{}, fmt.Errorf("commit job role lifecycle: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) ListVersions(ctx context.Context, orgID, productSurface, jobRoleID string, limit int) ([]Version, error) {
	versionIDs := []string{strings.TrimSpace(jobRoleID)}
	if role, err := r.GetJobRole(ctx, orgID, productSurface, jobRoleID); err == nil && role.JobRoleID != "" && role.JobRoleID != strings.TrimSpace(jobRoleID) {
		versionIDs = append(versionIDs, role.JobRoleID)
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, job_role_id, tenant_id, org_id, product_surface, version, action, changed_by, role_json, created_at
		FROM companion_job_role_audit
		WHERE org_id = $1 AND product_surface = $2 AND job_role_id = ANY($3)
		ORDER BY version DESC, created_at DESC
		LIMIT $4
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface), versionIDs, limit)
	if err != nil {
		return nil, fmt.Errorf("list job role versions: %w", err)
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		var version Version
		var raw []byte
		if err := rows.Scan(&version.ID, &version.JobRoleID, &version.TenantID, &version.OrgID, &version.ProductSurface, &version.Version, &version.Action, &version.ChangedBy, &raw, &version.CreatedAt); err != nil {
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

func (r *PostgresRepository) insertAudit(ctx context.Context, tx txExecutor, role JobRole, action, actorID string) error {
	raw, err := json.Marshal(role)
	if err != nil {
		return fmt.Errorf("marshal job role audit: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO companion_job_role_audit (
			job_role_id, tenant_id, org_id, product_surface, version, action, changed_by, role_json
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, role.JobRoleID, role.TenantID, role.OrgID, role.ProductSurface, role.Version, action, strings.TrimSpace(actorID), raw); err != nil {
		return fmt.Errorf("insert job role audit: %w", err)
	}
	if r.audit != nil && role.ID != uuid.Nil {
		if err := r.audit.RecordTx(ctx, tx, commonaudit.Event{
			TenantID:     role.TenantID,
			ResourceType: "job_role",
			ResourceID:   role.ID,
			Action:       action,
			ActorUserID:  strings.TrimSpace(actorID),
		}); err != nil {
			return fmt.Errorf("insert job role lifecycle audit: %w", err)
		}
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobRole(row rowScanner) (JobRole, error) {
	var role JobRole
	var legacyJobRoleID string
	var recommendedCapabilityIDs []uuid.UUID
	var legacySuccessCriteria []string
	var responsibilitiesRaw, successCriteriaRaw, slaRaw, memoryRaw, metadataRaw []byte
	err := row.Scan(
		&role.ID, &legacyJobRoleID, &role.TenantID, &role.OrgID, &role.ProductSurface, &role.Name, &role.Slug,
		&role.Description, &role.Mission, &responsibilitiesRaw, &recommendedCapabilityIDs, &role.RecommendedCapabilities,
		&role.DefaultAutonomyLevel, &role.DefaultPermissionBundleID, &successCriteriaRaw, &legacySuccessCriteria,
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
	if len(successCriteriaRaw) > 0 {
		if err := json.Unmarshal(successCriteriaRaw, &role.SuccessCriteria); err != nil {
			return JobRole{}, fmt.Errorf("unmarshal success criteria: %w", err)
		}
	}
	if len(role.SuccessCriteria) == 0 && len(legacySuccessCriteria) > 0 {
		role.SuccessCriteria = successCriteriaFromStrings(legacySuccessCriteria)
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
	role.JobRoleKey = strings.TrimSpace(legacyJobRoleID)
	if role.ID != uuid.Nil {
		role.JobRoleID = role.ID.String()
	}
	role.DefaultAutonomy = role.DefaultAutonomyLevel
	role.RecommendedCapabilityIDs = stringifyUUIDList(recommendedCapabilityIDs)
	return role, nil
}

func mapConflict(err error, op string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "companion_job_roles_tenant_slug_uidx") {
		return fmt.Errorf("%w: slug already exists", ErrConflict)
	}
	if strings.Contains(err.Error(), "companion_job_roles_org_id_product_surface_slug_key") {
		return fmt.Errorf("%w: slug already exists", ErrConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func jobRoleLookupPredicate(identifier, placeholder string) (string, any) {
	identifier = strings.TrimSpace(identifier)
	if parsed, err := uuid.Parse(identifier); err == nil {
		return "id = " + placeholder, parsed
	}
	return "job_role_id = " + placeholder, identifier
}

func storageJobRoleKey(role JobRole) string {
	for _, value := range []string{role.JobRoleKey, role.Slug, role.JobRoleID} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := uuid.Parse(value); err == nil {
			continue
		}
		return value
	}
	if role.ID != uuid.Nil {
		return role.ID.String()
	}
	return strings.TrimSpace(role.JobRoleID)
}

func parseUUIDList(values []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func stringifyUUIDList(values []uuid.UUID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		out = append(out, value.String())
	}
	return out
}

func legacySuccessCriteria(values SuccessCriteria) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value.Title == "" {
			continue
		}
		out = append(out, value.Title)
	}
	return out
}

func successCriteriaFromStrings(values []string) SuccessCriteria {
	out := make(SuccessCriteria, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, SuccessCriterion{Title: value})
	}
	return out
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
