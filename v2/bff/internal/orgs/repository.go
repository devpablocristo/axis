package orgs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/bff-v2/internal/orgs/repository/models"
	"github.com/devpablocristo/bff-v2/internal/orgs/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ensure(ctx context.Context, input domain.EnsureInput) (domain.Org, error) {
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
		WITH upsert AS (
		INSERT INTO axis_orgs (id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (provider, provider_org_id) WHERE provider_org_id <> '' DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), axis_orgs.name),
			slug = COALESCE(NULLIF(EXCLUDED.slug, ''), axis_orgs.slug),
			status = EXCLUDED.status,
			synced_at = EXCLUDED.synced_at,
			updated_at = EXCLUDED.updated_at
		RETURNING id, provider, provider_org_id, name, slug, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
		)
		SELECT u.id, u.provider, u.provider_org_id, u.name, u.slug, u.status, u.synced_at,
			(SELECT COUNT(*)::int FROM axis_products p WHERE p.org_id = u.id),
			u.created_at, u.updated_at, u.archived_at, u.trashed_at, u.purge_after
		FROM upsert u
	`, id, input.Provider, input.ProviderOrgID, input.Name, input.Slug, input.Status, syncedAt, now)
	return scanOrg(row)
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Org, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT o.id, o.provider, o.provider_org_id, o.name, o.slug, o.status, o.synced_at,
			(SELECT COUNT(*)::int FROM axis_products p WHERE p.org_id = o.id),
			o.created_at, o.updated_at, o.archived_at, o.trashed_at, o.purge_after
		FROM axis_orgs o
		WHERE o.id = $1::uuid
	`, id)
	return scanOrg(row)
}

func (r *Repository) List(ctx context.Context, input domain.NormalizedListInput) ([]domain.Org, error) {
	var where string
	switch input.Lifecycle {
	case domain.StateActive:
		where = "o.archived_at IS NULL AND o.trashed_at IS NULL"
	case domain.StateArchived:
		where = "o.archived_at IS NOT NULL AND o.trashed_at IS NULL"
	case domain.StateTrashed:
		where = "o.trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT o.id, o.provider, o.provider_org_id, o.name, o.slug, o.status, o.synced_at,
			(SELECT COUNT(*)::int FROM axis_products p WHERE p.org_id = o.id),
			o.created_at, o.updated_at, o.archived_at, o.trashed_at, o.purge_after
		FROM axis_orgs o
		WHERE `+where+`
		ORDER BY o.name, o.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrgs(rows)
}

func (r *Repository) Archive(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_orgs
		SET archived_at = $2,
			trashed_at = NULL,
			purge_after = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, input.OrgID, now)
	return r.lifecycleResult(ctx, input.OrgID, tag, err)
}

func (r *Repository) Unarchive(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_orgs
		SET archived_at = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, input.OrgID, now)
	return r.lifecycleResult(ctx, input.OrgID, tag, err)
}

func (r *Repository) Trash(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	purgeAfter := now.Add(30 * 24 * time.Hour)
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_orgs
		SET archived_at = NULL,
			trashed_at = $2,
			purge_after = $3,
			updated_at = $2
		WHERE id = $1::uuid
			AND trashed_at IS NULL
	`, input.OrgID, now, purgeAfter)
	return r.lifecycleResult(ctx, input.OrgID, tag, err)
}

func (r *Repository) Restore(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_orgs
		SET trashed_at = NULL,
			purge_after = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND trashed_at IS NOT NULL
	`, input.OrgID, now)
	return r.lifecycleResult(ctx, input.OrgID, tag, err)
}

func (r *Repository) Purge(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM axis_orgs
		WHERE id = $1::uuid
			AND trashed_at IS NOT NULL
	`, input.OrgID)
	return r.lifecycleResult(ctx, input.OrgID, tag, err)
}

func (r *Repository) HasProducts(ctx context.Context, orgID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM axis_products
			WHERE org_id = $1::uuid
		)
	`, orgID).Scan(&exists)
	return exists, err
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
		&model.ProductCount,
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

func scanOrgs(rows pgx.Rows) ([]domain.Org, error) {
	out := []domain.Org{}
	for rows.Next() {
		item, err := scanOrg(rows)
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

func (r *Repository) lifecycleResult(ctx context.Context, id string, tag pgconnCommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, stateErr := r.Get(ctx, id); stateErr != nil {
		return stateErr
	}
	return domainerr.Conflict("invalid lifecycle transition")
}

type pgconnCommandTag interface {
	RowsAffected() int64
}
