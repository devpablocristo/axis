package approvals

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, tenantID string, status domain.Status, limit int, after *domain.ListCursor) ([]domain.Approval, error) {
	args := []any{tenantID, string(status), limit}
	cursorClause := ""
	if after != nil {
		cursorClause = " AND (created_at, id) < ($4, $5::uuid)"
		args = append(args, after.CreatedAt, after.ID.String())
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+approvalColumns("a")+`
		FROM approvals a
		WHERE a.tenant_id = $1
			AND a.status = $2
			`+cursorClause+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $3
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Approval{}
	for rows.Next() {
		item, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	return r.get(ctx, tenantID, id)
}

func (r *Repository) Decide(ctx context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID, actorRole, note string) (domain.Approval, error) {
	now := time.Now().UTC()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Approval{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, err := scanApproval(tx.QueryRow(ctx, `SELECT `+approvalColumns("a")+` FROM approvals a WHERE a.tenant_id=$1 AND a.id=$2 FOR UPDATE`, tenantID, id))
	if err != nil {
		return domain.Approval{}, err
	}
	if existing.Status != domain.StatusPending {
		return domain.Approval{}, domainerr.Conflict("approval is already decided")
	}
	if !existing.ExpiresAt.After(now) {
		return domain.Approval{}, domainerr.Conflict("approval has expired")
	}
	decision := "approve"
	if status == domain.StatusRejected {
		decision = "reject"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO approval_decisions
		(id,tenant_id,approval_id,actor_id,actor_role,decision,note,decided_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, uuid.New(), tenantID, id, actorID, actorRole, decision, note, now); err != nil {
		if isUniqueViolation(err) {
			return domain.Approval{}, domainerr.Conflict("actor already decided this approval")
		}
		return domain.Approval{}, err
	}
	terminalStatus := domain.StatusPending
	if status == domain.StatusRejected {
		terminalStatus = domain.StatusRejected
	} else {
		var approvals int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM approval_decisions WHERE tenant_id=$1 AND approval_id=$2 AND decision='approve'`, tenantID, id).Scan(&approvals); err != nil {
			return domain.Approval{}, err
		}
		if approvals >= existing.QuorumRequired {
			terminalStatus = domain.StatusApproved
		}
	}
	if terminalStatus == domain.StatusPending {
		_, err = tx.Exec(ctx, `UPDATE approvals SET updated_at=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, id, now)
	} else {
		_, err = tx.Exec(ctx, `UPDATE approvals SET status=$3,decided_by=$4,decision_note=$5,decided_at=$6,updated_at=$6 WHERE tenant_id=$1 AND id=$2`, tenantID, id, string(terminalStatus), actorID, note, now)
	}
	if err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Approval{}, err
	}
	return r.Get(ctx, tenantID, id)
}

func (r *Repository) Review(ctx context.Context, tenantID string, id uuid.UUID, actorID, note string) (domain.Approval, error) {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `UPDATE approvals SET reviewed_by=$3,review_note=$4,reviewed_at=$5,updated_at=$5
		WHERE tenant_id=$1 AND id=$2 AND approval_kind='break_glass' AND status='approved'
		AND post_review_required AND reviewed_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM approval_decisions d WHERE d.tenant_id=$1 AND d.approval_id=$2 AND d.actor_id=$3)`, tenantID, id, actorID, note, now)
	if err != nil {
		return domain.Approval{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Approval{}, domainerr.Conflict("break-glass approval is not reviewable by this actor")
	}
	return r.Get(ctx, tenantID, id)
}

func (r *Repository) get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	item, err := scanApproval(r.pool.QueryRow(ctx, `SELECT `+approvalColumns("a")+` FROM approvals a WHERE a.tenant_id=$1 AND a.id=$2`, tenantID, id))
	if err != nil {
		return domain.Approval{}, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id,actor_id,actor_role,decision,note,decided_at FROM approval_decisions WHERE tenant_id=$1 AND approval_id=$2 ORDER BY decided_at,id`, tenantID, id)
	if err != nil {
		return domain.Approval{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var decision domain.Decision
		if err := rows.Scan(&decision.ID, &decision.ActorID, &decision.ActorRole, &decision.Decision, &decision.Note, &decision.DecidedAt); err != nil {
			return domain.Approval{}, err
		}
		item.Decisions = append(item.Decisions, decision)
	}
	return item, rows.Err()
}

func approvalColumns(alias string) string {
	p := alias + "."
	return p + `id::text,` + p + `tenant_id,` + p + `governance_check_id::text,` + p + `requester_id,` + p + `product_surface,` + p + `action_type,` + p + `target_system,` + p + `target_resource,` + p + `resource_type,` + p + `risk_level,` + p + `reason,` + p + `binding_hash,` + p + `governance_policy_snapshot_hash,` + p + `status,` + p + `approval_kind,` + p + `supervisor_user_id,` + p + `quorum_required,(SELECT count(*) FROM approval_decisions dc WHERE dc.tenant_id=` + p + `tenant_id AND dc.approval_id=` + p + `id AND dc.decision='approve'),` + p + `post_review_required,` + p + `reviewed_by,` + p + `review_note,` + p + `reviewed_at,` + p + `decided_by,` + p + `decision_note,` + p + `decided_at,` + p + `expires_at,` + p + `created_at,` + p + `updated_at`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApproval(row scanner) (domain.Approval, error) {
	var idText string
	var governanceCheckIDText string
	var status string
	var decidedAt sql.NullTime
	var reviewedAt sql.NullTime
	var item domain.Approval
	err := row.Scan(
		&idText,
		&item.TenantID,
		&governanceCheckIDText,
		&item.RequesterID,
		&item.ProductSurface,
		&item.ActionType,
		&item.TargetSystem,
		&item.TargetResource,
		&item.ResourceType,
		&item.RiskLevel,
		&item.Reason,
		&item.BindingHash,
		&item.PolicySnapshotHash,
		&status,
		&item.ApprovalKind,
		&item.SupervisorUserID,
		&item.QuorumRequired,
		&item.ApprovalCount,
		&item.PostReviewRequired,
		&item.ReviewedBy,
		&item.ReviewNote,
		&reviewedAt,
		&item.DecidedBy,
		&item.DecisionNote,
		&decidedAt,
		&item.ExpiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Approval{}, domainerr.NotFound("approval not found")
	}
	if err != nil {
		return domain.Approval{}, err
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return domain.Approval{}, err
	}
	governanceCheckID, err := uuid.Parse(governanceCheckIDText)
	if err != nil {
		return domain.Approval{}, err
	}
	item.ID = id
	item.GovernanceCheckID = governanceCheckID
	item.Status = domain.Status(status)
	if decidedAt.Valid {
		item.DecidedAt = &decidedAt.Time
	}
	if reviewedAt.Valid {
		item.ReviewedAt = &reviewedAt.Time
	}
	return item, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

var _ RepositoryPort = (*Repository)(nil)
