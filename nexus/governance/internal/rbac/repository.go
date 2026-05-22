package rbac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	domain "github.com/devpablocristo/nexus/governance/internal/rbac/usecases/domain"
)

var (
	ErrNotFound      = domainerr.NotFound("assignment not found")
	ErrAlreadyExists = domainerr.Conflict("assignment already exists")
)

type Repository interface {
	Create(ctx context.Context, a domain.Assignment) (domain.Assignment, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Assignment, error)
	List(ctx context.Context, filter ListFilter) ([]domain.Assignment, error)
	Check(ctx context.Context, orgID, userID string, role domain.Role) (bool, error)
	Archive(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type ListFilter struct {
	OrgID          string
	UserID         string
	Role           string
	IncludeRevoked bool
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectSQL = `SELECT id, org_id, user_id, role, COALESCE(granted_by, ''), granted_at, revoked_at FROM governance_role_assignments`

func (r *PostgresRepository) Create(ctx context.Context, a domain.Assignment) (domain.Assignment, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.GrantedAt.IsZero() {
		a.GrantedAt = time.Now().UTC()
	}
	var grantedBy any
	if a.GrantedBy != "" {
		grantedBy = a.GrantedBy
	}
	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO governance_role_assignments (id, org_id, user_id, role, granted_by, granted_at)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, a.ID, a.OrgID, a.UserID, string(a.Role), grantedBy, a.GrantedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.Assignment{}, ErrAlreadyExists
		}
		return domain.Assignment{}, fmt.Errorf("insert assignment: %w", err)
	}
	return a, nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Assignment, error) {
	row := r.db.Pool().QueryRow(ctx, selectSQL+` WHERE id = $1`, id)
	a, err := scanAssignment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Assignment{}, ErrNotFound
		}
		return domain.Assignment{}, err
	}
	return a, nil
}

func (r *PostgresRepository) List(ctx context.Context, filter ListFilter) ([]domain.Assignment, error) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if filter.OrgID != "" {
		add("org_id = $%d", filter.OrgID)
	}
	if filter.UserID != "" {
		add("user_id = $%d", filter.UserID)
	}
	if filter.Role != "" {
		add("role = $%d", filter.Role)
	}
	if !filter.IncludeRevoked {
		conds = append(conds, "revoked_at IS NULL")
	}
	q := selectSQL
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY granted_at DESC"

	rows, err := r.db.Pool().Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Assignment, 0)
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assignments rows: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) Check(ctx context.Context, orgID, userID string, role domain.Role) (bool, error) {
	var exists bool
	err := r.db.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM governance_role_assignments
			WHERE org_id = $1 AND user_id = $2 AND role = $3 AND revoked_at IS NULL
		)
	`, orgID, userID, string(role)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check assignment: %w", err)
	}
	return exists, nil
}

func (r *PostgresRepository) Archive(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE governance_role_assignments
		SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return fmt.Errorf("archive assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// puede ser no-encontrado o ya revocado; si existe, es idempotente
		var existed bool
		if checkErr := r.db.Pool().QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM governance_role_assignments WHERE id = $1)`, id).Scan(&existed); checkErr != nil {
			return fmt.Errorf("archive lookup: %w", checkErr)
		}
		if !existed {
			return ErrNotFound
		}
	}
	return nil
}

func (r *PostgresRepository) Restore(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE governance_role_assignments
		SET revoked_at = NULL
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("restore assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM governance_role_assignments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAssignment(row rowScanner) (domain.Assignment, error) {
	var (
		a       domain.Assignment
		role    string
		revoked *time.Time
	)
	if err := row.Scan(&a.ID, &a.OrgID, &a.UserID, &role, &a.GrantedBy, &a.GrantedAt, &revoked); err != nil {
		return domain.Assignment{}, err
	}
	a.Role = domain.Role(role)
	a.RevokedAt = revoked
	return a, nil
}
