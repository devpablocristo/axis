package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/bff-v2/internal/identity/repository/models"
	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ensure(ctx context.Context, input domain.EnsureInput) (domain.User, error) {
	now := time.Now().UTC()
	id := input.ID
	if id == "" {
		id = uuid.NewString()
	}
	syncedAt := input.SyncedAt
	if syncedAt == nil {
		syncedAt = &now
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_users (id, provider, provider_user_id, email, status, synced_at, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (provider, provider_user_id) WHERE provider_user_id <> '' DO UPDATE SET
			email = COALESCE(NULLIF(EXCLUDED.email, ''), axis_users.email),
			status = EXCLUDED.status,
			synced_at = EXCLUDED.synced_at,
			updated_at = EXCLUDED.updated_at,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		RETURNING id, provider, provider_user_id, email, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
	`, id, input.Provider, input.ProviderUserID, input.Email, input.Status, syncedAt, now)
	return scanUser(row)
}

func (r *Repository) Get(ctx context.Context, id string) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, provider, provider_user_id, email, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_users
		WHERE id = $1::uuid
	`, id)
	return scanUser(row)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM axis_users
		WHERE id = $1::uuid
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.NotFound("user not found")
	}
	return nil
}

func (r *Repository) FindByProviderUserID(ctx context.Context, provider, providerUserID string) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, provider, provider_user_id, email, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_users
		WHERE provider = $1 AND provider_user_id = $2
	`, provider, strings.TrimSpace(providerUserID))
	return scanUser(row)
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, provider, provider_user_id, email, status, synced_at, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM axis_users
		WHERE lower(email) = lower($1)
		LIMIT 1
	`, strings.TrimSpace(email))
	return scanUser(row)
}

func (r *Repository) MarkDeletedByProviderUserID(ctx context.Context, provider, providerUserID string) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE axis_users
		SET status = 'deleted',
			updated_at = $3,
			synced_at = $3
		WHERE provider = $1 AND provider_user_id = $2
	`, provider, strings.TrimSpace(providerUserID), now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.NotFound("user not found")
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (domain.User, error) {
	var model models.User
	err := row.Scan(
		&model.ID,
		&model.Provider,
		&model.ProviderUserID,
		&model.Email,
		&model.Status,
		&model.SyncedAt,
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
