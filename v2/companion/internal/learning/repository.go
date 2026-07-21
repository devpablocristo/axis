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
	evidence, source_trace_ids, status, proposed_by, succeeded_watermark,
	decided_by, decided_at, COALESCE(memory_id::text, ''), created_at, updated_at`

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
	var idText, virployeeText, memoryIDText string
	var evidence, sources []byte
	var out Proposal
	err := row.Scan(
		&idText, &out.TenantID, &virployeeText, &out.CapabilityKey, &out.Title, &out.Content,
		&out.ContentHash, &evidence, &sources, &out.Status, &out.ProposedBy, &out.SucceededWatermark,
		&out.DecidedBy, &out.DecidedAt, &memoryIDText, &out.CreatedAt, &out.UpdatedAt,
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
	if memoryIDText != "" {
		memoryID, err := uuid.Parse(memoryIDText)
		if err != nil {
			return Proposal{}, err
		}
		out.MemoryID = &memoryID
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

// Decide transitions a PENDING proposal to accepted or dismissed, stamping the
// human actor, the decision time, and (on accept) the installed memory id. The
// WHERE status='pending' guard makes the transition atomic and idempotent-safe:
// a proposal already decided (by a racing request) matches no row and surfaces
// as Conflict, so the usecase never double-installs.
func (r *Repository) Decide(ctx context.Context, tenantID string, id uuid.UUID, status, decidedBy string, memoryID *uuid.UUID) (Proposal, error) {
	var memoryArg any
	if memoryID != nil {
		memoryArg = memoryID.String()
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE companion_learning_proposals
		SET status = $3, decided_by = $4, decided_at = $5, memory_id = $6::uuid, updated_at = $5
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'pending'
		RETURNING `+proposalColumns+`
	`, tenantID, id.String(), status, decidedBy, time.Now().UTC(), memoryArg)
	proposal, err := scanProposal(row)
	if domainerr.IsNotFound(err) {
		return Proposal{}, domainerr.Conflict("proposal is no longer pending")
	}
	return proposal, err
}

// AttachMemory pins the installed memory id onto an already-accepted proposal.
// It runs after the pending→accepted claim (which happens before the install),
// so it is guarded on status='accepted'. A no-match means the proposal is not
// accepted (raced) and is treated as a NotFound by the caller.
func (r *Repository) AttachMemory(ctx context.Context, tenantID string, id, memoryID uuid.UUID) (Proposal, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE companion_learning_proposals
		SET memory_id = $3::uuid, updated_at = $4
		WHERE tenant_id = $1 AND id = $2::uuid AND status = 'accepted'
		RETURNING `+proposalColumns+`
	`, tenantID, id.String(), memoryID.String(), time.Now().UTC())
	return scanProposal(row)
}

func mapConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domainerr.Conflict("a pending proposal already exists for this capability")
	}
	return err
}

var _ RepositoryPort = (*Repository)(nil)
