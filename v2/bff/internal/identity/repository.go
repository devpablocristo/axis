package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/bff-v2/internal/identity/repository/models"
	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ensure(ctx context.Context, input domain.EnsureInput) (domain.User, error) {
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_users (id, email, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', $4, $4)
		ON CONFLICT (id) DO UPDATE SET
			email = COALESCE(NULLIF(EXCLUDED.email, ''), axis_users.email),
			name = COALESCE(NULLIF(EXCLUDED.name, ''), axis_users.name),
			status = 'active',
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING id, email, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
	`, input.ID, input.Email, input.Name, now)
	return scanUser(row)
}

func (r *Repository) Get(ctx context.Context, id string) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, name, status, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_users
		WHERE id = $1
	`, id)
	return scanUser(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (domain.User, error) {
	var model models.User
	err := row.Scan(
		&model.ID,
		&model.Email,
		&model.Name,
		&model.Status,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domainerr.NotFound("user not found")
	}
	if err != nil {
		return domain.User{}, err
	}
	return model.ToDomain(), nil
}
