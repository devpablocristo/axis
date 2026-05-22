package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	approvaldomain "github.com/devpablocristo/nexus/governance/internal/approvals/usecases/domain"
	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

// DecisionApplier abre una transacción y bloquea la fila del approval con
// SELECT ... FOR UPDATE. Esto resuelve dos cosas a la vez:
//   - C10: approval + request se actualizan atómicamente (o nada).
//   - C11: bajo concurrencia (break-glass multi-sig, doble click), dos
//     decisiones simultáneas sobre el mismo approval se serializan vía el
//     row lock; el segundo ve el estado actualizado del primero.
type DecisionApplier struct {
	db *sharedpostgres.DB
}

func NewDecisionApplier(db *sharedpostgres.DB) *DecisionApplier {
	return &DecisionApplier{db: db}
}

// BeginDecision abre una tx y devuelve un Lock junto al snapshot bloqueado
// del approval. El caller MUST llamar a alguno de PersistPartial /
// PersistFinal / Rollback (este último es seguro deferrear sin condicional).
func (d *DecisionApplier) BeginDecision(ctx context.Context, approvalID uuid.UUID) (DecisionLock, approvaldomain.Approval, error) {
	tx, err := d.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, approvaldomain.Approval{}, fmt.Errorf("begin decision tx: %w", err)
	}
	row := tx.QueryRow(ctx, selectApprovalSQL+` WHERE id = $1 FOR UPDATE`, approvalID)
	a, err := scanApproval(row)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, approvaldomain.Approval{}, ErrNotFound
		}
		return nil, approvaldomain.Approval{}, fmt.Errorf("lock approval: %w", err)
	}
	return &pgDecisionLock{tx: tx}, a, nil
}

// DecisionLock representa el handle de una tx con la fila del approval ya
// bloqueada. Cualquier otra decisión sobre el mismo approval queda en cola.
//
// La request se lee fuera de la tx vía requestRepo: como el lock sobre la
// fila approvals serializa a todos los flujos que pueden mutar la request
// vía approval-decision, leerla por fuera no introduce race.
type DecisionLock interface {
	// PersistPartial actualiza solo la decisión del approval (break-glass
	// aún sin alcanzar RequiredApprovals) y commitea la tx.
	PersistPartial(ctx context.Context, a approvaldomain.Approval) error
	// PersistFinal actualiza approval + request en la misma tx y commitea.
	PersistFinal(ctx context.Context, a approvaldomain.Approval, r requestdomain.Request) error
	// Rollback descarta la tx. Idempotente: noop si Persist* ya commiteó.
	Rollback(ctx context.Context) error
}

type pgDecisionLock struct {
	tx       pgx.Tx
	finished bool
}

func (l *pgDecisionLock) PersistPartial(ctx context.Context, a approvaldomain.Approval) error {
	if l.finished {
		return errors.New("decision lock already finished")
	}
	if err := updateApprovalInTx(ctx, l.tx, a); err != nil {
		return err
	}
	if err := l.tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit partial decision: %w", err)
	}
	l.finished = true
	return nil
}

func (l *pgDecisionLock) PersistFinal(ctx context.Context, a approvaldomain.Approval, r requestdomain.Request) error {
	if l.finished {
		return errors.New("decision lock already finished")
	}
	if err := updateApprovalInTx(ctx, l.tx, a); err != nil {
		return err
	}
	if err := updateRequestInTx(ctx, l.tx, r); err != nil {
		return err
	}
	if err := l.tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit final decision: %w", err)
	}
	l.finished = true
	return nil
}

func (l *pgDecisionLock) Rollback(ctx context.Context) error {
	if l.finished {
		return nil
	}
	l.finished = true
	return l.tx.Rollback(ctx)
}

func updateApprovalInTx(ctx context.Context, tx pgx.Tx, a approvaldomain.Approval) error {
	decisionsJSON, err := json.Marshal(a.Decisions)
	if err != nil {
		return fmt.Errorf("marshal decisions: %w", err)
	}
	if a.Decisions == nil {
		decisionsJSON = []byte("[]")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE approvals
		SET status = $2, decided_by = $3, decision_note = $4, decided_at = $5, decisions = $6
		WHERE id = $1
	`, a.ID, a.Status, a.DecidedBy, a.DecisionNote, a.DecidedAt, decisionsJSON)
	if err != nil {
		return fmt.Errorf("update approval in tx: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func updateRequestInTx(ctx context.Context, tx pgx.Tx, r requestdomain.Request) error {
	tag, err := tx.Exec(ctx, `
		UPDATE requests SET
			status = $2, risk_level = $3, decision = $4, decision_reason = $5,
			policy_id = $6, approval_id = $7, execution_result = $8, error_message = $9,
			ai_summary = $10, ai_degraded = $11,
			evaluated_at = $12, decided_at = $13, executed_at = $14, expires_at = $15, updated_at = $16
		WHERE id = $1
	`,
		r.ID, r.Status, r.RiskLevel, r.Decision, r.DecisionReason,
		r.PolicyID, r.ApprovalID, r.ExecutionResult, r.ErrorMessage,
		r.AISummary, r.AIDegraded,
		r.EvaluatedAt, r.DecidedAt, r.ExecutedAt, r.ExpiresAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update request in tx: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update request in tx: request not found")
	}
	return nil
}
