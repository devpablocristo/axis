package productintegrations

import (
	"context"
	"encoding/json"

	"github.com/devpablocristo/bff-v2/internal/productedge"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActiveCredential struct {
	Context       productedge.InvocationContext
	IntegrationID uuid.UUID
	Contract      json.RawMessage
}

type CredentialRepository interface {
	ActiveByDigest(context.Context, []byte) (ActiveCredential, error)
}

type PostgresCredentialRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCredentialRepository(pool *pgxpool.Pool) *PostgresCredentialRepository {
	return &PostgresCredentialRepository{pool: pool}
}

func (r *PostgresCredentialRepository) ActiveByDigest(ctx context.Context, digest []byte) (ActiveCredential, error) {
	if r == nil || r.pool == nil {
		return ActiveCredential{}, pgx.ErrNoRows
	}
	var out ActiveCredential
	err := r.pool.QueryRow(ctx, `
		SELECT c.org_id::text,c.product_id::text,p.product_surface,c.service_principal,c.scopes,
			i.id,v.revision,v.contract_hash,v.contract_json
		FROM product_credentials c
		JOIN product_integrations i ON i.id=c.integration_id
		JOIN product_integration_versions v ON v.id=i.active_version_id
		JOIN axis_products p ON p.id=c.product_id AND p.org_id=c.org_id
		JOIN axis_orgs o ON o.id=c.org_id
		WHERE c.secret_digest=$1 AND c.status='active' AND i.lifecycle='active'
			AND v.status='active' AND p.status='active' AND p.archived_at IS NULL AND p.trashed_at IS NULL
			AND o.status='active'
	`, digest).Scan(
		&out.Context.OrgID, &out.Context.ProductID, &out.Context.ProductSurface,
		&out.Context.PrincipalID, &out.Context.Scopes, &out.IntegrationID,
		&out.Context.IntegrationRevision, &out.Context.IntegrationHash, &out.Contract,
	)
	return out, err
}

var _ CredentialRepository = (*PostgresCredentialRepository)(nil)
