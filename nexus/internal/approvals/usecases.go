package approvals

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	approvaldomain "github.com/devpablocristo/nexus/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus/internal/audit/usecases/domain"
	"github.com/devpablocristo/nexus/internal/callbacks"
	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
	"github.com/google/uuid"
)

type RequestUpdater interface {
	GetByID(ctx context.Context, id uuid.UUID) (requestdomain.Request, error)
	Update(ctx context.Context, r requestdomain.Request) (requestdomain.Request, error)
}

// AuditSink emite eventos al audit trail (best-effort).
type AuditSink interface {
	AppendEvent(ctx context.Context, requestID uuid.UUID, eventType, actorType, actorID, summary string, data map[string]any) error
}

// DecisionTx abre una transacción con row lock sobre el approval. Resuelve
// dos cosas a la vez: serializa decisiones concurrentes (C11) y persiste
// approval+request atómicamente (C10). Si está nil, el usecase cae al
// camino de fallback sin lock ni atomicidad — solo aceptable para tests con fakes.
type DecisionTx interface {
	BeginDecision(ctx context.Context, approvalID uuid.UUID) (DecisionLock, approvaldomain.Approval, error)
}

type Usecases struct {
	repo        Repository
	requestRepo RequestUpdater
	audit       AuditSink
	callbacks   callbacks.ApprovalPublisher
	decisionTx  DecisionTx
}

func NewUsecases(repo Repository, requestRepo RequestUpdater) *Usecases {
	return &Usecases{repo: repo, requestRepo: requestRepo}
}

// WithDecisionTx inyecta el applier transaccional. Si no se inyecta, los
// updates se hacen en dos pasos secuenciales (no-atómico, solo para tests).
func (u *Usecases) WithDecisionTx(tx DecisionTx) *Usecases {
	u.decisionTx = tx
	return u
}

// WithAuditSink inyecta el audit sink (patrón builder de v2).
func (u *Usecases) WithAuditSink(sink AuditSink) *Usecases {
	u.audit = sink
	return u
}

func (u *Usecases) WithApprovalCallbacks(publisher callbacks.ApprovalPublisher) *Usecases {
	u.callbacks = publisher
	return u
}

func (u *Usecases) ListPending(ctx context.Context, limit int, orgID *string, allowAll bool) ([]approvaldomain.Approval, error) {
	return u.repo.ListPending(ctx, limit, orgID, allowAll)
}

func (u *Usecases) GetByID(ctx context.Context, approvalID uuid.UUID) (approvaldomain.Approval, error) {
	return u.repo.GetByID(ctx, approvalID)
}

func (u *Usecases) Approve(ctx context.Context, approvalID uuid.UUID, decidedBy, note string) error {
	decidedBy = strings.TrimSpace(decidedBy)
	if decidedBy == "" {
		return ErrActorRequired
	}

	// Camino atómico con row lock (resuelve C10 + C11). Sin decisionTx
	// caemos al fallback: solo válido para tests con fakes.
	if u.decisionTx == nil {
		return u.approveDirect(ctx, approvalID, decidedBy, note)
	}

	lock, a, err := u.decisionTx.BeginDecision(ctx, approvalID)
	if err != nil {
		return fmt.Errorf("lock approval: %w", err)
	}
	defer func() { _ = lock.Rollback(ctx) }()

	if a.Status != approvaldomain.ApprovalStatusPending {
		return ErrNotPending
	}
	now := time.Now().UTC()
	if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
		return ErrExpired
	}

	if a.BreakGlass {
		// Acá ya tenemos lock: el snapshot a.Decisions refleja el último
		// commit, así que el chequeo de "ya decidió" es definitivo.
		for _, d := range a.Decisions {
			if d.ApproverID == decidedBy {
				return ErrAlreadyDecided
			}
		}
		a.Decisions = append(a.Decisions, approvaldomain.ApprovalDecision{
			ApproverID: decidedBy, Action: "approve", Note: note, DecidedAt: now,
		})
		approveCount := 0
		for _, d := range a.Decisions {
			if d.Action == "approve" {
				approveCount++
			}
		}
		if approveCount < a.RequiredApprovals {
			if err := lock.PersistPartial(ctx, a); err != nil {
				return fmt.Errorf("persist partial approval: %w", err)
			}
			u.emitAudit(ctx, a.RequestID, auditdomain.EventApproved, decidedBy,
				fmt.Sprintf("Partial approval (%d/%d): %s", approveCount, a.RequiredApprovals, note),
				map[string]any{"decided_by": decidedBy, "note": note, "approvals": approveCount, "required": a.RequiredApprovals})
			return nil
		}
	} else {
		a.Decisions = []approvaldomain.ApprovalDecision{
			{ApproverID: decidedBy, Action: "approve", Note: note, DecidedAt: now},
		}
	}

	a.Status = approvaldomain.ApprovalStatusApproved
	a.DecidedBy = decidedBy
	a.DecisionNote = note
	a.DecidedAt = &now

	req, err := u.requestRepo.GetByID(ctx, a.RequestID)
	if err != nil {
		return fmt.Errorf("get request for approval: %w", err)
	}
	req.Status = requestdomain.StatusApproved
	req.DecidedAt = &now
	req.UpdatedAt = now

	if err := lock.PersistFinal(ctx, a, req); err != nil {
		return fmt.Errorf("persist final approval: %w", err)
	}
	u.emitAudit(ctx, a.RequestID, auditdomain.EventApproved, decidedBy,
		"Approved: "+note, map[string]any{"decided_by": decidedBy, "note": note, "break_glass": a.BreakGlass})
	u.emitApprovalResolved(ctx, req, a)
	return nil
}

func (u *Usecases) Reject(ctx context.Context, approvalID uuid.UUID, decidedBy, note string) error {
	decidedBy = strings.TrimSpace(decidedBy)
	if decidedBy == "" {
		return ErrActorRequired
	}

	if u.decisionTx == nil {
		return u.rejectDirect(ctx, approvalID, decidedBy, note)
	}

	lock, a, err := u.decisionTx.BeginDecision(ctx, approvalID)
	if err != nil {
		return fmt.Errorf("lock approval: %w", err)
	}
	defer func() { _ = lock.Rollback(ctx) }()

	if a.Status != approvaldomain.ApprovalStatusPending {
		return ErrNotPending
	}
	now := time.Now().UTC()
	if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
		return ErrExpired
	}

	if a.BreakGlass {
		for _, d := range a.Decisions {
			if d.ApproverID == decidedBy {
				return ErrAlreadyDecided
			}
		}
	}

	// Un rechazo siempre finaliza (en break-glass o no).
	a.Decisions = append(a.Decisions, approvaldomain.ApprovalDecision{
		ApproverID: decidedBy, Action: "reject", Note: note, DecidedAt: now,
	})
	a.Status = approvaldomain.ApprovalStatusRejected
	a.DecidedBy = decidedBy
	a.DecisionNote = note
	a.DecidedAt = &now

	req, err := u.requestRepo.GetByID(ctx, a.RequestID)
	if err != nil {
		return fmt.Errorf("get request for rejection: %w", err)
	}
	req.Status = requestdomain.StatusRejected
	req.DecidedAt = &now
	req.UpdatedAt = now

	if err := lock.PersistFinal(ctx, a, req); err != nil {
		return fmt.Errorf("persist final rejection: %w", err)
	}
	u.emitAudit(ctx, a.RequestID, auditdomain.EventRejected, decidedBy,
		"Rejected: "+note, map[string]any{"decided_by": decidedBy, "note": note, "break_glass": a.BreakGlass})
	u.emitApprovalResolved(ctx, req, a)
	return nil
}

// approveDirect / rejectDirect: paths directos sin row-lock ni atomicidad. Solo
// aceptables para tests con fakes (in-memory). En prod wire siempre inyecta
// DecisionTx, así que estos no se llaman.
func (u *Usecases) approveDirect(ctx context.Context, approvalID uuid.UUID, decidedBy, note string) error {
	a, err := u.repo.GetByID(ctx, approvalID)
	if err != nil {
		return fmt.Errorf("get approval: %w", err)
	}
	if a.Status != approvaldomain.ApprovalStatusPending {
		return ErrNotPending
	}
	now := time.Now().UTC()
	if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
		return ErrExpired
	}
	if a.BreakGlass {
		for _, d := range a.Decisions {
			if d.ApproverID == decidedBy {
				return ErrAlreadyDecided
			}
		}
		a.Decisions = append(a.Decisions, approvaldomain.ApprovalDecision{
			ApproverID: decidedBy, Action: "approve", Note: note, DecidedAt: now,
		})
		approveCount := 0
		for _, d := range a.Decisions {
			if d.Action == "approve" {
				approveCount++
			}
		}
		if approveCount < a.RequiredApprovals {
			if _, err := u.repo.Update(ctx, a); err != nil {
				return fmt.Errorf("update approval (partial): %w", err)
			}
			u.emitAudit(ctx, a.RequestID, auditdomain.EventApproved, decidedBy,
				fmt.Sprintf("Partial approval (%d/%d): %s", approveCount, a.RequiredApprovals, note),
				map[string]any{"decided_by": decidedBy, "note": note, "approvals": approveCount, "required": a.RequiredApprovals})
			return nil
		}
	} else {
		a.Decisions = []approvaldomain.ApprovalDecision{
			{ApproverID: decidedBy, Action: "approve", Note: note, DecidedAt: now},
		}
	}
	a.Status = approvaldomain.ApprovalStatusApproved
	a.DecidedBy = decidedBy
	a.DecisionNote = note
	a.DecidedAt = &now
	if _, err := u.repo.Update(ctx, a); err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	req, err := u.requestRepo.GetByID(ctx, a.RequestID)
	if err != nil {
		return fmt.Errorf("get request for approval: %w", err)
	}
	req.Status = requestdomain.StatusApproved
	req.DecidedAt = &now
	req.UpdatedAt = now
	if _, err := u.requestRepo.Update(ctx, req); err != nil {
		return fmt.Errorf("update request status: %w", err)
	}
	u.emitAudit(ctx, a.RequestID, auditdomain.EventApproved, decidedBy,
		"Approved: "+note, map[string]any{"decided_by": decidedBy, "note": note, "break_glass": a.BreakGlass})
	u.emitApprovalResolved(ctx, req, a)
	return nil
}

func (u *Usecases) rejectDirect(ctx context.Context, approvalID uuid.UUID, decidedBy, note string) error {
	a, err := u.repo.GetByID(ctx, approvalID)
	if err != nil {
		return fmt.Errorf("get approval: %w", err)
	}
	if a.Status != approvaldomain.ApprovalStatusPending {
		return ErrNotPending
	}
	now := time.Now().UTC()
	if !a.ExpiresAt.IsZero() && !now.Before(a.ExpiresAt) {
		return ErrExpired
	}
	if a.BreakGlass {
		for _, d := range a.Decisions {
			if d.ApproverID == decidedBy {
				return ErrAlreadyDecided
			}
		}
	}
	a.Decisions = append(a.Decisions, approvaldomain.ApprovalDecision{
		ApproverID: decidedBy, Action: "reject", Note: note, DecidedAt: now,
	})
	a.Status = approvaldomain.ApprovalStatusRejected
	a.DecidedBy = decidedBy
	a.DecisionNote = note
	a.DecidedAt = &now
	if _, err := u.repo.Update(ctx, a); err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	req, err := u.requestRepo.GetByID(ctx, a.RequestID)
	if err != nil {
		return fmt.Errorf("get request for rejection: %w", err)
	}
	req.Status = requestdomain.StatusRejected
	req.DecidedAt = &now
	req.UpdatedAt = now
	if _, err := u.requestRepo.Update(ctx, req); err != nil {
		return fmt.Errorf("update request status: %w", err)
	}
	u.emitAudit(ctx, a.RequestID, auditdomain.EventRejected, decidedBy,
		"Rejected: "+note, map[string]any{"decided_by": decidedBy, "note": note, "break_glass": a.BreakGlass})
	u.emitApprovalResolved(ctx, req, a)
	return nil
}

// emitAudit emite un evento al audit trail. Best-effort: loguea error, nunca falla la operación.
func (u *Usecases) emitAudit(ctx context.Context, requestID uuid.UUID, eventType, actorID, summary string, data map[string]any) {
	if u.audit == nil {
		return
	}
	if err := u.audit.AppendEvent(ctx, requestID, eventType, "human", actorID, summary, data); err != nil {
		slog.Error("audit event failed", "error", err, "request_id", requestID, "event", eventType)
	}
}

func (u *Usecases) emitApprovalResolved(ctx context.Context, req requestdomain.Request, approval approvaldomain.Approval) {
	if u.callbacks == nil {
		return
	}
	if err := u.callbacks.Publish(ctx, callbacks.ApprovalEvent{
		Event:        callbacks.EventApprovalResolved,
		ApprovalID:   approval.ID.String(),
		OrgID:        stringOrEmpty(req.OrgID, approval.OrgID),
		RequestID:    approval.RequestID.String(),
		Decision:     string(approval.Status),
		DecidedBy:    strings.TrimSpace(approval.DecidedBy),
		DecisionNote: strings.TrimSpace(approval.DecisionNote),
		DecidedAt:    timePtrRFC3339(approval.DecidedAt),
	}); err != nil {
		slog.Error("approval callback publish failed", "event", callbacks.EventApprovalResolved, "request_id", approval.RequestID, "error", err)
	}
}

func stringOrEmpty(values ...*string) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func timePtrRFC3339(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
