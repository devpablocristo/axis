package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

var ErrNotFound = errors.New("business model not found")

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) GetActive(ctx context.Context, orgID, productSurface string) (Model, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT id::text, org_id, product_surface, version, status, model_json, created_by, created_at, updated_at
		FROM companion_business_models
		WHERE org_id = $1 AND product_surface = $2 AND status = 'active'
	`, strings.TrimSpace(orgID), strings.TrimSpace(productSurface))
	model, err := scanBusinessModel(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Model{}, ErrNotFound
		}
		return Model{}, fmt.Errorf("get active business model: %w", err)
	}
	return model, nil
}

func (r *PostgresRepository) Save(ctx context.Context, model Model) (Model, error) {
	raw, err := json.Marshal(model)
	if err != nil {
		return Model{}, fmt.Errorf("marshal business model: %w", err)
	}
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return Model{}, fmt.Errorf("begin business model save: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Error("business_model_rollback_failed", "error", rollbackErr)
		}
	}()
	var version int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM companion_business_models
		WHERE org_id = $1 AND product_surface = $2
	`, model.OrgID, model.ProductSurface).Scan(&version)
	if err != nil {
		return Model{}, fmt.Errorf("next business model version: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE companion_business_models
		SET status = 'archived', updated_at = now()
		WHERE org_id = $1 AND product_surface = $2 AND status = 'active'
	`, model.OrgID, model.ProductSurface)
	if err != nil {
		return Model{}, fmt.Errorf("archive active business model: %w", err)
	}
	now := time.Now().UTC()
	id := uuid.New()
	row := tx.QueryRow(ctx, `
		INSERT INTO companion_business_models
			(id, org_id, product_surface, version, status, model_json, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'active',$5,$6,$7,$7)
		RETURNING id::text, org_id, product_surface, version, status, model_json, created_by, created_at, updated_at
	`, id, model.OrgID, model.ProductSurface, version, raw, model.CreatedBy, now)
	saved, err := scanBusinessModel(row)
	if err != nil {
		return Model{}, fmt.Errorf("insert business model: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_business_model_audit
			(org_id, product_surface, version, action, changed_by, model_json)
		VALUES ($1,$2,$3,'save',$4,$5)
	`, saved.OrgID, saved.ProductSurface, saved.Version, saved.CreatedBy, raw)
	if err != nil {
		return Model{}, fmt.Errorf("audit business model: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, fmt.Errorf("commit business model save: %w", err)
	}
	committed = true
	return saved, nil
}

func scanBusinessModel(row interface{ Scan(dest ...any) error }) (Model, error) {
	var (
		model                                        Model
		raw                                          []byte
		id, orgID, productSurface, status, createdBy string
		version                                      int
		createdAt, updatedAt                         time.Time
	)
	if err := row.Scan(&id, &orgID, &productSurface, &version, &status, &raw, &createdBy, &createdAt, &updatedAt); err != nil {
		return Model{}, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &model); err != nil {
			return Model{}, fmt.Errorf("unmarshal business model: %w", err)
		}
	}
	model.ID = strings.TrimSpace(id)
	model.OrgID = orgID
	model.ProductSurface = productSurface
	model.Version = version
	model.Status = status
	model.CreatedBy = createdBy
	model.CreatedAt = createdAt
	model.UpdatedAt = updatedAt
	return model, nil
}
