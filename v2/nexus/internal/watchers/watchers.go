package watchers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ExpiredApproval struct {
	ID                uuid.UUID
	OrgID             string
	GovernanceCheckID uuid.UUID
	VirployeeID       string
	BindingHash       string
	ExpiresAt         time.Time
}

type RepositoryPort interface {
	ExpireApprovals(context.Context, time.Time, int) ([]ExpiredApproval, error)
}

type AuditPort interface {
	Append(context.Context, string, auditdomain.AppendInput) (auditdomain.AuditEvent, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// ExpireApprovals claims due rows inside the transaction. SKIP LOCKED makes
// the operation safe when several Nexus replicas run the watcher concurrently.
func (r *Repository) ExpireApprovals(ctx context.Context, now time.Time, limit int) ([]ExpiredApproval, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		WITH due AS (
			SELECT id FROM approvals
			WHERE status = 'pending' AND expires_at <= $1
			ORDER BY expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE approvals a
		SET status = 'expired', decided_by = 'service:nexus-watcher',
			decision_note = 'approval TTL elapsed', decided_at = $1, updated_at = $1
		FROM due
		WHERE a.id = due.id
		RETURNING a.id, a.org_id, a.governance_check_id, a.requester_id,
			a.binding_hash, a.expires_at
	`, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpiredApproval
	var checkIDs []uuid.UUID
	for rows.Next() {
		var item ExpiredApproval
		if err := rows.Scan(&item.ID, &item.OrgID, &item.GovernanceCheckID, &item.VirployeeID, &item.BindingHash, &item.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, item)
		checkIDs = append(checkIDs, item.GovernanceCheckID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(checkIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE governance_checks SET status = 'expired' WHERE id = ANY($1)`, checkIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

type Watcher struct {
	repo  RepositoryPort
	audit AuditPort
	now   func() time.Time
}

func New(repo RepositoryPort, audit AuditPort) *Watcher {
	return &Watcher{repo: repo, audit: audit, now: time.Now}
}

func (w *Watcher) RunOnce(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		return 0, fmt.Errorf("watcher batch must be positive")
	}
	items, err := w.repo.ExpireApprovals(ctx, w.now().UTC(), batch)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		_, err := w.audit.Append(ctx, item.OrgID, auditdomain.AppendInput{
			VirployeeID: item.VirployeeID,
			SubjectType: "approval",
			SubjectID:   item.ID.String(),
			EventType:   "approval_expired",
			ActorType:   "service",
			ActorID:     "nexus-watcher",
			Summary:     "approval expired without a decision",
			Data: map[string]any{
				"approval_id": item.ID.String(), "governance_check_id": item.GovernanceCheckID.String(),
				"binding_hash": item.BindingHash, "expires_at": item.ExpiresAt.UTC().Format(time.RFC3339Nano),
			},
		})
		if err != nil {
			slog.ErrorContext(ctx, "audit emit failed for approval expiration", "error", err, "approval_id", item.ID)
		}
	}
	return len(items), nil
}

var _ RepositoryPort = (*Repository)(nil)
