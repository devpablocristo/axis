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
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Virployee{}, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO virployees (id, tenant_id, name, job_role_id, description, supervisor_user_id, autonomy, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8, $8)
		RETURNING id::text, name, job_role_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
	`, id.String(), tenantID, input.Name, input.JobRoleID.String(), input.Description, input.SupervisorUserID, string(input.Autonomy), now)
	item, err := scanVirployee(row)
	if err != nil {
		return domain.Virployee{}, err
	}
	if err := replaceVirployeeCapabilities(ctx, tx, tenantID, id, input.CapabilityIDs); err != nil {
		return domain.Virployee{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Virployee{}, err
	}
	item.CapabilityIDs = input.CapabilityIDs
	return item, nil
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
		SELECT id::text, name, job_role_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
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
	return r.attachCapabilityIDs(ctx, tenantID, out)
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Virployee, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, name, job_role_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
		FROM virployees
		WHERE tenant_id = $1 AND id = $2::uuid AND trashed_at IS NULL
	`, tenantID, id.String())
	item, err := scanVirployee(row)
	if err != nil {
		return domain.Virployee{}, err
	}
	ids, err := r.capabilityIDs(ctx, tenantID, id)
	if err != nil {
		return domain.Virployee{}, err
	}
	item.CapabilityIDs = ids
	return item, nil
}

func (r *Repository) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Virployee{}, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		UPDATE virployees
		SET name = $3, job_role_id = $4::uuid, description = $5, supervisor_user_id = $6, autonomy = $7, updated_at = $8
		WHERE tenant_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING id::text, name, job_role_id::text, description, supervisor_user_id::text, autonomy, created_at, updated_at, archived_at, trashed_at, purge_after
	`, tenantID, id.String(), input.Name, input.JobRoleID.String(), input.Description, input.SupervisorUserID, string(input.Autonomy), time.Now().UTC())
	item, err := scanVirployee(row)
	if err == nil {
		if err := replaceVirployeeCapabilities(ctx, tx, tenantID, id, input.CapabilityIDs); err != nil {
			return domain.Virployee{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Virployee{}, err
		}
		item.CapabilityIDs = input.CapabilityIDs
		return item, nil
	}
	if !domainerr.IsNotFound(err) {
		return domain.Virployee{}, err
	}
	state, stateErr := r.State(ctx, tenantID, id)
	if stateErr != nil {
		return domain.Virployee{}, stateErr
	}
	if state != lifecycle.StateActive {
		return domain.Virployee{}, domainerr.Conflict("virployee is not active")
	}
	return domain.Virployee{}, err
}

func (r *Repository) Archive(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error {
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

func (r *Repository) Unarchive(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
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

func (r *Repository) Purge(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
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
	return state == lifecycle.StateArchived, nil
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

func (r *Repository) Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE virployees
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
		return lifecycle.StateTrashed, nil
	case archivedAt.Valid:
		return lifecycle.StateArchived, nil
	default:
		return lifecycle.StateActive, nil
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

func (r *Repository) attachCapabilityIDs(ctx context.Context, tenantID string, items []domain.Virployee) ([]domain.Virployee, error) {
	for i := range items {
		ids, err := r.capabilityIDs(ctx, tenantID, items[i].ID)
		if err != nil {
			return nil, err
		}
		items[i].CapabilityIDs = ids
	}
	return items, nil
}

func (r *Repository) capabilityIDs(ctx context.Context, tenantID string, virployeeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT capability_id::text
		FROM virployee_capabilities
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
		ORDER BY capability_id::text ASC
	`, tenantID, virployeeID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []uuid.UUID{}
	for rows.Next() {
		var idText string
		if err := rows.Scan(&idText); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(idText)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func replaceVirployeeCapabilities(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	virployeeID uuid.UUID,
	capabilityIDs []uuid.UUID,
) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM virployee_capabilities
		WHERE tenant_id = $1
			AND virployee_id = $2::uuid
	`, tenantID, virployeeID.String()); err != nil {
		return err
	}
	for _, capabilityID := range capabilityIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO virployee_capabilities (tenant_id, virployee_id, capability_id)
			VALUES ($1, $2::uuid, $3::uuid)
		`, tenantID, virployeeID.String(), capabilityID.String()); err != nil {
			return mapVirployeeStorageError(err)
		}
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanVirployee(row scanner) (domain.Virployee, error) {
	var idText string
	var jobRoleIDText string
	var supervisorUserIDText string
	var autonomyText string
	var model models.Virployee
	err := row.Scan(
		&idText,
		&model.Name,
		&jobRoleIDText,
		&model.Description,
		&supervisorUserIDText,
		&autonomyText,
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
		return domain.Virployee{}, mapVirployeeStorageError(err)
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.Virployee{}, err
	}
	jobRoleID, err := uuid.Parse(jobRoleIDText)
	if err != nil {
		return domain.Virployee{}, err
	}
	model.ID = id
	model.JobRoleID = jobRoleID
	model.SupervisorUserID = supervisorUserIDText
	model.Autonomy = domain.AutonomyLevel(autonomyText)
	return model.ToDomain(), nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func mapVirployeeStorageError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		switch pgErr.ConstraintName {
		case "virployees_job_role_id_fkey":
			return domainerr.Validation("job_role_id must reference an existing job role")
		case "virployee_capabilities_capability_id_fkey":
			return domainerr.Validation("capability_ids must reference existing capabilities")
		default:
			return domainerr.Validation("related resource does not exist")
		}
	}
	return err
}

var _ RepositoryPort = (*Repository)(nil)
