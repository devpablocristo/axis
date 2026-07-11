package products

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/bff-v2/internal/products/repository/models"
	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	postgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.Product, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_products
		WHERE id = $1::uuid
	`, id.String())
	return scanProduct(row)
}

func (r *Repository) List(ctx context.Context, input domain.NormalizedListInput) ([]domain.Product, error) {
	var where string
	switch input.Lifecycle {
	case domain.StateActive:
		where = "archived_at IS NULL AND trashed_at IS NULL"
	case domain.StateArchived:
		where = "archived_at IS NOT NULL AND trashed_at IS NULL"
	case domain.StateTrashed:
		where = "trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_products
		WHERE `+where+`
		ORDER BY name, product_surface
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProducts(rows)
}

func (r *Repository) Create(ctx context.Context, input domain.NormalizedCreateInput) (domain.Product, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_products (id, product_surface, name, status, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, 'active', $4, $4)
		RETURNING id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
	`, uuid.NewString(), input.ProductSurface, input.Name, now)
	out, err := scanProduct(row)
	if postgres.IsUniqueViolation(err) {
		return domain.Product{}, domainerr.Conflict("product_surface already exists")
	}
	return out, err
}

func (r *Repository) Update(ctx context.Context, input domain.NormalizedUpdateInput) (domain.Product, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		UPDATE axis_products
		SET name = $2,
			updated_at = $3
		WHERE id = $1::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING id, product_surface, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
	`, input.ProductID.String(), input.Name, now)
	return scanProduct(row)
}

func (r *Repository) Archive(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_products
		SET archived_at = $2,
			trashed_at = NULL,
			purge_after = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, input.ProductID.String(), now)
	return r.lifecycleResult(ctx, input.ProductID, tag, err)
}

func (r *Repository) Unarchive(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_products
		SET archived_at = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, input.ProductID.String(), now)
	return r.lifecycleResult(ctx, input.ProductID, tag, err)
}

func (r *Repository) Trash(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	purgeAfter := now.Add(30 * 24 * time.Hour)
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_products
		SET archived_at = NULL,
			trashed_at = $2,
			purge_after = $3,
			updated_at = $2
		WHERE id = $1::uuid
			AND trashed_at IS NULL
	`, input.ProductID.String(), now, purgeAfter)
	return r.lifecycleResult(ctx, input.ProductID, tag, err)
}

func (r *Repository) Restore(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_products
		SET trashed_at = NULL,
			purge_after = NULL,
			updated_at = $2
		WHERE id = $1::uuid
			AND trashed_at IS NOT NULL
	`, input.ProductID.String(), now)
	return r.lifecycleResult(ctx, input.ProductID, tag, err)
}

func (r *Repository) Purge(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM axis_products
		WHERE id = $1::uuid
			AND trashed_at IS NOT NULL
	`, input.ProductID.String())
	return r.lifecycleResult(ctx, input.ProductID, tag, err)
}

func (r *Repository) IsProductInUse(ctx context.Context, productSurface string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM axis_tenants
			WHERE product_surface = $1
		)
	`, productSurface).Scan(&exists)
	return exists, err
}

func (r *Repository) ActiveProductExists(ctx context.Context, productSurface string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM axis_products
			WHERE product_surface = $1
				AND status = 'active'
				AND archived_at IS NULL
				AND trashed_at IS NULL
		)
	`, productSurface).Scan(&exists)
	return exists, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProduct(row scanner) (domain.Product, error) {
	var model models.Product
	err := row.Scan(
		&model.ID,
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
		return domain.Product{}, domainerr.NotFound("product not found")
	}
	if err != nil {
		return domain.Product{}, err
	}
	return model.ToDomain(), nil
}

func scanProducts(rows pgx.Rows) ([]domain.Product, error) {
	out := []domain.Product{}
	for rows.Next() {
		item, err := scanProduct(rows)
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

func (r *Repository) lifecycleResult(ctx context.Context, id uuid.UUID, tag pgconnCommandTag, err error) error {
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
