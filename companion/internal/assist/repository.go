package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/devpablocristo/companion/internal/assist/usecases/domain"
)

var ErrNotFound = domainerr.NotFound("not found")

type PackFilter struct {
	OrgID           string
	OwnerSystem     string
	ProductSurface  string
	AssistType      string
	IncludeArchived bool
	ArchivedOnly    bool
}

type RunFilter struct {
	OrgID          string
	OwnerSystem    string
	ProductSurface string
	AssistType     string
	SubjectID      string
	Status         string
	Limit          int
}

type Repository interface {
	UpsertPack(ctx context.Context, pack domain.AssistPack) (domain.AssistPack, error)
	GetPack(ctx context.Context, id uuid.UUID) (domain.AssistPack, error)
	GetPackByType(ctx context.Context, orgID, ownerSystem, productSurface, assistType string) (domain.AssistPack, error)
	GetPackByTypeIncludingArchived(ctx context.Context, orgID, ownerSystem, productSurface, assistType string) (domain.AssistPack, error)
	ListPacks(ctx context.Context, filter PackFilter) ([]domain.AssistPack, error)
	UpdatePack(ctx context.Context, pack domain.AssistPack) (domain.AssistPack, error)
	SoftDelete(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error
	Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error
	HardDelete(ctx context.Context, tenantID string, resourceID uuid.UUID) error
	IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error)
	CreateRun(ctx context.Context, run domain.AssistRun) (domain.AssistRun, error)
	GetRun(ctx context.Context, id uuid.UUID) (domain.AssistRun, error)
	GetRunByIdempotencyKey(ctx context.Context, orgID, idempotencyKey string) (domain.AssistRun, error)
	UpdateRunResult(ctx context.Context, id uuid.UUID, status string, output map[string]any, errorMessage string, completedAt time.Time) (domain.AssistRun, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]domain.AssistRun, error)
}

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectPackSQL = `
	SELECT id, org_id, owner_system, product_surface, assist_type, name, description,
	       prompt_template, model_policy_json, output_schema_json, enabled,
	       archived_at, created_at, updated_at
	FROM assist_packs`

const selectRunSQL = `
	SELECT id, org_id, pack_id, owner_system, product_surface, assist_type, subject_type,
	       subject_id, input_json, output_json, status, error_message, idempotency_key,
	       created_at, completed_at
	FROM assist_runs`

func (r *PostgresRepository) UpsertPack(ctx context.Context, pack domain.AssistPack) (domain.AssistPack, error) {
	now := time.Now().UTC()
	if pack.ID == uuid.Nil {
		pack.ID = uuid.New()
	}
	if pack.CreatedAt.IsZero() {
		pack.CreatedAt = now
	}
	pack.UpdatedAt = now
	modelPolicyJSON, err := json.Marshal(nonNilMap(pack.ModelPolicy))
	if err != nil {
		return domain.AssistPack{}, fmt.Errorf("marshal model policy: %w", err)
	}
	outputSchemaJSON, err := json.Marshal(nonNilMap(pack.OutputSchema))
	if err != nil {
		return domain.AssistPack{}, fmt.Errorf("marshal output schema: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO assist_packs (
			id, org_id, owner_system, product_surface, assist_type, name, description,
			prompt_template, model_policy_json, output_schema_json, enabled,
			created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (org_id, owner_system, product_surface, assist_type)
		DO UPDATE SET
			product_surface = EXCLUDED.product_surface,
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			prompt_template = EXCLUDED.prompt_template,
			model_policy_json = EXCLUDED.model_policy_json,
			output_schema_json = EXCLUDED.output_schema_json,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, org_id, owner_system, product_surface, assist_type, name, description,
		          prompt_template, model_policy_json, output_schema_json, enabled,
		          archived_at, created_at, updated_at
	`, pack.ID, pack.OrgID, pack.OwnerSystem, pack.ProductSurface, pack.AssistType, pack.Name, pack.Description,
		pack.PromptTemplate, modelPolicyJSON, outputSchemaJSON, pack.Enabled,
		pack.CreatedAt, pack.UpdatedAt)
	return scanPack(row)
}

func (r *PostgresRepository) GetPack(ctx context.Context, id uuid.UUID) (domain.AssistPack, error) {
	row := r.db.Pool().QueryRow(ctx, selectPackSQL+` WHERE id = $1`, id)
	pack, err := scanPack(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistPack{}, ErrNotFound
		}
		return domain.AssistPack{}, err
	}
	return pack, nil
}

func (r *PostgresRepository) GetPackByType(ctx context.Context, orgID, ownerSystem, productSurface, assistType string) (domain.AssistPack, error) {
	row := r.db.Pool().QueryRow(ctx, selectPackSQL+`
		WHERE org_id = $1 AND owner_system = $2 AND product_surface = $3 AND assist_type = $4 AND archived_at IS NULL
	`, orgID, ownerSystem, productSurface, assistType)
	pack, err := scanPack(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistPack{}, ErrNotFound
		}
		return domain.AssistPack{}, err
	}
	return pack, nil
}

// GetPackByTypeIncludingArchived resolves a pack by its natural key WITHOUT the
// archived_at IS NULL filter, so callers (e.g. upsert) can detect an archived
// pack and refuse to silently un-archive it. Returns ErrNotFound if absent.
func (r *PostgresRepository) GetPackByTypeIncludingArchived(ctx context.Context, orgID, ownerSystem, productSurface, assistType string) (domain.AssistPack, error) {
	row := r.db.Pool().QueryRow(ctx, selectPackSQL+`
		WHERE org_id = $1 AND owner_system = $2 AND product_surface = $3 AND assist_type = $4
	`, orgID, ownerSystem, productSurface, assistType)
	pack, err := scanPack(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistPack{}, ErrNotFound
		}
		return domain.AssistPack{}, err
	}
	return pack, nil
}

func (r *PostgresRepository) ListPacks(ctx context.Context, filter PackFilter) ([]domain.AssistPack, error) {
	query := selectPackSQL + ` WHERE org_id = $1`
	args := []any{filter.OrgID}
	if filter.OwnerSystem != "" {
		query += fmt.Sprintf(` AND owner_system = $%d`, len(args)+1)
		args = append(args, filter.OwnerSystem)
	}
	if filter.ProductSurface != "" {
		query += fmt.Sprintf(` AND product_surface = $%d`, len(args)+1)
		args = append(args, filter.ProductSurface)
	}
	if filter.AssistType != "" {
		query += fmt.Sprintf(` AND assist_type = $%d`, len(args)+1)
		args = append(args, filter.AssistType)
	}
	if filter.ArchivedOnly {
		query += ` AND archived_at IS NOT NULL`
	} else if !filter.IncludeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY owner_system, assist_type`
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list assist packs: %w", err)
	}
	defer rows.Close()
	out := make([]domain.AssistPack, 0)
	for rows.Next() {
		pack, err := scanPack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pack)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdatePack(ctx context.Context, pack domain.AssistPack) (domain.AssistPack, error) {
	pack.UpdatedAt = time.Now().UTC()
	modelPolicyJSON, err := json.Marshal(nonNilMap(pack.ModelPolicy))
	if err != nil {
		return domain.AssistPack{}, fmt.Errorf("marshal model policy: %w", err)
	}
	outputSchemaJSON, err := json.Marshal(nonNilMap(pack.OutputSchema))
	if err != nil {
		return domain.AssistPack{}, fmt.Errorf("marshal output schema: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE assist_packs SET
			owner_system = $2,
			product_surface = $3,
			assist_type = $4,
			name = $5,
			description = $6,
			prompt_template = $7,
			model_policy_json = $8,
			output_schema_json = $9,
			enabled = $10,
			updated_at = $11
		WHERE id = $1
		RETURNING id, org_id, owner_system, product_surface, assist_type, name, description,
		          prompt_template, model_policy_json, output_schema_json, enabled,
		          archived_at, created_at, updated_at
	`, pack.ID, pack.OwnerSystem, pack.ProductSurface, pack.AssistType, pack.Name, pack.Description,
		pack.PromptTemplate, modelPolicyJSON, outputSchemaJSON, pack.Enabled, pack.UpdatedAt)
	updated, err := scanPack(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistPack{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return domain.AssistPack{}, domainerr.Conflict("assist pack with this owner_system/product_surface/assist_type already exists")
		}
		return domain.AssistPack{}, err
	}
	return updated, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). Mirrors connectors/repository.go.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *PostgresRepository) SoftDelete(ctx context.Context, tenantID string, resourceID uuid.UUID, at time.Time) error {
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE assist_packs SET archived_at = $3, updated_at = $3
		WHERE id = $1 AND org_id = $2 AND archived_at IS NULL
	`, resourceID, tenantID, at.UTC())
	if err != nil {
		return fmt.Errorf("archive assist pack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) Restore(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE assist_packs SET archived_at = NULL, updated_at = $3
		WHERE id = $1 AND org_id = $2 AND archived_at IS NOT NULL
	`, resourceID, tenantID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("restore assist pack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) HardDelete(ctx context.Context, tenantID string, resourceID uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM assist_packs WHERE id = $1 AND org_id = $2`, resourceID, tenantID)
	if err != nil {
		return fmt.Errorf("delete assist pack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) IsArchived(ctx context.Context, tenantID string, resourceID uuid.UUID) (bool, error) {
	var archived bool
	err := r.db.Pool().QueryRow(ctx, `
		SELECT archived_at IS NOT NULL FROM assist_packs WHERE id = $1 AND org_id = $2
	`, resourceID, tenantID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return archived, nil
}

func (r *PostgresRepository) CreateRun(ctx context.Context, run domain.AssistRun) (domain.AssistRun, error) {
	now := time.Now().UTC()
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	inputJSON, err := json.Marshal(nonNilMap(run.Input))
	if err != nil {
		return domain.AssistRun{}, fmt.Errorf("marshal assist input: %w", err)
	}
	outputJSON, err := json.Marshal(nonNilMap(run.Output))
	if err != nil {
		return domain.AssistRun{}, fmt.Errorf("marshal assist output: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO assist_runs (
			id, org_id, pack_id, owner_system, product_surface, assist_type, subject_type,
			subject_id, input_json, output_json, status, error_message, idempotency_key,
			created_at, completed_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, org_id, pack_id, owner_system, product_surface, assist_type, subject_type,
		          subject_id, input_json, output_json, status, error_message, idempotency_key,
		          created_at, completed_at
	`, run.ID, run.OrgID, run.PackID, run.OwnerSystem, run.ProductSurface, run.AssistType, run.SubjectType,
		run.SubjectID, inputJSON, outputJSON, run.Status, run.ErrorMessage, run.IdempotencyKey,
		run.CreatedAt, run.CompletedAt)
	created, err := scanRun(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.AssistRun{}, domainerr.Conflict("assist run with this idempotency key already exists")
		}
		return domain.AssistRun{}, err
	}
	return created, nil
}

func (r *PostgresRepository) GetRun(ctx context.Context, id uuid.UUID) (domain.AssistRun, error) {
	row := r.db.Pool().QueryRow(ctx, selectRunSQL+` WHERE id = $1`, id)
	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistRun{}, ErrNotFound
		}
		return domain.AssistRun{}, err
	}
	return run, nil
}

// GetRunByIdempotencyKey returns the run previously created with the given
// idempotency key for the org, or ErrNotFound. The empty key never matches.
func (r *PostgresRepository) GetRunByIdempotencyKey(ctx context.Context, orgID, idempotencyKey string) (domain.AssistRun, error) {
	if idempotencyKey == "" {
		return domain.AssistRun{}, ErrNotFound
	}
	row := r.db.Pool().QueryRow(ctx, selectRunSQL+`
		WHERE org_id = $1 AND idempotency_key = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, orgID, idempotencyKey)
	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistRun{}, ErrNotFound
		}
		return domain.AssistRun{}, err
	}
	return run, nil
}

// UpdateRunResult finalizes a previously-reserved run with its outcome.
func (r *PostgresRepository) UpdateRunResult(ctx context.Context, id uuid.UUID, status string, output map[string]any, errorMessage string, completedAt time.Time) (domain.AssistRun, error) {
	outputJSON, err := json.Marshal(nonNilMap(output))
	if err != nil {
		return domain.AssistRun{}, fmt.Errorf("marshal assist output: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE assist_runs SET output_json = $2, status = $3, error_message = $4, completed_at = $5
		WHERE id = $1
		RETURNING id, org_id, pack_id, owner_system, product_surface, assist_type, subject_type,
		          subject_id, input_json, output_json, status, error_message, idempotency_key,
		          created_at, completed_at
	`, id, outputJSON, status, errorMessage, completedAt.UTC())
	updated, err := scanRun(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AssistRun{}, ErrNotFound
		}
		return domain.AssistRun{}, err
	}
	return updated, nil
}

func (r *PostgresRepository) ListRuns(ctx context.Context, filter RunFilter) ([]domain.AssistRun, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := selectRunSQL + ` WHERE org_id = $1`
	args := []any{filter.OrgID}
	if filter.OwnerSystem != "" {
		query += fmt.Sprintf(` AND owner_system = $%d`, len(args)+1)
		args = append(args, filter.OwnerSystem)
	}
	if filter.ProductSurface != "" {
		query += fmt.Sprintf(` AND product_surface = $%d`, len(args)+1)
		args = append(args, filter.ProductSurface)
	}
	if filter.AssistType != "" {
		query += fmt.Sprintf(` AND assist_type = $%d`, len(args)+1)
		args = append(args, filter.AssistType)
	}
	if filter.SubjectID != "" {
		query += fmt.Sprintf(` AND subject_id = $%d`, len(args)+1)
		args = append(args, filter.SubjectID)
	}
	if filter.Status != "" {
		query += fmt.Sprintf(` AND status = $%d`, len(args)+1)
		args = append(args, filter.Status)
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list assist runs: %w", err)
	}
	defer rows.Close()
	out := make([]domain.AssistRun, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanPack(row scanRow) (domain.AssistPack, error) {
	var pack domain.AssistPack
	var modelPolicyJSON []byte
	var outputSchemaJSON []byte
	if err := row.Scan(
		&pack.ID, &pack.OrgID, &pack.OwnerSystem, &pack.ProductSurface, &pack.AssistType,
		&pack.Name, &pack.Description, &pack.PromptTemplate,
		&modelPolicyJSON, &outputSchemaJSON, &pack.Enabled, &pack.ArchivedAt, &pack.CreatedAt, &pack.UpdatedAt,
	); err != nil {
		return domain.AssistPack{}, err
	}
	if err := json.Unmarshal(modelPolicyJSON, &pack.ModelPolicy); err != nil {
		return domain.AssistPack{}, err
	}
	if pack.ModelPolicy == nil {
		pack.ModelPolicy = map[string]any{}
	}
	if len(outputSchemaJSON) > 0 {
		if err := json.Unmarshal(outputSchemaJSON, &pack.OutputSchema); err != nil {
			return domain.AssistPack{}, err
		}
	}
	if pack.OutputSchema == nil {
		pack.OutputSchema = map[string]any{}
	}
	return pack, nil
}

func scanRun(row scanRow) (domain.AssistRun, error) {
	var run domain.AssistRun
	var inputJSON []byte
	var outputJSON []byte
	if err := row.Scan(
		&run.ID, &run.OrgID, &run.PackID, &run.OwnerSystem, &run.ProductSurface, &run.AssistType,
		&run.SubjectType, &run.SubjectID, &inputJSON, &outputJSON, &run.Status, &run.ErrorMessage,
		&run.IdempotencyKey, &run.CreatedAt, &run.CompletedAt,
	); err != nil {
		return domain.AssistRun{}, err
	}
	if err := json.Unmarshal(inputJSON, &run.Input); err != nil {
		return domain.AssistRun{}, err
	}
	if err := json.Unmarshal(outputJSON, &run.Output); err != nil {
		return domain.AssistRun{}, err
	}
	if run.Input == nil {
		run.Input = map[string]any{}
	}
	if run.Output == nil {
		run.Output = map[string]any{}
	}
	return run, nil
}

func nonNilMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}
