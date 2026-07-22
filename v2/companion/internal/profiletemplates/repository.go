package profiletemplates

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/companion-v2/internal/profiletemplates/repository/models"
	"github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
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

func (r *Repository) Create(ctx context.Context, orgID string, input domain.NormalizedCreateInput) (domain.ProfileTemplate, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO profile_templates (
			id, org_id, name, description, system_prompt, max_autonomy, created_at, updated_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id::text, org_id, name, description, system_prompt, max_autonomy,
			created_at, updated_at, archived_at, trashed_at, purge_after
	`, id.String(), orgID, input.Name, input.Description, input.SystemPrompt, string(input.MaxAutonomy), now)
	return scanProfileTemplate(row)
}

func (r *Repository) List(ctx context.Context, orgID string, state domain.State) ([]domain.ProfileTemplate, error) {
	var where string
	switch state {
	case domain.StateActive, "":
		where = "org_id = $1 AND archived_at IS NULL AND trashed_at IS NULL"
	case domain.StateArchived:
		where = "org_id = $1 AND archived_at IS NOT NULL AND trashed_at IS NULL"
	case domain.StateTrashed:
		where = "org_id = $1 AND trashed_at IS NOT NULL"
	default:
		return nil, domainerr.Validation("invalid lifecycle state")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, org_id, name, description, system_prompt, max_autonomy,
			created_at, updated_at, archived_at, trashed_at, purge_after
		FROM profile_templates
		WHERE `+where+`
		ORDER BY name ASC, id ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.ProfileTemplate{}
	for rows.Next() {
		item, err := scanProfileTemplate(rows)
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

func (r *Repository) Get(ctx context.Context, orgID string, id uuid.UUID) (domain.ProfileTemplate, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, org_id, name, description, system_prompt, max_autonomy,
			created_at, updated_at, archived_at, trashed_at, purge_after
		FROM profile_templates
		WHERE org_id = $1 AND id = $2::uuid AND trashed_at IS NULL
	`, orgID, id.String())
	return scanProfileTemplate(row)
}

func (r *Repository) Update(ctx context.Context, orgID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ProfileTemplate, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE profile_templates
		SET name = $3,
			description = $4,
			system_prompt = $5,
			max_autonomy = $6,
			updated_at = $7
		WHERE org_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING id::text, org_id, name, description, system_prompt, max_autonomy,
			created_at, updated_at, archived_at, trashed_at, purge_after
	`, orgID, id.String(), input.Name, input.Description, input.SystemPrompt, string(input.MaxAutonomy), time.Now().UTC())
	item, err := scanProfileTemplate(row)
	if err == nil {
		return item, nil
	}
	if !domainerr.IsNotFound(err) {
		return domain.ProfileTemplate{}, err
	}
	state, stateErr := r.State(ctx, orgID, id)
	if stateErr != nil {
		return domain.ProfileTemplate{}, stateErr
	}
	if state != lifecycle.StateActive {
		return domain.ProfileTemplate{}, domainerr.Conflict("profile is not active")
	}
	return domain.ProfileTemplate{}, err
}

func (r *Repository) Archive(ctx context.Context, orgID string, resourceID uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE profile_templates
		SET archived_at = $3, updated_at = $3
		WHERE org_id = $1
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, orgID, resourceID.String(), at.UTC())
	return r.lifecycleResult(ctx, orgID, resourceID, tag, err)
}

func (r *Repository) Unarchive(ctx context.Context, orgID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE profile_templates
		SET archived_at = NULL, updated_at = $3
		WHERE org_id = $1
			AND id = $2::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, orgID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, orgID, resourceID, tag, err)
}

func (r *Repository) Purge(ctx context.Context, orgID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM profile_templates
		WHERE org_id = $1 AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, orgID, resourceID.String())
	return r.lifecycleResult(ctx, orgID, resourceID, tag, err)
}

func (r *Repository) IsArchived(ctx context.Context, orgID string, resourceID uuid.UUID) (bool, error) {
	state, err := r.State(ctx, orgID, resourceID)
	if err != nil {
		return false, err
	}
	return state == lifecycle.StateArchived, nil
}

func (r *Repository) Trash(ctx context.Context, orgID string, resourceID uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE profile_templates
		SET archived_at = NULL, trashed_at = $3, purge_after = $4, updated_at = $3
		WHERE org_id = $1
			AND id = $2::uuid
			AND trashed_at IS NULL
	`, orgID, resourceID.String(), at.UTC(), nullableTime(purgeAfter))
	return r.lifecycleResult(ctx, orgID, resourceID, tag, err)
}

func (r *Repository) Restore(ctx context.Context, orgID string, resourceID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE profile_templates
		SET trashed_at = NULL, purge_after = NULL, updated_at = $3
		WHERE org_id = $1
			AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, orgID, resourceID.String(), time.Now().UTC())
	return r.lifecycleResult(ctx, orgID, resourceID, tag, err)
}

func (r *Repository) State(ctx context.Context, orgID string, resourceID uuid.UUID) (lifecycle.LifecycleState, error) {
	var archivedAt sql.NullTime
	var trashedAt sql.NullTime
	err := r.pool.QueryRow(ctx, `
		SELECT archived_at, trashed_at
		FROM profile_templates
		WHERE org_id = $1 AND id = $2::uuid
	`, orgID, resourceID.String()).Scan(&archivedAt, &trashedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domainerr.NotFoundf("profile", resourceID.String())
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

func (r *Repository) HasActiveVirployeeAssignments(ctx context.Context, orgID string, id uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM virployees v
			WHERE v.org_id = $1
			  AND v.profile_template_id = $2::uuid
			  AND v.archived_at IS NULL
			  AND v.trashed_at IS NULL
		)
	`, orgID, id.String()).Scan(&exists)
	return exists, err
}

func (r *Repository) lifecycleResult(ctx context.Context, orgID string, id uuid.UUID, tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, stateErr := r.State(ctx, orgID, id); stateErr != nil {
		return stateErr
	}
	return domainerr.Conflict("invalid lifecycle transition")
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProfileTemplate(row scanner) (domain.ProfileTemplate, error) {
	var idText string
	var maxAutonomy string
	var model models.ProfileTemplate
	err := row.Scan(
		&idText,
		&model.OrgID,
		&model.Name,
		&model.Description,
		&model.SystemPrompt,
		&maxAutonomy,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProfileTemplate{}, domainerr.NotFound("profile not found")
	}
	if err != nil {
		return domain.ProfileTemplate{}, err
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.ProfileTemplate{}, err
	}
	model.ID = id
	model.MaxAutonomy = virployeedomain.AutonomyLevel(maxAutonomy)
	return model.ToDomain(), nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

var _ RepositoryPort = (*Repository)(nil)
