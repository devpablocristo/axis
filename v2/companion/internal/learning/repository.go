package learning

import (
	"context"
	"encoding/json"
	"errors"
	"time"

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

const proposalColumns = `id::text, tenant_id, virployee_id::text, capability_key, title, content, content_hash,
	evidence, source_trace_ids, status, proposed_by, succeeded_watermark, created_at, updated_at`

func (r *Repository) Create(ctx context.Context, tenantID string, input NormalizedCreateInput) (Proposal, error) {
	evidence, err := json.Marshal(input.Evidence)
	if err != nil {
		return Proposal{}, err
	}
	sources, err := json.Marshal(input.SourceTraceIDs)
	if err != nil {
		return Proposal{}, err
	}
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO companion_learning_proposals (
			id, tenant_id, virployee_id, capability_key, title, content, content_hash,
			evidence, source_trace_ids, status, proposed_by, succeeded_watermark, created_at, updated_at
		)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, 'pending', $10, $11, $12, $12)
		RETURNING `+proposalColumns+`
	`, uuid.New().String(), tenantID, input.VirployeeID.String(), input.CapabilityKey, input.Title,
		input.Content, input.ContentHash, evidence, sources, input.ProposedBy, input.SucceededWatermark, now)
	return scanProposal(row)
}

func (r *Repository) List(ctx context.Context, tenantID, status string, virployeeID *uuid.UUID) ([]Proposal, error) {
	query := `
		SELECT ` + proposalColumns + `
		FROM companion_learning_proposals
		WHERE tenant_id = $1 AND status = $2`
	args := []any{tenantID, status}
	if virployeeID != nil {
		query += ` AND virployee_id = $3::uuid`
		args = append(args, virployeeID.String())
	}
	query += ` ORDER BY created_at DESC, id DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Proposal{}
	for rows.Next() {
		proposal, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, proposal)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (Proposal, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+proposalColumns+`
		FROM companion_learning_proposals
		WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, id.String())
	return scanProposal(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProposal(row scanner) (Proposal, error) {
	var idText, virployeeText string
	var evidence, sources []byte
	var out Proposal
	err := row.Scan(
		&idText, &out.TenantID, &virployeeText, &out.CapabilityKey, &out.Title, &out.Content,
		&out.ContentHash, &evidence, &sources, &out.Status, &out.ProposedBy, &out.SucceededWatermark, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, domainerr.NotFound("learning proposal not found")
	}
	if err != nil {
		return Proposal{}, mapConflict(err)
	}
	if out.ID, err = uuid.Parse(idText); err != nil {
		return Proposal{}, err
	}
	if out.VirployeeID, err = uuid.Parse(virployeeText); err != nil {
		return Proposal{}, err
	}
	if err := json.Unmarshal(evidence, &out.Evidence); err != nil {
		out.Evidence = map[string]any{}
	}
	if err := json.Unmarshal(sources, &out.SourceTraceIDs); err != nil {
		out.SourceTraceIDs = []string{}
	}
	if out.SourceTraceIDs == nil {
		out.SourceTraceIDs = []string{}
	}
	if out.Evidence == nil {
		out.Evidence = map[string]any{}
	}
	return out, nil
}

func mapConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domainerr.Conflict("a pending proposal already exists for this capability")
	}
	return err
}

var _ RepositoryPort = (*Repository)(nil)
