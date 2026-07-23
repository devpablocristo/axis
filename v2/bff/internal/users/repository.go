package users

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devpablocristo/bff-v2/internal/users/repository/models"
	"github.com/devpablocristo/bff-v2/internal/users/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Get(ctx context.Context, orgID uuid.UUID, userID string) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT 'user',
			u.id::text,
			u.email,
			m.role,
			m.org_id,
			CASE
				WHEN m.trashed_at IS NOT NULL THEN 'trashed'
				WHEN m.archived_at IS NOT NULL THEN 'archived'
				ELSE 'active'
			END,
			m.created_at,
			m.updated_at,
			m.archived_at,
			m.trashed_at,
			m.purge_after
		FROM axis_org_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.org_id = $1::uuid
			AND m.user_id = $2::uuid
	`, orgID.String(), userID)
	return scanUser(row)
}

func (r *Repository) List(ctx context.Context, input domain.NormalizedListInput) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT 'user' AS kind,
			u.id::text AS id,
			u.email,
			m.role,
			m.org_id,
			CASE
				WHEN m.trashed_at IS NOT NULL THEN 'trashed'
				WHEN m.archived_at IS NOT NULL THEN 'archived'
				ELSE 'active'
			END AS state,
			m.created_at,
			m.updated_at,
			m.archived_at,
			m.trashed_at,
			m.purge_after
		FROM axis_org_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.org_id = $1::uuid
			AND u.status = 'active'
			AND u.archived_at IS NULL
			AND u.trashed_at IS NULL
			AND (
				($2 = 'active' AND m.status = 'active' AND m.archived_at IS NULL AND m.trashed_at IS NULL)
				OR ($2 = 'archived' AND m.archived_at IS NOT NULL AND m.trashed_at IS NULL)
				OR ($2 = 'trashed' AND m.trashed_at IS NOT NULL)
			)
		UNION ALL
		SELECT 'invitation' AS kind,
			'invitation:' || i.id::text AS id,
			i.email,
			i.role,
			i.org_id,
			CASE
				WHEN i.trashed_at IS NOT NULL THEN 'trashed'
				WHEN i.archived_at IS NOT NULL THEN 'archived'
				ELSE 'pending'
			END AS state,
			i.created_at,
			i.updated_at,
			i.archived_at,
			i.trashed_at,
			i.purge_after
		FROM axis_user_invitations i
		WHERE i.org_id = $1::uuid
			AND (
				($2 = 'active' AND i.status = 'pending' AND i.archived_at IS NULL AND i.trashed_at IS NULL)
				OR ($2 = 'archived' AND i.archived_at IS NOT NULL AND i.trashed_at IS NULL)
				OR ($2 = 'trashed' AND i.trashed_at IS NOT NULL)
			)
		ORDER BY email, id
	`, input.OrgID.String(), input.State)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

func (r *Repository) UpsertMembership(ctx context.Context, input UpsertMembershipInput) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.upsertMembership(ctx, tx, input.OrgID, input.UserID, input.Role); err != nil {
		return domain.User{}, err
	}
	if err := r.resolvePendingInvitation(ctx, tx, input.OrgID, input.Email); err != nil {
		return domain.User{}, err
	}
	out, err := r.getWithTx(ctx, tx, input.OrgID, input.UserID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return out, nil
}

func (r *Repository) UpsertInvitation(ctx context.Context, input UpsertInvitationInput) (domain.User, error) {
	now := time.Now().UTC()
	invitationID := uuid.New()
	status := strings.TrimSpace(strings.ToLower(input.Status))
	if status == "" {
		status = domain.StatePending
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO axis_user_invitations (
			id,
			org_id,
			provider,
			provider_invitation_id,
			email,
			role,
			status,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (org_id, lower(email)) WHERE status = 'pending' AND archived_at IS NULL AND trashed_at IS NULL
		DO UPDATE SET
			role = EXCLUDED.role,
			provider = EXCLUDED.provider,
			provider_invitation_id = COALESCE(NULLIF(EXCLUDED.provider_invitation_id, ''), axis_user_invitations.provider_invitation_id),
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
		RETURNING 'invitation',
			'invitation:' || id::text,
			email,
			role,
			org_id,
			'pending',
			created_at,
			updated_at,
			archived_at,
			trashed_at,
			purge_after
	`, invitationID.String(), input.OrgID.String(), input.Provider, input.ProviderInvitationID, input.Email, input.Role, status, now)
	return scanUser(row)
}

func (r *Repository) Update(ctx context.Context, input domain.NormalizedUpdateInput) (domain.User, error) {
	if domain.KindFromID(input.UserID) == domain.KindInvitation {
		return r.updateInvitation(ctx, input)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := r.activeMembershipWithTx(ctx, tx, input.OrgID, input.UserID); err != nil {
		return domain.User{}, err
	}
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE axis_users
		SET email = $2,
			status = 'active',
			updated_at = $3,
			archived_at = NULL,
			trashed_at = NULL,
			purge_after = NULL
		WHERE id = $1::uuid
	`, input.UserID, input.Email, now)
	if err != nil {
		return domain.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.User{}, domainerr.NotFound("user not found")
	}
	tag, err = tx.Exec(ctx, `
		UPDATE axis_org_members
		SET role = $3,
			status = 'active',
			updated_at = $4
		WHERE org_id = $1::uuid
			AND user_id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, input.OrgID.String(), input.UserID, input.Role, now)
	if err != nil {
		return domain.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.User{}, domainerr.NotFound("organization user not found")
	}
	out, err := r.getWithTx(ctx, tx, input.OrgID, input.UserID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return out, nil
}

func (r *Repository) Archive(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	return r.lifecycleMembershipOrInvitation(ctx, input, `
		UPDATE axis_org_members
		SET archived_at = $3,
			trashed_at = NULL,
			purge_after = NULL,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND user_id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, `
		UPDATE axis_user_invitations
		SET archived_at = $3,
			trashed_at = NULL,
			purge_after = NULL,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND id = $2::uuid
			AND archived_at IS NULL
			AND trashed_at IS NULL
	`, now, nil)
}

func (r *Repository) Unarchive(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	return r.lifecycleMembershipOrInvitation(ctx, input, `
		UPDATE axis_org_members
		SET archived_at = NULL,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND user_id = $2::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, `
		UPDATE axis_user_invitations
		SET archived_at = NULL,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND id = $2::uuid
			AND archived_at IS NOT NULL
			AND trashed_at IS NULL
	`, now, nil)
}

func (r *Repository) Trash(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	purgeAfter := now.Add(30 * 24 * time.Hour)
	return r.lifecycleMembershipOrInvitation(ctx, input, `
		UPDATE axis_org_members
		SET archived_at = NULL,
			trashed_at = $3,
			purge_after = $4,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND user_id = $2::uuid
			AND trashed_at IS NULL
	`, `
		UPDATE axis_user_invitations
		SET archived_at = NULL,
			trashed_at = $3,
			purge_after = $4,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND id = $2::uuid
			AND trashed_at IS NULL
	`, now, &purgeAfter)
}

func (r *Repository) Restore(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	now := time.Now().UTC()
	return r.lifecycleMembershipOrInvitation(ctx, input, `
		UPDATE axis_org_members
		SET trashed_at = NULL,
			purge_after = NULL,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND user_id = $2::uuid
			AND trashed_at IS NOT NULL
	`, `
		UPDATE axis_user_invitations
		SET trashed_at = NULL,
			purge_after = NULL,
			updated_at = $3
		WHERE org_id = $1::uuid
			AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, now, nil)
}

func (r *Repository) Purge(ctx context.Context, input domain.NormalizedLifecycleInput) error {
	return r.lifecycleMembershipOrInvitation(ctx, input, `
		DELETE FROM axis_org_members
		WHERE org_id = $1::uuid
			AND user_id = $2::uuid
			AND trashed_at IS NOT NULL
	`, `
		DELETE FROM axis_user_invitations
		WHERE org_id = $1::uuid
			AND id = $2::uuid
			AND trashed_at IS NOT NULL
	`, time.Time{}, nil)
}

func (r *Repository) ActiveMembershipExists(ctx context.Context, input domain.NormalizedEnsureActiveInput) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM axis_org_members m
			JOIN axis_users u ON u.id = m.user_id
			WHERE m.org_id = $1::uuid
				AND m.user_id = $2::uuid
				AND m.status = 'active'
				AND m.archived_at IS NULL
				AND m.trashed_at IS NULL
				AND u.status = 'active'
				AND u.archived_at IS NULL
				AND u.trashed_at IS NULL
		)
	`, input.OrgID.String(), input.UserID).Scan(&exists)
	return exists, err
}

func (r *Repository) upsertMembership(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, userID, role string) error {
	now := time.Now().UTC()
	_, err := tx.Exec(ctx, `
		INSERT INTO axis_org_members (org_id, user_id, role, status, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, 'active', $4, $4)
		ON CONFLICT (org_id, user_id) DO UPDATE SET
			role = EXCLUDED.role,
			status = 'active',
			updated_at = EXCLUDED.updated_at
	`, orgID.String(), userID, role, now)
	return err
}

func (r *Repository) resolvePendingInvitation(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, email string) error {
	now := time.Now().UTC()
	_, err := tx.Exec(ctx, `
		UPDATE axis_user_invitations
		SET status = 'accepted',
			updated_at = $3,
			archived_at = NULL,
			trashed_at = $3,
			purge_after = $4
		WHERE org_id = $1::uuid
			AND lower(email) = lower($2)
			AND status = 'pending'
	`, orgID.String(), email, now, now.Add(30*24*time.Hour))
	return err
}

func (r *Repository) activeMembershipWithTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, userID string) (domain.User, error) {
	row := tx.QueryRow(ctx, `
		SELECT 'user',
			u.id::text,
			u.email,
			m.role,
			m.org_id,
			'active',
			m.created_at,
			m.updated_at,
			m.archived_at,
			m.trashed_at,
			m.purge_after
		FROM axis_org_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.org_id = $1::uuid
			AND m.user_id = $2::uuid
			AND m.status = 'active'
			AND m.archived_at IS NULL
			AND m.trashed_at IS NULL
	`, orgID.String(), userID)
	return scanUser(row)
}

func (r *Repository) getWithTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, userID string) (domain.User, error) {
	row := tx.QueryRow(ctx, `
		SELECT 'user',
			u.id::text,
			u.email,
			m.role,
			m.org_id,
			CASE
				WHEN m.trashed_at IS NOT NULL THEN 'trashed'
				WHEN m.archived_at IS NOT NULL THEN 'archived'
				ELSE 'active'
			END,
			m.created_at,
			m.updated_at,
			m.archived_at,
			m.trashed_at,
			m.purge_after
		FROM axis_org_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.org_id = $1::uuid
			AND m.user_id = $2::uuid
	`, orgID.String(), userID)
	return scanUser(row)
}

func (r *Repository) updateInvitation(ctx context.Context, input domain.NormalizedUpdateInput) (domain.User, error) {
	id := strings.TrimPrefix(input.UserID, "invitation:")
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		UPDATE axis_user_invitations
		SET email = $3,
			role = $4,
			updated_at = $5
		WHERE org_id = $1::uuid
			AND id = $2::uuid
			AND status = 'pending'
			AND archived_at IS NULL
			AND trashed_at IS NULL
		RETURNING 'invitation',
			'invitation:' || id::text,
			email,
			role,
			org_id,
			'pending',
			created_at,
			updated_at,
			archived_at,
			trashed_at,
			purge_after
	`, input.OrgID.String(), id, input.Email, input.Role, now)
	return scanUser(row)
}

func (r *Repository) lifecycleMembershipOrInvitation(
	ctx context.Context,
	input domain.NormalizedLifecycleInput,
	memberSQL string,
	invitationSQL string,
	now time.Time,
	purgeAfter *time.Time,
) error {
	id := input.UserID
	isInvitation := domain.KindFromID(id) == domain.KindInvitation
	if isInvitation {
		id = strings.TrimPrefix(id, "invitation:")
	}
	query := memberSQL
	if isInvitation {
		query = invitationSQL
	}
	var tag pgconnTag
	var err error
	if purgeAfter != nil {
		tag, err = r.pool.Exec(ctx, query, input.OrgID.String(), id, now, *purgeAfter)
	} else if now.IsZero() {
		tag, err = r.pool.Exec(ctx, query, input.OrgID.String(), id)
	} else {
		tag, err = r.pool.Exec(ctx, query, input.OrgID.String(), id, now)
	}
	return lifecycleResult(tag.RowsAffected(), err, "organization user not found")
}

type pgconnTag interface {
	RowsAffected() int64
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (domain.User, error) {
	var model models.ProductUser
	err := row.Scan(
		&model.Kind,
		&model.ID,
		&model.Email,
		&model.Role,
		&model.OrgID,
		&model.State,
		&model.CreatedAt,
		&model.UpdatedAt,
		&model.ArchivedAt,
		&model.TrashedAt,
		&model.PurgeAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domainerr.NotFound("organization user not found")
	}
	if err != nil {
		return domain.User{}, err
	}
	return model.ToDomain(), nil
}

func scanUsers(rows pgx.Rows) ([]domain.User, error) {
	out := []domain.User{}
	for rows.Next() {
		item, err := scanUser(rows)
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

func lifecycleResult(rowsAffected int64, err error, notFoundMessage string) error {
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domainerr.NotFound(notFoundMessage)
	}
	return nil
}
