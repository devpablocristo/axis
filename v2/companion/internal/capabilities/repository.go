package capabilities

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/companion-v2/internal/capabilities/repository/models"
	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

const capabilitySelectColumns = `id::text, tenant_id, capability_key, name, description, required_autonomy,
	risk_class, side_effect_class, requires_nexus_approval, evidence_required, rollback_capability_key,
	promotion_state, manifest, manifest_hash, conformed_hash, conformance_report, conformed_at, activated_at,
	created_at, updated_at, archived_at, trashed_at, purge_after`

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Capability, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO capabilities (
			id, tenant_id, capability_key, name, description, required_autonomy,
			risk_class, side_effect_class, requires_nexus_approval, evidence_required, rollback_capability_key,
			created_at, updated_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
		RETURNING `+capabilitySelectColumns+`
	`, id.String(), tenantID, input.CapabilityKey, input.Name, input.Description, string(input.RequiredAutonomy),
		input.Governance.RiskClass, input.Governance.SideEffectClass, input.Governance.RequiresNexusApproval,
		input.Governance.EvidenceRequired, input.Governance.RollbackCapabilityKey, now)
	return scanCapability(row)
}

func (r *Repository) List(ctx context.Context, tenantID string, state domain.State) ([]domain.Capability, error) {
	var where string
	switch state {
	case domain.StateActive, "":
		where = "tenant_id = $1 AND archived_at IS NULL AND trashed_at IS NULL"
	case domain.StateArchived:
		where = "tenant_id = $1 AND archived_at IS NOT NULL AND trashed_at IS NULL"
	case domain.StateTrashed:
		where = "tenant_id = $1 AND trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+capabilitySelectColumns+`
		FROM capabilities
		WHERE `+where+`
		ORDER BY capability_key ASC, id ASC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Capability{}
	for rows.Next() {
		item, err := scanCapability(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Capability, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+capabilitySelectColumns+`
		FROM capabilities
		WHERE tenant_id = $1 AND id = $2::uuid AND trashed_at IS NULL
	`, tenantID, id.String())
	return scanCapability(row)
}

func (r *Repository) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Capability, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE capabilities
		SET name = $3,
			description = $4,
			required_autonomy = $5,
			risk_class = $6,
			side_effect_class = $7,
			requires_nexus_approval = $8,
			evidence_required = $9,
			rollback_capability_key = $10,
			promotion_state = CASE WHEN
				required_autonomy IS DISTINCT FROM $5 OR
				risk_class IS DISTINCT FROM $6 OR
				side_effect_class IS DISTINCT FROM $7 OR
				requires_nexus_approval IS DISTINCT FROM $8 OR
				evidence_required IS DISTINCT FROM $9 OR
				rollback_capability_key IS DISTINCT FROM $10
				THEN 'draft' ELSE promotion_state END,
			conformed_hash = CASE WHEN
				required_autonomy IS DISTINCT FROM $5 OR risk_class IS DISTINCT FROM $6 OR
				side_effect_class IS DISTINCT FROM $7 OR requires_nexus_approval IS DISTINCT FROM $8 OR
				evidence_required IS DISTINCT FROM $9 OR rollback_capability_key IS DISTINCT FROM $10
				THEN '' ELSE conformed_hash END,
			conformance_report = CASE WHEN
				required_autonomy IS DISTINCT FROM $5 OR risk_class IS DISTINCT FROM $6 OR
				side_effect_class IS DISTINCT FROM $7 OR requires_nexus_approval IS DISTINCT FROM $8 OR
				evidence_required IS DISTINCT FROM $9 OR rollback_capability_key IS DISTINCT FROM $10
				THEN '{"conformant":false,"checks":[]}'::jsonb ELSE conformance_report END,
			conformed_at = CASE WHEN
				required_autonomy IS DISTINCT FROM $5 OR risk_class IS DISTINCT FROM $6 OR
				side_effect_class IS DISTINCT FROM $7 OR requires_nexus_approval IS DISTINCT FROM $8 OR
				evidence_required IS DISTINCT FROM $9 OR rollback_capability_key IS DISTINCT FROM $10
				THEN NULL ELSE conformed_at END,
			activated_at = CASE WHEN
				required_autonomy IS DISTINCT FROM $5 OR risk_class IS DISTINCT FROM $6 OR
				side_effect_class IS DISTINCT FROM $7 OR requires_nexus_approval IS DISTINCT FROM $8 OR
				evidence_required IS DISTINCT FROM $9 OR rollback_capability_key IS DISTINCT FROM $10
				THEN NULL ELSE activated_at END,
			updated_at = $11
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING `+capabilitySelectColumns+`
	`, tenantID, id.String(), input.Name, input.Description, string(input.RequiredAutonomy),
		input.Governance.RiskClass, input.Governance.SideEffectClass, input.Governance.RequiresNexusApproval,
		input.Governance.EvidenceRequired, input.Governance.RollbackCapabilityKey, time.Now().UTC())
	item, err := scanCapability(row)
	if err == nil {
		return item, nil
	}
	if !domainerr.IsNotFound(err) {
		return domain.Capability{}, err
	}
	state, stateErr := r.State(ctx, tenantID, id)
	if stateErr != nil {
		return domain.Capability{}, stateErr
	}
	if state != lifecycle.StateActive {
		return domain.Capability{}, domainerr.Conflict("capability is not active")
	}
	return domain.Capability{}, err
}

func (r *Repository) UpdateManifest(ctx context.Context, tenantID string, id uuid.UUID, manifest domain.Manifest, manifestHash string) (domain.Capability, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return domain.Capability{}, err
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE capabilities
		SET manifest = $3::jsonb,
			manifest_hash = $4,
			promotion_state = 'draft',
			conformed_hash = '',
			conformance_report = '{"conformant":false,"checks":[]}'::jsonb,
			conformed_at = NULL,
			activated_at = NULL,
			updated_at = $5
		WHERE tenant_id = $1 AND id = $2::uuid
			AND archived_at IS NULL AND trashed_at IS NULL
		RETURNING `+capabilitySelectColumns+`
	`, tenantID, id.String(), raw, manifestHash, time.Now().UTC())
	return scanCapability(row)
}

func (r *Repository) SaveConformance(ctx context.Context, tenantID string, id uuid.UUID, expected domain.Capability, report domain.ConformanceReport) (domain.Capability, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return domain.Capability{}, err
	}
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		UPDATE capabilities
		SET promotion_state = CASE WHEN $4::boolean THEN 'conformant' ELSE 'draft' END,
			conformed_hash = CASE WHEN $4::boolean THEN $3::text ELSE '' END,
			conformance_report = $5::jsonb,
			conformed_at = CASE WHEN $4::boolean THEN $6::timestamptz ELSE NULL END,
			activated_at = NULL,
			updated_at = $6::timestamptz
		WHERE tenant_id = $1 AND id = $2::uuid
			AND manifest_hash = $3
			AND required_autonomy = $7
			AND risk_class = $8
			AND side_effect_class = $9
			AND requires_nexus_approval = $10
			AND evidence_required = $11
			AND rollback_capability_key = $12
			AND archived_at IS NULL AND trashed_at IS NULL
		RETURNING `+capabilitySelectColumns+`
	`, tenantID, id.String(), expected.ManifestHash, report.Conformant, raw, now,
		string(expected.RequiredAutonomy), expected.RiskClass, expected.SideEffectClass,
		expected.RequiresNexusApproval, expected.EvidenceRequired, expected.RollbackCapabilityKey)
	return scanCapability(row)
}

func (r *Repository) Activate(ctx context.Context, tenantID string, id uuid.UUID, manifestHash string) (domain.Capability, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		UPDATE capabilities
		SET promotion_state = 'active', activated_at = $4, updated_at = $4
		WHERE tenant_id = $1 AND id = $2::uuid
			AND promotion_state = 'conformant'
			AND manifest_hash = $3 AND conformed_hash = $3
			AND NOT EXISTS (
				SELECT 1
				FROM jsonb_array_elements_text(COALESCE(manifest->'quota_areas', '[]'::jsonb)) AS required(area)
				WHERE NOT EXISTS (
					SELECT 1 FROM quota_policies qp
					WHERE qp.tenant_id = capabilities.tenant_id
					  AND qp.product_surface = manifest->>'product_surface'
					  AND qp.area = required.area AND qp.active = true
				)
			)
			AND archived_at IS NULL AND trashed_at IS NULL
		RETURNING `+capabilitySelectColumns+`
	`, tenantID, id.String(), manifestHash, now)
	return scanCapability(row)
}

func (r *Repository) Archive(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE capabilities
		SET archived_at = $3, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), at.UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Unarchive(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE capabilities
		SET archived_at = NULL, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Purge(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM capabilities
		WHERE tenant_id = $1 AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, tenantID, resourceID.String())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error) {
	state, err := r.State(ctx, tenantID, resourceID)
	if err != nil {
		return false, err
	}
	return state == lifecycle.StateArchived, nil
}

func (r *Repository) Trash(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE capabilities
		SET archived_at = NULL, trashed_at = $3, purge_after = $4, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), at.UTC(), nullableTime(purgeAfter))
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE capabilities
		SET trashed_at = NULL, purge_after = NULL, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, tenantID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) State(ctx context.Context, tenantID string, resourceID uuid.UUID) (lifecycle.LifecycleState, error) {
	var archivedAt sql.NullTime
	var trashedAt sql.NullTime
	err := r.pool.QueryRow(ctx, `
		SELECT archived_at, trashed_at
		FROM capabilities
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, resourceID.String()).Scan(&archivedAt, &trashedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domainerr.NotFoundf("capability", resourceID.String())
	}
	if err != nil {
		return "", err
	}
	switch {
	case trashedAt.Valid:
		return lifecycle.StateTrashed, nil
	case archivedAt.Valid:
		return lifecycle.StateArchived, nil
	default:
		return lifecycle.StateActive, nil
	}
}

func (r *Repository) HasActiveVirployeeAssignments(ctx context.Context, tenantID string, id uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM virployee_capabilities vc
			JOIN virployees v ON v.id = vc.virployee_id
			WHERE v.tenant_id = $1
			  AND vc.capability_id = $2::uuid
			  AND v.archived_at IS NULL
			  AND v.trashed_at IS NULL
		)
	`, tenantID, id.String()).Scan(&exists)
	return exists, err
}

func (r *Repository) lifecycleResult(ctx context.Context, tenantID string, id uuid.UUID, tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, stateErr := r.State(ctx, tenantID, id); stateErr != nil {
		return stateErr
	}
	return domainerr.Conflict("invalid lifecycle transition")
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCapability(row scanner) (domain.Capability, error) {
	var idText string
	var requiredAutonomy string
	var promotionState string
	var manifestJSON, reportJSON []byte
	var model models.Capability
	err := row.Scan(
		&idText,
		&model.TenantID,
		&model.CapabilityKey,
		&model.Name,
		&model.Description,
		&requiredAutonomy,
		&model.RiskClass,
		&model.SideEffectClass,
		&model.RequiresNexusApproval,
		&model.EvidenceRequired,
		&model.RollbackCapabilityKey,
		&promotionState,
		&manifestJSON,
		&model.ManifestHash,
		&model.ConformedHash,
		&reportJSON,
		&model.ConformedAt,
		&model.ActivatedAt,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Capability{}, domainerr.NotFound("capability not found")
	}
	if err != nil {
		return domain.Capability{}, mapCapabilityConflict(err)
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.Capability{}, err
	}
	model.ID = id
	model.RequiredAutonomy = virployeedomain.AutonomyLevel(requiredAutonomy)
	model.PromotionState = domain.PromotionState(promotionState)
	if err := json.Unmarshal(manifestJSON, &model.Manifest); err != nil {
		return domain.Capability{}, err
	}
	if err := json.Unmarshal(reportJSON, &model.ConformanceReport); err != nil {
		return domain.Capability{}, err
	}
	return model.ToDomain(), nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func mapCapabilityConflict(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domainerr.Conflict("capability key already exists")
	}
	return err
}

var _ RepositoryPort = (*Repository)(nil)
