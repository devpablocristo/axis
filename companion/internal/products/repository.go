package products

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) UpsertProduct(ctx context.Context, product Product) (Product, error) {
	product = normalizeProduct(product)
	metadata, err := json.Marshal(product.Metadata)
	if err != nil {
		return Product{}, fmt.Errorf("marshal product metadata: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_products
			(product_surface, display_name, status, metadata_json, created_by)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (product_surface) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			status = EXCLUDED.status,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()
		RETURNING product_surface, display_name, status, metadata_json, created_by, created_at, updated_at
	`, product.ProductSurface, product.DisplayName, product.Status, metadata, product.CreatedBy)
	return scanProduct(row)
}

func (r *PostgresRepository) GetProduct(ctx context.Context, productSurface string) (Product, error) {
	row := r.db.Pool().QueryRow(ctx, selectProduct+` WHERE product_surface = $1`, normalizeProductSurface(productSurface))
	product, err := scanProduct(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Product{}, ErrProductNotFound
		}
		return Product{}, fmt.Errorf("get product: %w", err)
	}
	return product, nil
}

func (r *PostgresRepository) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := r.db.Pool().Query(ctx, selectProduct+` ORDER BY product_surface ASC`)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	out := make([]Product, 0)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, product)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertInstallation(ctx context.Context, installation Installation) (Installation, error) {
	installation = normalizeInstallation(installation)
	config, err := json.Marshal(installation.Config)
	if err != nil {
		return Installation{}, fmt.Errorf("marshal product installation config: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO companion_product_installations
			(org_id, product_surface, external_tenant_id, base_url, auth_mode, secret_ref, enabled, config_json, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (org_id, product_surface) DO UPDATE SET
			external_tenant_id = EXCLUDED.external_tenant_id,
			base_url = EXCLUDED.base_url,
			auth_mode = EXCLUDED.auth_mode,
			secret_ref = EXCLUDED.secret_ref,
			enabled = EXCLUDED.enabled,
			config_json = EXCLUDED.config_json,
			updated_at = now()
		RETURNING id::text, org_id, product_surface, external_tenant_id, base_url, auth_mode, secret_ref, enabled, config_json, created_by, created_at, updated_at
	`, installation.OrgID, installation.ProductSurface, installation.ExternalTenantID, installation.BaseURL,
		installation.AuthMode, installation.SecretRef, installation.Enabled, config, installation.CreatedBy)
	return scanInstallation(row)
}

func (r *PostgresRepository) GetInstallation(ctx context.Context, orgID, productSurface string) (Installation, error) {
	row := r.db.Pool().QueryRow(ctx, selectInstallation+`
		WHERE org_id = $1 AND product_surface = $2
	`, strings.TrimSpace(orgID), normalizeProductSurface(productSurface))
	installation, err := scanInstallation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, ErrInstallationNotFound
		}
		return Installation{}, fmt.Errorf("get product installation: %w", err)
	}
	return installation, nil
}

func (r *PostgresRepository) ListInstallations(ctx context.Context, orgID string) ([]Installation, error) {
	rows, err := r.db.Pool().Query(ctx, selectInstallation+`
		WHERE org_id = $1
		ORDER BY product_surface ASC
	`, strings.TrimSpace(orgID))
	if err != nil {
		return nil, fmt.Errorf("list product installations: %w", err)
	}
	defer rows.Close()
	out := make([]Installation, 0)
	for rows.Next() {
		installation, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, installation)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListInstallationsByProduct(ctx context.Context, productSurface string) ([]Installation, error) {
	rows, err := r.db.Pool().Query(ctx, selectInstallation+`
		WHERE product_surface = $1
		ORDER BY org_id ASC
	`, normalizeProductSurface(productSurface))
	if err != nil {
		return nil, fmt.Errorf("list product installations by product: %w", err)
	}
	defer rows.Close()
	out := make([]Installation, 0)
	for rows.Next() {
		installation, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, installation)
	}
	return out, rows.Err()
}

const selectProduct = `
	SELECT product_surface, display_name, status, metadata_json, created_by, created_at, updated_at
	FROM companion_products`

const selectInstallation = `
	SELECT id::text, org_id, product_surface, external_tenant_id, base_url, auth_mode, secret_ref,
	       enabled, config_json, created_by, created_at, updated_at
	FROM companion_product_installations`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProduct(row rowScanner) (Product, error) {
	var (
		product              Product
		metadata             []byte
		createdAt, updatedAt time.Time
	)
	if err := row.Scan(&product.ProductSurface, &product.DisplayName, &product.Status, &metadata, &product.CreatedBy, &createdAt, &updatedAt); err != nil {
		return Product{}, err
	}
	product.Metadata = unmarshalMap(metadata)
	product.CreatedAt = createdAt
	product.UpdatedAt = updatedAt
	return normalizeProduct(product), nil
}

func scanInstallation(row rowScanner) (Installation, error) {
	var (
		installation         Installation
		config               []byte
		createdAt, updatedAt time.Time
	)
	if err := row.Scan(&installation.ID, &installation.OrgID, &installation.ProductSurface, &installation.ExternalTenantID,
		&installation.BaseURL, &installation.AuthMode, &installation.SecretRef, &installation.Enabled,
		&config, &installation.CreatedBy, &createdAt, &updatedAt); err != nil {
		return Installation{}, err
	}
	installation.Config = unmarshalMap(config)
	installation.CreatedAt = createdAt
	installation.UpdatedAt = updatedAt
	return normalizeInstallation(installation), nil
}

func unmarshalMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
