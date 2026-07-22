package artifacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

func (r *Repository) UpsertManifest(ctx context.Context, scope Scope, manifest Manifest) (Record, error) {
	id := uuid.New()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO companion_artifacts (
			id, tenant_id, virployee_id, product_surface, subject_id, repository_generation,
			document_id, name, source_ref, sha256, mime_type, size_bytes, required, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'received')
		ON CONFLICT (tenant_id, virployee_id, product_surface, subject_id, repository_generation, document_id)
		DO UPDATE SET
			name = EXCLUDED.name,
			source_ref = EXCLUDED.source_ref,
			sha256 = EXCLUDED.sha256,
			mime_type = EXCLUDED.mime_type,
			size_bytes = EXCLUDED.size_bytes,
			required = EXCLUDED.required,
			updated_at = now()
		WHERE companion_artifacts.sha256 = EXCLUDED.sha256
		  AND companion_artifacts.size_bytes = EXCLUDED.size_bytes
		RETURNING `+recordColumns+`
	`, id, scope.TenantID, scope.VirployeeID, scope.ProductSurface, scope.SubjectID, scope.RepositoryGeneration,
		manifest.DocumentID, manifest.Name, manifest.SourceRef, manifest.SHA256, manifest.MIMEType, manifest.SizeBytes, manifest.Required)
	record, err := scanRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, fmt.Errorf("document_id %s changed within one repository generation", manifest.DocumentID)
	}
	if err != nil {
		return Record{}, err
	}
	record.Manifest.ReadURL = manifest.ReadURL
	return record, nil
}

func (r *Repository) SetStatus(ctx context.Context, tenantID string, id uuid.UUID, status Status, stagedURI, actualMIME, errorCode string) (Record, error) {
	var expiresAt *time.Time
	if stagedURI != "" {
		value := r.now().UTC().Add(StagingTTL)
		expiresAt = &value
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE companion_artifacts
		SET status=$3,
		    staged_uri=CASE WHEN $4 <> '' THEN $4 ELSE staged_uri END,
		    actual_mime=CASE WHEN $5 <> '' THEN $5 ELSE actual_mime END,
		    error_code=$6,
		    expires_at=COALESCE($7, expires_at),
		    updated_at=now()
		WHERE tenant_id=$1 AND id=$2
		RETURNING `+recordColumns,
		tenantID, id, status, stagedURI, actualMIME, errorCode, expiresAt)
	return scanRecord(row)
}

func (r *Repository) ListGeneration(ctx context.Context, scope Scope) ([]Record, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+recordColumns+`
		FROM companion_artifacts
		WHERE tenant_id=$1 AND virployee_id=$2 AND product_surface=$3 AND subject_id=$4 AND repository_generation=$5
		ORDER BY document_id`, scope.TenantID, scope.VirployeeID, scope.ProductSurface, scope.SubjectID, scope.RepositoryGeneration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Record, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

const recordColumns = `id, tenant_id, virployee_id, product_surface, subject_id, repository_generation,
	document_id, name, source_ref, sha256, mime_type, size_bytes, required, status,
	staged_uri, actual_mime, error_code, expires_at, created_at, updated_at`

type recordScanner interface{ Scan(...any) error }

func scanRecord(row recordScanner) (Record, error) {
	var record Record
	var expiresAt *time.Time
	err := row.Scan(
		&record.ID, &record.Scope.TenantID, &record.Scope.VirployeeID, &record.Scope.ProductSurface,
		&record.Scope.SubjectID, &record.Scope.RepositoryGeneration, &record.Manifest.DocumentID,
		&record.Manifest.Name, &record.Manifest.SourceRef, &record.Manifest.SHA256, &record.Manifest.MIMEType,
		&record.Manifest.SizeBytes, &record.Manifest.Required, &record.Status, &record.StagedURI,
		&record.ActualMIME, &record.ErrorCode, &expiresAt, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Record{}, err
		}
		return Record{}, err
	}
	if expiresAt != nil {
		record.ExpiresAt = *expiresAt
	}
	return record, nil
}
