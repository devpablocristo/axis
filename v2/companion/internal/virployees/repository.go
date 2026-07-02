package virployees

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/companion-v2/internal/virployees/repository/models"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.Virployee, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO virployees (id, tenant_id, name, role, description, supervisor_user_id, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7, $7)
		RETURNING id::text, name, role, description, supervisor_user_id::text, created_at, updated_at, archived_at, trashed_at, purge_after
	`, id.String(), tenantID, input.Name, input.Role, input.Description, input.SupervisorUserID.String(), now)
	return scanVirployee(row)
}

func (r *Repository) List(ctx context.Context, tenantID string, state domain.State) ([]domain.Virployee, error) {
	where := "tenant_id = $1 AND archived_at IS NULL AND trashed_at IS NULL"
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

	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, name, role, description, supervisor_user_id::text, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM virployees
		WHERE %s
		ORDER BY created_at DESC, id DESC
	`, where), tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Virployee{}
	for rows.Next() {
		item, err := scanVirployee(rows)
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

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, name, role, description, supervisor_user_id::text, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid AND trashed_at IS NULL
	`, tenantID, id.String())
	return scanVirployee(row)
}

func (r *Repository) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE virployees
		SET name = $3, role = $4, description = $5, supervisor_user_id = $6::uuid, updated_at = $7
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING id::text, name, role, description, supervisor_user_id::text, created_at, updated_at, archived_at, trashed_at, purge_after
	`, tenantID, id.String(), input.Name, input.Role, input.Description, input.SupervisorUserID.String(), time.Now().UTC())
	item, err := scanVirployee(row)
	if err == nil {
		return item, nil
	}
	if !domainerr.IsNotFound(err) {
		return domain.Virployee{}, err
	}
	state, stateErr := r.State(ctx, tenantID, id)
	if stateErr != nil {
		return domain.Virployee{}, stateErr
	}
	if state != domain.StateActive {
		return domain.Virployee{}, domainerr.Conflict("virployee is not active")
	}
	return domain.Virployee{}, err
}

func (r *Repository) SoftDelete(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET archived_at = $3, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), at.UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET archived_at = NULL, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) HardDelete(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, resourceID.String())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error) {
	state, err := r.State(ctx, tenantID, resourceID)
	if err != nil {
		return false, err
	}
	return state == domain.StateArchived, nil
}

func (r *Repository) Trash(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET archived_at = NULL, trashed_at = $3, purge_after = $4, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND trashed_at IS NULL
	`, tenantID, resourceID.String(), at.UTC(), nullableTime(purgeAfter))
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) RestoreTrashed(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
		SET trashed_at = NULL, purge_after = NULL, updated_at = $3
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, tenantID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, tenantID, resourceID, tag, err)
}

func (r *Repository) State(ctx context.Context, tenantID string, resourceID uuid.UUID) (domain.State, error) {
	var archivedAt sql.NullTime
	var trashedAt sql.NullTime
	err := r.pool.QueryRow(ctx, `
		SELECT archived_at, trashed_at
		FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, resourceID.String()).Scan(&archivedAt, &trashedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domainerr.NotFoundf("virployee", resourceID.String())
	}
	if err != nil {
		return "", err
	}
	switch {
	case trashedAt.Valid:
		return domain.StateTrashed, nil
	case archivedAt.Valid:
		return domain.StateArchived, nil
	default:
		return domain.StateActive, nil
	}
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

func scanVirployee(row scanner) (domain.Virployee, error) {
	var idText string
	var supervisorUserIDText string
	var model models.Virployee
	err := row.Scan(
		&idText,
		&model.Name,
		&model.Role,
		&model.Description,
		&supervisorUserIDText,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Virployee{}, domainerr.NotFound("virployee not found")
	}
	if err != nil {
		return domain.Virployee{}, err
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.Virployee{}, err
	}
	supervisorUserID, err := uuid.Parse(supervisorUserIDText)
	if err != nil {
		return domain.Virployee{}, err
	}
	model.ID = id
	model.SupervisorUserID = supervisorUserID
	return model.ToDomain(), nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

var _ RepositoryPort = (*Repository)(nil)
