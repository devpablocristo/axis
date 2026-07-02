package tenancy

import (
	"context"
	"errors"
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
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_orgs (id, name, status, created_at, updated_at)
		VALUES ($1, $2, 'active', $3, $3)
		ON CONFLICT (id) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), axis_orgs.name),
			status = 'active',
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING id, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
	`, input.OrgID, input.Name, now)
	return scanOrg(row)
}

func (r *Repository) CreateTenant(ctx context.Context, input domain.NormalizedCreateTenantInput) (domain.Tenant, error) {
	now := time.Now().UTC()
	id := uuid.New()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_tenants (id, org_id, product_surface, name, status, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, 'active', $5, $5)
		ON CONFLICT (org_id, product_surface) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), axis_tenants.name),
			status = 'active',
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING id, org_id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
	`, id.String(), input.OrgID, input.ProductSurface, input.Name, now)
	return scanTenant(row)
}

func (r *Repository) TenantByID(ctx context.Context, id uuid.UUID) (domain.Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, org_id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_tenants
		WHERE id = $1::uuid
	`, id.String())
	return scanTenant(row)
}

func (r *Repository) ListForPrincipal(ctx context.Context, userID string) ([]domain.Tenant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.org_id, t.product_surface, t.name, t.status, t.created_at, t.updated_at, t.archived_at, t.trashed_at, t.purge_after
		FROM axis_tenants t
		JOIN axis_tenant_members m ON m.tenant_id = t.id
		WHERE m.user_id = $1
			AND m.status = 'active'
			AND m.archived_at IS NULL
			AND m.trashed_at IS NULL
			AND t.status = 'active'
			AND t.archived_at IS NULL
			AND t.trashed_at IS NULL
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
		SELECT id, org_id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_tenants
		WHERE ($1 = '' OR org_id = $1) AND archived_at IS NULL AND trashed_at IS NULL
		ORDER BY org_id, product_surface
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTenants(rows)
}

func (r *Repository) UpsertMember(ctx context.Context, input domain.NormalizedAddMemberInput) (domain.TenantMember, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_tenant_members (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, 'active', $4, $4)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET
			role = EXCLUDED.role,
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
		SELECT tenant_id, user_id, role, status, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_tenant_members
		WHERE tenant_id = $1::uuid AND user_id = $2
	`, tenantID.String(), userID)
	return scanMember(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOrg(row scanner) (domain.Org, error) {
	var model models.Org
	err := row.Scan(
		&model.ID,
		&model.Name,
		&model.Status,
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
		&model.ProductSurface,
		&model.Name,
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
