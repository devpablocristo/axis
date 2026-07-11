package tenancy

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/bff-v2/internal/tenancy/repository/models"
	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) EnsureOrg(ctx context.Context, input domain.EnsureOrgInput) (domain.Org, error) {
	now := time.Now().UTC()
	id := input.OrgID
	if id == "" {
		id = uuid.NewString()
	}
	syncedAt := input.SyncedAt
	if syncedAt == nil {
		syncedAt = &now
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_orgs (id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (provider, provider_org_id) WHERE provider_org_id <> '' DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), axis_orgs.name),
			slug = COALESCE(NULLIF(EXCLUDED.slug, ''), axis_orgs.slug),
			status = EXCLUDED.status,
			synced_at = EXCLUDED.synced_at,
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
	`, id, input.Provider, input.ProviderOrgID, input.Name, input.Slug, input.Status, syncedAt, now)
	return scanOrg(row)
}

func (r *Repository) OrgByID(ctx context.Context, id string) (domain.Org, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_orgs
		WHERE id = $1::uuid
	`, strings.TrimSpace(id))
	return scanOrg(row)
}

func (r *Repository) OrgByProvider(ctx context.Context, provider, providerOrgID string) (domain.Org, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_orgs
		WHERE provider = $1 AND provider_org_id = $2
	`, strings.TrimSpace(provider), strings.TrimSpace(providerOrgID))
	return scanOrg(row)
}

func (r *Repository) DeleteOrg(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM axis_orgs
		WHERE id = $1::uuid
	`, strings.TrimSpace(id))
	return err
}

func (r *Repository) CreateTenant(ctx context.Context, input domain.NormalizedCreateTenantInput) (domain.Tenant, error) {
	now := time.Now().UTC()
	id := uuid.New()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_tenants (id, org_id, product_surface, status, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, 'active', $4, $4)
		ON CONFLICT (org_id, product_surface) DO UPDATE SET
			status = 'active',
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING id,
			org_id,
			(SELECT name FROM axis_orgs WHERE axis_orgs.id = axis_tenants.org_id),
			product_surface,
			COALESCE((SELECT name FROM axis_products WHERE axis_products.product_surface = axis_tenants.product_surface), product_surface),
			status,
			created_at,
			updated_at,
			archived_at,
			trashed_at,
			purge_after
	`, id.String(), input.OrgID, input.ProductSurface, now)
	return scanTenant(row)
}

func (r *Repository) HasOtherOrgTenants(ctx context.Context, orgID string, excludedTenantID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM axis_tenants
			WHERE org_id = $1::uuid
				AND id <> $2::uuid
		)
	`, strings.TrimSpace(orgID), excludedTenantID.String()).Scan(&exists)
	return exists, err
}

func (r *Repository) TenantByID(ctx context.Context, id uuid.UUID) (domain.Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT t.id, t.org_id, o.name, t.product_surface, COALESCE(p.name, t.product_surface), t.status, t.created_at, t.updated_at, t.archived_at, t.trashed_at, t.purge_after
		FROM axis_tenants t
		JOIN axis_orgs o ON o.id = t.org_id
		LEFT JOIN axis_products p ON p.product_surface = t.product_surface
		WHERE t.id = $1::uuid
	`, id.String())
	return scanTenant(row)
}

func (r *Repository) ListForPrincipal(ctx context.Context, userID string) ([]domain.Tenant, error) {
	return r.ListForPrincipalLifecycle(ctx, userID, domain.StateActive)
}

func (r *Repository) ListLifecycle(ctx context.Context, lifecycle string) ([]domain.Tenant, error) {
	var where string
	switch domain.NormalizeState(lifecycle) {
	case domain.StateActive:
		where = "t.archived_at IS NULL AND t.trashed_at IS NULL"
	case domain.StateArchived:
		where = "t.archived_at IS NOT NULL AND t.trashed_at IS NULL"
	case domain.StateTrashed:
		where = "t.trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.org_id, o.name, t.product_surface, COALESCE(p.name, t.product_surface), t.status, t.created_at, t.updated_at, t.archived_at, t.trashed_at, t.purge_after
		FROM axis_tenants t
		JOIN axis_orgs o ON o.id = t.org_id
		LEFT JOIN axis_products p ON p.product_surface = t.product_surface
		WHERE `+where+`
		ORDER BY o.name, t.product_surface
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenants(rows)
}

func (r *Repository) ListForPrincipalLifecycle(ctx context.Context, userID, lifecycle string) ([]domain.Tenant, error) {
	var where string
	switch domain.NormalizeState(lifecycle) {
	case domain.StateActive:
		where = "t.archived_at IS NULL AND t.trashed_at IS NULL"
	case domain.StateArchived:
		where = "t.archived_at IS NOT NULL AND t.trashed_at IS NULL"
	case domain.StateTrashed:
		where = "t.trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.org_id, o.name, t.product_surface, COALESCE(p.name, t.product_surface), t.status, t.created_at, t.updated_at, t.archived_at, t.trashed_at, t.purge_after
		FROM axis_tenants t
		JOIN axis_orgs o ON o.id = t.org_id
		LEFT JOIN axis_products p ON p.product_surface = t.product_surface
		JOIN axis_tenant_members m ON m.tenant_id = t.id
		JOIN axis_users u ON u.id = m.user_id
		WHERE (m.user_id::text = $1 OR u.provider_user_id = $1)
			AND m.status = 'active'
			AND m.archived_at IS NULL
			AND m.trashed_at IS NULL
			AND `+where+`
		ORDER BY t.org_id, t.product_surface
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenants(rows)
}

func (r *Repository) List(ctx context.Context, orgID string) ([]domain.Tenant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.org_id, o.name, t.product_surface, COALESCE(p.name, t.product_surface), t.status, t.created_at, t.updated_at, t.archived_at, t.trashed_at, t.purge_after
		FROM axis_tenants t
		JOIN axis_orgs o ON o.id = t.org_id
		LEFT JOIN axis_products p ON p.product_surface = t.product_surface
		WHERE ($1 = '' OR t.org_id::text = $1) AND t.archived_at IS NULL AND t.trashed_at IS NULL
		ORDER BY org_id, product_surface
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenants(rows)
}

func (r *Repository) ArchiveTenant(ctx context.Context, id uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_tenants
		SET archived_at = $2,
			trashed_at = NULL,
			purge_after = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, id.String(), at.UTC())
	return r.lifecycleResult(ctx, id, tag, err)
}

func (r *Repository) UnarchiveTenant(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_tenants
		SET archived_at = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, id.String(), now)
	return r.lifecycleResult(ctx, id, tag, err)
}

func (r *Repository) TrashTenant(ctx context.Context, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_tenants
		SET archived_at = NULL,
			trashed_at = $2,
			purge_after = $3,
			updated_at = $2
		WHERE id = $1::uuid
			AND trashed_at IS NULL
	`, id.String(), at.UTC(), nullableTime(purgeAfter))
	return r.lifecycleResult(ctx, id, tag, err)
}

func (r *Repository) RestoreTenant(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_tenants
		SET trashed_at = NULL,
			purge_after = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND trashed_at IS NOT NULL
	`, id.String(), now)
	return r.lifecycleResult(ctx, id, tag, err)
}

func (r *Repository) PurgeTenant(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM axis_tenants
		WHERE id = $1::uuid
			AND trashed_at IS NOT NULL
	`, id.String())
	return r.lifecycleResult(ctx, id, tag, err)
}

func (r *Repository) UpsertMember(ctx context.Context, input domain.NormalizedAddMemberInput) (domain.TenantMember, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_tenant_members (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, 'active', $4, $4)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET
			role = CASE
				WHEN axis_tenant_members.role = 'owner' AND EXCLUDED.role <> 'owner'
					THEN axis_tenant_members.role
				ELSE EXCLUDED.role
			END,
			status = 'active',
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING tenant_id, user_id, role, status, created_at, updated_at, archived_at, trashed_at, purge_after
	`, input.TenantID.String(), input.UserID, input.Role, now)
	return scanMember(row)
}

func (r *Repository) TenantMembership(ctx context.Context, tenantID uuid.UUID, userID string) (domain.TenantMember, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT m.tenant_id, m.user_id, m.role, m.status, m.created_at, m.updated_at, m.archived_at, m.trashed_at, m.purge_after
		FROM axis_tenant_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.tenant_id = $1::uuid AND (m.user_id::text = $2 OR u.provider_user_id = $2)
	`, tenantID.String(), userID)
	return scanMember(row)
}

func (r *Repository) PrincipalHasOwnerRole(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM axis_tenant_members m
			JOIN axis_users u ON u.id = m.user_id
			JOIN axis_tenants t ON t.id = m.tenant_id
			WHERE (m.user_id::text = $1 OR u.provider_user_id = $1)
				AND m.role = 'owner'
				AND m.status = 'active'
				AND m.archived_at IS NULL
				AND m.trashed_at IS NULL
				AND t.status = 'active'
				AND t.archived_at IS NULL
				AND t.trashed_at IS NULL
		)
	`, strings.TrimSpace(userID)).Scan(&exists)
	return exists, err
}

func (r *Repository) DeactivateUserMemberships(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		UPDATE axis_tenant_members
		SET status = 'inactive',
			archived_at = COALESCE(archived_at, $2),
			updated_at = $2
		WHERE user_id = $1::uuid
			AND status = 'active'
			AND trashed_at IS NULL
	`, strings.TrimSpace(userID), now)
	return err
}

func (r *Repository) DeactivateOrgUserMemberships(ctx context.Context, orgID, userID string) error {
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		UPDATE axis_tenant_members m
		SET status = 'inactive',
			archived_at = COALESCE(m.archived_at, $3),
			updated_at = $3
		FROM axis_tenants t
		WHERE m.tenant_id = t.id
			AND t.org_id = $1::uuid
			AND m.user_id = $2::uuid
			AND m.status = 'active'
			AND m.trashed_at IS NULL
	`, strings.TrimSpace(orgID), strings.TrimSpace(userID), now)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOrg(row scanner) (domain.Org, error) {
	var model models.Org
	err := row.Scan(
		&model.ID,
		&model.Provider,
		&model.ProviderOrgID,
		&model.Name,
		&model.Slug,
		&model.Status,
		&model.SyncedAt,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Org{}, domainerr.NotFound("org not found")
	}
	if err != nil {
		return domain.Org{}, err
	}
	return model.ToDomain(), nil
}

func scanTenant(row scanner) (domain.Tenant, error) {
	var model models.Tenant
	err := row.Scan(
		&model.ID,
		&model.OrgID,
		&model.OrgName,
		&model.ProductSurface,
		&model.ProductName,
		&model.Status,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tenant{}, domainerr.NotFound("tenant not found")
	}
	if err != nil {
		return domain.Tenant{}, err
	}
	return model.ToDomain(), nil
}

func scanTenants(rows pgx.Rows) ([]domain.Tenant, error) {
	out := []domain.Tenant{}
	for rows.Next() {
		item, err := scanTenant(rows)
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

func scanMember(row scanner) (domain.TenantMember, error) {
	var model models.TenantMember
	err := row.Scan(
		&model.TenantID,
		&model.UserID,
		&model.Role,
		&model.Status,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TenantMember{}, domainerr.NotFound("tenant membership not found")
	}
	if err != nil {
		return domain.TenantMember{}, err
	}
	return model.ToDomain(), nil
}

func (r *Repository) lifecycleResult(ctx context.Context, id uuid.UUID, tag pgconnCommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, stateErr := r.TenantByID(ctx, id); stateErr != nil {
		return stateErr
	}
	return domainerr.Conflict("invalid lifecycle transition")
}

type pgconnCommandTag interface {
	RowsAffected() int64
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}
