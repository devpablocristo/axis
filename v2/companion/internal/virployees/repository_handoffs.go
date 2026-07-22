package virployees

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) CreateHandoff(ctx context.Context, orgID string, fromID uuid.UUID, actorID string, in CreateHandoffInput) (Handoff, error) {
	id := uuid.New()
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO companion_handoffs (
			id,org_id,case_id,source_run_id,from_virployee_id,to_virployee_id,
			reason_code,note,note_hash,requested_by,expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, id, orgID, in.CaseID, nullableUUIDPtr(in.SourceRunID), fromID, in.ToID, strings.TrimSpace(in.ReasonCode),
		strings.TrimSpace(in.Note), hashOptional(in.Note), actorID, now.Add(time.Hour))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "one_pending") {
			return Handoff{}, domainerr.Conflict("assist case already has a pending handoff")
		}
		return Handoff{}, err
	}
	return r.GetHandoff(ctx, orgID, id)
}

func (r *Repository) GetHandoff(ctx context.Context, orgID string, id uuid.UUID) (Handoff, error) {
	return r.scanHandoff(r.pool.QueryRow(ctx, handoffSelect+` WHERE org_id=$1 AND id=$2`, orgID, id))
}

func (r *Repository) ListHandoffs(ctx context.Context, orgID, status string, limit int) ([]Handoff, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, handoffSelect+` WHERE org_id=$1 AND ($2='' OR status=$2) ORDER BY created_at DESC,id LIMIT $3`, orgID, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Handoff, 0)
	for rows.Next() {
		item, err := r.scanHandoff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

const handoffColumns = `id,org_id,case_id,source_run_id,from_virployee_id,to_virployee_id,
	reason_code,note,note_hash,status,requested_by,decided_by,decision_note,version,expires_at,
	created_at,updated_at,decided_at`
const handoffSelect = `SELECT ` + handoffColumns + ` FROM companion_handoffs`

func (r *Repository) scanHandoff(row rowScanner) (Handoff, error) {
	var out Handoff
	if err := row.Scan(&out.ID, &out.OrgID, &out.CaseID, &out.SourceRunID, &out.FromVirployeeID, &out.ToVirployeeID,
		&out.ReasonCode, &out.Note, &out.NoteHash, &out.Status, &out.RequestedBy, &out.DecidedBy, &out.DecisionNote,
		&out.Version, &out.ExpiresAt, &out.CreatedAt, &out.UpdatedAt, &out.DecidedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Handoff{}, domainerr.NotFound("handoff not found")
		}
		return Handoff{}, err
	}
	return out, nil
}

func (r *Repository) DecideHandoff(ctx context.Context, orgID string, id uuid.UUID, actorID, decision string, in DecideHandoffInput) (Handoff, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Handoff{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var handoff Handoff
	handoff, err = r.scanHandoff(tx.QueryRow(ctx, handoffSelect+` WHERE org_id=$1 AND id=$2 FOR UPDATE`, orgID, id))
	if err != nil {
		return Handoff{}, err
	}
	if handoff.Status != "pending" || handoff.Version != in.Version {
		return Handoff{}, domainerr.Conflict("handoff is no longer pending")
	}
	if !handoff.ExpiresAt.After(time.Now().UTC()) {
		_, _ = tx.Exec(ctx, `UPDATE companion_handoffs SET status='expired',version=version+1,updated_at=now(),decided_at=now() WHERE org_id=$1 AND id=$2`, orgID, id)
		if err = tx.Commit(ctx); err != nil {
			return Handoff{}, err
		}
		return Handoff{}, domainerr.Conflict("handoff has expired")
	}
	status := "rejected"
	if decision == "accept" {
		status = "accepted"
	}
	tag, err := tx.Exec(ctx, `UPDATE companion_handoffs SET status=$4,decided_by=$5,decision_note=$6,
		version=version+1,decided_at=now(),updated_at=now() WHERE org_id=$1 AND id=$2 AND status='pending' AND version=$3`,
		orgID, id, in.Version, status, actorID, strings.TrimSpace(in.Note))
	if err != nil {
		return Handoff{}, err
	}
	if tag.RowsAffected() != 1 {
		return Handoff{}, domainerr.Conflict("handoff decision lost a concurrent race")
	}
	if status == "accepted" {
		tag, err = tx.Exec(ctx, `UPDATE companion_assist_cases SET owner_virployee_id=$3,status='open',version=version+1,updated_at=now()
			WHERE org_id=$1 AND id=$2 AND owner_virployee_id=$4`, orgID, handoff.CaseID, handoff.ToVirployeeID, handoff.FromVirployeeID)
		if err != nil {
			return Handoff{}, err
		}
		if tag.RowsAffected() != 1 {
			return Handoff{}, domainerr.Conflict("assist case owner changed before handoff acceptance")
		}
		_, err = tx.Exec(ctx, `UPDATE companion_assist_runs SET responsible_virployee_id=$3,ownership_version=ownership_version+1,updated_at=now()
			WHERE org_id=$1 AND case_id=$2 AND status NOT IN ('done','failed','needs_human')`, orgID, handoff.CaseID, handoff.ToVirployeeID)
		if err != nil {
			return Handoff{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Handoff{}, err
	}
	return r.GetHandoff(ctx, orgID, id)
}

func (r *Repository) CancelHandoff(ctx context.Context, orgID string, id uuid.UUID, actorID string, version int64) (Handoff, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE companion_handoffs SET status='cancelled',decided_by=$4,version=version+1,decided_at=now(),updated_at=now()
		WHERE org_id=$1 AND id=$2 AND version=$3 AND status='pending'`, orgID, id, version, actorID)
	if err != nil {
		return Handoff{}, err
	}
	if tag.RowsAffected() != 1 {
		return Handoff{}, domainerr.Conflict("handoff is no longer pending")
	}
	return r.GetHandoff(ctx, orgID, id)
}

func (r *Repository) ExpireHandoffs(ctx context.Context, limit int) ([]Handoff, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `WITH due AS (
		SELECT id FROM companion_handoffs WHERE status='pending' AND expires_at<=now() ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $1
	) UPDATE companion_handoffs h SET status='expired',version=version+1,decided_at=now(),updated_at=now()
	FROM due WHERE h.id=due.id RETURNING h.id,h.org_id,h.case_id,h.source_run_id,h.from_virployee_id,h.to_virployee_id,
		h.reason_code,h.note,h.note_hash,h.status,h.requested_by,h.decided_by,h.decision_note,h.version,h.expires_at,
		h.created_at,h.updated_at,h.decided_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Handoff, 0)
	for rows.Next() {
		item, err := r.scanHandoff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ActiveRunForCase(ctx context.Context, orgID string, caseID uuid.UUID) (AssistRun, error) {
	return r.scanAssistRun(r.pool.QueryRow(ctx, assistRunSelect+` WHERE org_id=$1 AND case_id=$2 AND status NOT IN ('done','failed','needs_human') ORDER BY started_at DESC LIMIT 1`, orgID, caseID))
}
