package actiontypes

import (
	"context"
	"errors"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.ActionType, error) {
	id := uuid.New()
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO action_types (
			id, tenant_id, action_type_key, name, description, category, risk_class, enabled, created_at, updated_at
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		RETURNING id::text, tenant_id, action_type_key, name, description, category, risk_class, enabled, created_at, updated_at
	`, id.String(), tenantID, input.ActionTypeKey, input.Name, input.Description, input.Category, string(input.RiskClass), input.Enabled, now)
	return scanActionType(row)
}

func (r *Repository) List(ctx context.Context, tenantID string) ([]domain.ActionType, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, tenant_id, action_type_key, name, description, category, risk_class, enabled, created_at, updated_at
		FROM action_types
		WHERE tenant_id = $1
		ORDER BY action_type_key ASC, id ASC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.ActionType{}
	for rows.Next() {
		item, err := scanActionType(rows)
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

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.ActionType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id, action_type_key, name, description, category, risk_class, enabled, created_at, updated_at
		FROM action_types
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, id.String())
	return scanActionType(row)
}

func (r *Repository) GetByKey(ctx context.Context, tenantID string, key string) (domain.ActionType, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id, action_type_key, name, description, category, risk_class, enabled, created_at, updated_at
		FROM action_types
		WHERE tenant_id = $1 AND action_type_key = $2
	`, tenantID, key)
	return scanActionType(row)
}

func (r *Repository) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ActionType, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE action_types
		SET name = $3,
			description = $4,
			category = $5,
			risk_class = $6,
			enabled = $7,
			updated_at = $8
		WHERE tenant_id = $1 AND id = $2::uuid
		RETURNING id::text, tenant_id, action_type_key, name, description, category, risk_class, enabled, created_at, updated_at
	`, tenantID, id.String(), input.Name, input.Description, input.Category, string(input.RiskClass), input.Enabled, time.Now().UTC())
	return scanActionType(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanActionType(row scanner) (domain.ActionType, error) {
	var idText string
	var riskClass string
	var item domain.ActionType
	err := row.Scan(
		&idText,
		&item.TenantID,
		&item.ActionTypeKey,
		&item.Name,
		&item.Description,
		&item.Category,
		&riskClass,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ActionType{}, domainerr.NotFound("action type not found")
	}
	if err != nil {
		return domain.ActionType{}, mapActionTypeError(err)
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.ActionType{}, err
	}
	item.ID = id
	item.RiskClass = domain.RiskClass(riskClass)
	return item, nil
}

func mapActionTypeError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domainerr.Conflict("action type key already exists")
	}
	return err
}

var _ RepositoryPort = (*Repository)(nil)
