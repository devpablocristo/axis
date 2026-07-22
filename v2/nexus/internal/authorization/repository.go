package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, grant Grant) (Grant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO functional_role_grants
		(id,tenant_id,user_id,role_key,product_surface,action_type_pattern,resource_type,resource_id,max_risk_class,
		 valid_from,valid_until,revision,granted_by,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14)`,
		grant.ID, grant.TenantID, grant.UserID, grant.RoleKey, grant.ProductSurface, grant.ActionTypePattern, grant.ResourceType, grant.ResourceID,
		grant.MaxRiskClass, grant.ValidFrom, grant.ValidUntil, grant.Revision, grant.GrantedBy, grant.CreatedAt)
	if err != nil {
		return Grant{}, err
	}
	if err := auditGrant(ctx, tx, grant, "granted", grant.GrantedBy); err != nil {
		return Grant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

func (r *Repository) List(ctx context.Context, tenantID, userID string) ([]Grant, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,user_id,role_key,product_surface,action_type_pattern,resource_type,resource_id,
		max_risk_class,valid_from,valid_until,revision,granted_by,revoked_at,revoked_by,revocation_reason,created_at,updated_at
		FROM functional_role_grants WHERE tenant_id=$1 AND ($2='' OR user_id=$2) ORDER BY created_at DESC,id`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		item, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ActiveForUser(ctx context.Context, tenantID, userID string, at time.Time) ([]Grant, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,tenant_id,user_id,role_key,product_surface,action_type_pattern,resource_type,resource_id,
		max_risk_class,valid_from,valid_until,revision,granted_by,revoked_at,revoked_by,revocation_reason,created_at,updated_at
		FROM functional_role_grants WHERE tenant_id=$1 AND user_id=$2 AND revoked_at IS NULL AND valid_from<=$3 AND valid_until>$3
		ORDER BY role_key,id`, tenantID, userID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		item, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Revoke(ctx context.Context, tenantID string, id uuid.UUID, actor, reason string, expected int64) (Grant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `UPDATE functional_role_grants SET revoked_at=now(),revoked_by=$4,revocation_reason=$5,revision=revision+1,updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND revision=$3 AND revoked_at IS NULL RETURNING id,tenant_id,user_id,role_key,product_surface,
		action_type_pattern,resource_type,resource_id,max_risk_class,valid_from,valid_until,revision,granted_by,revoked_at,revoked_by,
		revocation_reason,created_at,updated_at`, tenantID, id, expected, actor, reason)
	grant, err := scanGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, domainerr.Conflict("role grant revision changed or grant is revoked")
	}
	if err != nil {
		return Grant{}, err
	}
	if err := auditGrant(ctx, tx, grant, "revoked", actor); err != nil {
		return Grant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

type scanner interface{ Scan(...any) error }

func scanGrant(row scanner) (Grant, error) {
	var g Grant
	err := row.Scan(&g.ID, &g.TenantID, &g.UserID, &g.RoleKey, &g.ProductSurface, &g.ActionTypePattern, &g.ResourceType, &g.ResourceID, &g.MaxRiskClass, &g.ValidFrom, &g.ValidUntil, &g.Revision, &g.GrantedBy, &g.RevokedAt, &g.RevokedBy, &g.RevocationReason, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}
func auditGrant(ctx context.Context, tx pgx.Tx, g Grant, action, actor string) error {
	raw, _ := json.Marshal(g)
	_, err := tx.Exec(ctx, `INSERT INTO functional_role_grant_audit(id,tenant_id,grant_id,actor_id,action,revision,snapshot,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, uuid.New(), g.TenantID, g.ID, actor, action, g.Revision, raw, time.Now().UTC())
	return err
}
