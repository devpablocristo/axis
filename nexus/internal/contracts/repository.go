package contracts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "github.com/devpablocristo/nexus/internal/contracts/usecases/domain"
	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = domainerr.NotFound("contract not found")

type Repository interface {
	Upsert(ctx context.Context, contract domain.Contract) (domain.Contract, error)
	GetActive(ctx context.Context, name string, orgID *string) (domain.Contract, error)
	List(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.Contract, error)
	RecordValidation(ctx context.Context, report domain.ValidationReport) error
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Upsert(ctx context.Context, contract domain.Contract) (domain.Contract, error) {
	now := time.Now().UTC()
	if contract.ID == uuid.Nil {
		contract.ID = uuid.New()
	}
	if contract.Status == "" {
		contract.Status = domain.ContractStatusDraft
	}
	if contract.ValidationMode == "" {
		contract.ValidationMode = domain.ValidationModeReportOnly
	}
	if contract.Compatibility == "" {
		contract.Compatibility = "backward"
	}
	if contract.CreatedBy == "" {
		contract.CreatedBy = "system"
	}
	contract.UpdatedAt = now
	if contract.CreatedAt.IsZero() {
		contract.CreatedAt = now
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO governance_contracts
			(id, org_id, name, version, subject_type, schema_json, status, validation_mode, compatibility, created_by, created_at, updated_at, promoted_at, deprecated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (name, version, (COALESCE(org_id, ''))) DO UPDATE SET
			subject_type = EXCLUDED.subject_type,
			schema_json = EXCLUDED.schema_json,
			status = EXCLUDED.status,
			validation_mode = EXCLUDED.validation_mode,
			compatibility = EXCLUDED.compatibility,
			updated_at = EXCLUDED.updated_at,
			promoted_at = EXCLUDED.promoted_at,
			deprecated_at = EXCLUDED.deprecated_at
		RETURNING id, org_id, name, version, subject_type, schema_json, status, validation_mode, compatibility, created_by, created_at, updated_at, promoted_at, deprecated_at
	`, contract.ID, normalizedOrgPtr(contract.OrgID), contract.Name, contract.Version, contract.SubjectType, contract.Schema,
		contract.Status, contract.ValidationMode, contract.Compatibility, contract.CreatedBy, contract.CreatedAt, contract.UpdatedAt, contract.PromotedAt, contract.DeprecatedAt)
	return scanContract(row)
}

func (r *PostgresRepository) GetActive(ctx context.Context, name string, orgID *string) (domain.Contract, error) {
	row := r.db.Pool().QueryRow(ctx, selectContractSQL+`
		WHERE name = $1
		  AND status = 'active'
		  AND (($2::text IS NOT NULL AND org_id = $2::text) OR org_id IS NULL)
		ORDER BY CASE WHEN org_id = $2::text THEN 0 ELSE 1 END, promoted_at DESC NULLS LAST, created_at DESC
		LIMIT 1
	`, name, normalizedOrgPtr(orgID))
	contract, err := scanContract(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Contract{}, ErrNotFound
		}
		return domain.Contract{}, err
	}
	return contract, nil
}

func (r *PostgresRepository) List(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.Contract, error) {
	query := selectContractSQL + ` WHERE 1=1`
	args := []any{}
	switch {
	case orgID != nil && includeGlobal:
		query += ` AND (org_id = $1 OR org_id IS NULL)`
		args = append(args, *normalizedOrgPtr(orgID))
	case orgID != nil:
		query += ` AND org_id = $1`
		args = append(args, *normalizedOrgPtr(orgID))
	case !includeGlobal:
		query += ` AND org_id IS NOT NULL`
	}
	query += ` ORDER BY name, version, org_id NULLS FIRST`
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Contract, 0)
	for rows.Next() {
		contract, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, contract)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) RecordValidation(ctx context.Context, report domain.ValidationReport) error {
	if report.ID == uuid.Nil {
		report.ID = uuid.New()
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	rawErrors, err := json.Marshal(report.Errors)
	if err != nil {
		return fmt.Errorf("marshal validation errors: %w", err)
	}
	_, err = r.db.Pool().Exec(ctx, `
		INSERT INTO governance_contract_validation_reports
			(id, org_id, contract_name, contract_version, subject_type, subject_id, mode, valid, errors, payload_hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, report.ID, normalizedOrgPtr(report.OrgID), report.ContractName, report.ContractVersion,
		report.SubjectType, report.SubjectID, report.Mode, report.Valid, rawErrors, report.PayloadHash, report.CreatedAt)
	if err != nil {
		return fmt.Errorf("record validation report: %w", err)
	}
	return nil
}

const selectContractSQL = `
	SELECT id, org_id, name, version, subject_type, schema_json, status, validation_mode, compatibility, created_by, created_at, updated_at, promoted_at, deprecated_at
	FROM governance_contracts`

type scanRow interface {
	Scan(dest ...any) error
}

func scanContract(row scanRow) (domain.Contract, error) {
	var contract domain.Contract
	var rawSchema []byte
	if err := row.Scan(&contract.ID, &contract.OrgID, &contract.Name, &contract.Version, &contract.SubjectType,
		&rawSchema, &contract.Status, &contract.ValidationMode, &contract.Compatibility, &contract.CreatedBy,
		&contract.CreatedAt, &contract.UpdatedAt, &contract.PromotedAt, &contract.DeprecatedAt); err != nil {
		return domain.Contract{}, err
	}
	if len(rawSchema) > 0 {
		if err := json.Unmarshal(rawSchema, &contract.Schema); err != nil {
			return domain.Contract{}, fmt.Errorf("decode contract schema: %w", err)
		}
	}
	if contract.Schema == nil {
		contract.Schema = make(map[string]any)
	}
	return contract, nil
}

func normalizedOrgPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
