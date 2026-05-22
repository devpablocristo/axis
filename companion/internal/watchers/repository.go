package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// Repository port de persistencia para watchers y proposals.
type Repository interface {
	CreateWatcher(ctx context.Context, w domain.Watcher) (domain.Watcher, error)
	GetWatcher(ctx context.Context, id uuid.UUID) (domain.Watcher, error)
	ListWatchers(ctx context.Context, orgID string) ([]domain.Watcher, error)
	ListEnabledOrgIDs(ctx context.Context) ([]string, error)
	UpdateWatcher(ctx context.Context, w domain.Watcher) (domain.Watcher, error)
	DeleteWatcher(ctx context.Context, id uuid.UUID) error

	CreateProposal(ctx context.Context, p domain.Proposal) (domain.Proposal, error)
	UpdateProposal(ctx context.Context, p domain.Proposal) error
	ListProposalsByWatcher(ctx context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error)
	PendingProposals(ctx context.Context, orgID string) ([]domain.Proposal, error)
}

// PostgresRepository implementa Repository con pgx.
type PostgresRepository struct {
	db *sharedpostgres.DB
}

// NewPostgresRepository crea un repositorio PostgreSQL.
func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// --- Watchers ---

func (r *PostgresRepository) CreateWatcher(ctx context.Context, w domain.Watcher) (domain.Watcher, error) {
	now := time.Now().UTC()
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	w.CreatedAt = now
	w.UpdatedAt = now
	if w.Config == nil {
		w.Config = json.RawMessage(`{}`)
	}

	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_watchers (id, org_id, name, watcher_type, config, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		w.ID, w.OrgID, w.Name, string(w.WatcherType), w.Config, w.Enabled, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return domain.Watcher{}, fmt.Errorf("create watcher: %w", err)
	}
	return w, nil
}

func (r *PostgresRepository) GetWatcher(ctx context.Context, id uuid.UUID) (domain.Watcher, error) {
	row := r.db.Pool().QueryRow(ctx, `
		SELECT id, org_id, name, watcher_type, config, enabled, last_run_at, last_result, created_at, updated_at
		FROM companion_watchers WHERE id = $1`, id)
	return scanWatcher(row)
}

func (r *PostgresRepository) ListWatchers(ctx context.Context, orgID string) ([]domain.Watcher, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, org_id, name, watcher_type, config, enabled, last_run_at, last_result, created_at, updated_at
		FROM companion_watchers WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list watchers: %w", err)
	}
	defer rows.Close()

	var result []domain.Watcher
	for rows.Next() {
		w, err := scanWatcherRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan watcher: %w", err)
		}
		result = append(result, w)
	}
	return result, nil
}

func (r *PostgresRepository) UpdateWatcher(ctx context.Context, w domain.Watcher) (domain.Watcher, error) {
	w.UpdatedAt = time.Now().UTC()
	_, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_watchers
		SET name = $2, config = $3, enabled = $4, last_run_at = $5, last_result = $6, updated_at = $7
		WHERE id = $1`,
		w.ID, w.Name, w.Config, w.Enabled, w.LastRunAt, w.LastResult, w.UpdatedAt,
	)
	if err != nil {
		return domain.Watcher{}, fmt.Errorf("update watcher: %w", err)
	}
	return w, nil
}

func (r *PostgresRepository) ListEnabledOrgIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT DISTINCT org_id FROM companion_watchers WHERE enabled = true ORDER BY org_id`)
	if err != nil {
		return nil, fmt.Errorf("list enabled org ids: %w", err)
	}
	defer rows.Close()

	var orgIDs []string
	for rows.Next() {
		var orgID string
		if err := rows.Scan(&orgID); err != nil {
			return nil, fmt.Errorf("scan org_id: %w", err)
		}
		orgIDs = append(orgIDs, orgID)
	}
	return orgIDs, nil
}

func (r *PostgresRepository) DeleteWatcher(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM companion_watchers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete watcher: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Proposals ---

func (r *PostgresRepository) CreateProposal(ctx context.Context, p domain.Proposal) (domain.Proposal, error) {
	now := time.Now().UTC()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	p.CreatedAt = now
	if p.ExecutionStatus == "" {
		p.ExecutionStatus = domain.ProposalPending
	}
	if p.Params == nil {
		p.Params = json.RawMessage(`{}`)
	}

	_, err := r.db.Pool().Exec(ctx, `
		INSERT INTO companion_proposals (id, watcher_id, org_id, action_type, target_resource, params, reason, governance_request_id, governance_decision, execution_status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		p.ID, p.WatcherID, p.OrgID, p.ActionType, p.TargetResource, p.Params, p.Reason, p.GovernanceRequestID, p.GovernanceDecision, p.ExecutionStatus, p.CreatedAt,
	)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("create proposal: %w", err)
	}
	return p, nil
}

func (r *PostgresRepository) UpdateProposal(ctx context.Context, p domain.Proposal) error {
	_, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_proposals
		SET governance_request_id = $2, governance_decision = $3, execution_status = $4, execution_result = $5, resolved_at = $6
		WHERE id = $1`,
		p.ID, p.GovernanceRequestID, p.GovernanceDecision, p.ExecutionStatus, p.ExecutionResult, p.ResolvedAt,
	)
	if err != nil {
		return fmt.Errorf("update proposal: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListProposalsByWatcher(ctx context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, watcher_id, org_id, action_type, target_resource, params, reason, governance_request_id, governance_decision, execution_status, execution_result, created_at, resolved_at
		FROM companion_proposals WHERE watcher_id = $1 ORDER BY created_at DESC LIMIT $2`, watcherID, limit)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()

	var result []domain.Proposal
	for rows.Next() {
		p, err := scanProposalRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		result = append(result, p)
	}
	return result, nil
}

func (r *PostgresRepository) PendingProposals(ctx context.Context, orgID string) ([]domain.Proposal, error) {
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, watcher_id, org_id, action_type, target_resource, params, reason, governance_request_id, governance_decision, execution_status, execution_result, created_at, resolved_at
		FROM companion_proposals WHERE org_id = $1 AND execution_status = 'pending' ORDER BY created_at`, orgID)
	if err != nil {
		return nil, fmt.Errorf("pending proposals: %w", err)
	}
	defer rows.Close()

	var result []domain.Proposal
	for rows.Next() {
		p, err := scanProposalRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		result = append(result, p)
	}
	return result, nil
}

// --- Scanners ---

func scanWatcher(row pgx.Row) (domain.Watcher, error) {
	var w domain.Watcher
	var watcherType string
	err := row.Scan(&w.ID, &w.OrgID, &w.Name, &watcherType, &w.Config, &w.Enabled, &w.LastRunAt, &w.LastResult, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Watcher{}, ErrNotFound
		}
		return domain.Watcher{}, fmt.Errorf("scan watcher: %w", err)
	}
	w.WatcherType = domain.WatcherType(watcherType)
	return w, nil
}

func scanWatcherRows(rows pgx.Rows) (domain.Watcher, error) {
	var w domain.Watcher
	var watcherType string
	err := rows.Scan(&w.ID, &w.OrgID, &w.Name, &watcherType, &w.Config, &w.Enabled, &w.LastRunAt, &w.LastResult, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return domain.Watcher{}, fmt.Errorf("scan watcher row: %w", err)
	}
	w.WatcherType = domain.WatcherType(watcherType)
	return w, nil
}

func scanProposalRows(rows pgx.Rows) (domain.Proposal, error) {
	var p domain.Proposal
	err := rows.Scan(&p.ID, &p.WatcherID, &p.OrgID, &p.ActionType, &p.TargetResource, &p.Params, &p.Reason, &p.GovernanceRequestID, &p.GovernanceDecision, &p.ExecutionStatus, &p.ExecutionResult, &p.CreatedAt, &p.ResolvedAt)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("scan proposal row: %w", err)
	}
	return p, nil
}
