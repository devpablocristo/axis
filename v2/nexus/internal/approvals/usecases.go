package approvals

import (
	"context"
	"log/slog"
	"strings"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes"
	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/pagination"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	List(ctx context.Context, tenantID string, status domain.Status, limit int, after *domain.ListCursor) ([]domain.Approval, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error)
	Decide(ctx context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID, actorRole, note string) (domain.Approval, error)
	Review(ctx context.Context, tenantID string, id uuid.UUID, actorID, note string) (domain.Approval, error)
}

type AuditEmitterPort interface {
	Append(ctx context.Context, tenantID string, in auditdomain.AppendInput) (auditdomain.AuditEvent, error)
}

type UseCases struct {
	repo  RepositoryPort
	audit AuditEmitterPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) SetAuditEmitter(emitter AuditEmitterPort) { u.audit = emitter }

func (u *UseCases) List(ctx context.Context, tenantID string, input domain.ListInput) (domain.ListPage, error) {
	status, err := domain.NormalizeListStatus(input.StatusRaw)
	if err != nil {
		return domain.ListPage{}, err
	}
	after, err := decodeListCursor(input.Cursor)
	if err != nil {
		return domain.ListPage{}, err
	}
	limit := normalizeLimit(input.Limit)
	items, err := u.repo.List(ctx, actiontypes.NormalizeTenantID(tenantID), status, limit+1, after)
	if err != nil {
		return domain.ListPage{}, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor, err = encodeListCursor(items[len(items)-1])
		if err != nil {
			return domain.ListPage{}, err
		}
	}
	return domain.ListPage{
		Items:      append([]domain.Approval(nil), items...),
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	return u.repo.Get(ctx, actiontypes.NormalizeTenantID(tenantID), id)
}

func (u *UseCases) Approve(ctx context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput) (domain.Approval, error) {
	return u.decide(ctx, tenantID, id, actor, input, domain.StatusApproved)
}

func (u *UseCases) Reject(ctx context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput) (domain.Approval, error) {
	return u.decide(ctx, tenantID, id, actor, input, domain.StatusRejected)
}

func (u *UseCases) decide(ctx context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput, status domain.Status) (domain.Approval, error) {
	tenantID = actiontypes.NormalizeTenantID(tenantID)
	approval, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return domain.Approval{}, err
	}
	actor, err = authorizeDecision(approval, actor)
	if err != nil {
		return domain.Approval{}, err
	}
	note := domain.NormalizeDecisionNote(input)
	if approval.ApprovalKind == "break_glass" && note == "" {
		return domain.Approval{}, domainerr.Validation("break-glass decisions require a justification")
	}
	updated, err := u.repo.Decide(ctx, tenantID, id, status, actor.ID, actor.Role, note)
	if err != nil {
		return domain.Approval{}, err
	}
	u.emitDecision(ctx, updated, actor, status)
	return updated, nil
}

func (u *UseCases) Review(ctx context.Context, tenantID string, id uuid.UUID, actor domain.DecisionActor, input domain.DecisionInput) (domain.Approval, error) {
	tenantID = actiontypes.NormalizeTenantID(tenantID)
	approval, err := u.repo.Get(ctx, tenantID, id)
	if err != nil {
		return domain.Approval{}, err
	}
	actor = normalizeActor(actor)
	if actor.ID == "" {
		return domain.Approval{}, domainerr.Validation("actor is required")
	}
	if actor.Role != "owner" && actor.Role != "admin" {
		return domain.Approval{}, domainerr.Forbidden("break-glass review requires an owner or admin")
	}
	if actor.ID == approval.RequesterID || strings.HasPrefix(strings.ToLower(actor.ID), "service:") {
		return domain.Approval{}, domainerr.Forbidden("requesters and service principals cannot review approvals")
	}
	note := domain.NormalizeDecisionNote(input)
	if note == "" {
		return domain.Approval{}, domainerr.Validation("post-action review requires a note")
	}
	updated, err := u.repo.Review(ctx, tenantID, id, actor.ID, note)
	if err != nil {
		return domain.Approval{}, err
	}
	u.emitReview(ctx, updated, actor)
	return updated, nil
}

func authorizeDecision(approval domain.Approval, actor domain.DecisionActor) (domain.DecisionActor, error) {
	actor = normalizeActor(actor)
	if actor.ID == "" {
		return domain.DecisionActor{}, domainerr.Validation("actor is required")
	}
	if actor.ID == approval.RequesterID || strings.HasPrefix(strings.ToLower(actor.ID), "service:") || actor.Role == "service" || actor.Role == "virployee" {
		return domain.DecisionActor{}, domainerr.Forbidden("requesters, virployees and service principals cannot decide approvals")
	}
	if actor.Role == "owner" || actor.Role == "admin" {
		return actor, nil
	}
	if (actor.Role == "member" || actor.Role == "supervisor") && actor.ID == strings.TrimSpace(approval.SupervisorUserID) {
		return actor, nil
	}
	return domain.DecisionActor{}, domainerr.Forbidden("approval decision requires the assigned supervisor or an owner/admin")
}

func normalizeActor(actor domain.DecisionActor) domain.DecisionActor {
	actor.ID = strings.TrimSpace(actor.ID)
	actor.Role = strings.ToLower(strings.TrimSpace(actor.Role))
	return actor
}

func (u *UseCases) emitDecision(ctx context.Context, approval domain.Approval, actor domain.DecisionActor, status domain.Status) {
	if u.audit == nil {
		return
	}
	decision := "approve"
	if status == domain.StatusRejected {
		decision = "reject"
	}
	_, err := u.audit.Append(ctx, approval.TenantID, auditdomain.AppendInput{
		VirployeeID: approval.RequesterID, SubjectType: "approval", SubjectID: approval.ID.String(),
		EventType: auditdomain.EventGovernanceDecided, ActorType: "human", ActorID: actor.ID,
		Summary: "governance approval decision recorded",
		Data:    map[string]any{"governance_check_id": approval.GovernanceCheckID.String(), "binding_hash": approval.BindingHash, "decision": decision, "status": string(approval.Status), "actor_role": actor.Role, "approval_kind": approval.ApprovalKind, "approval_count": approval.ApprovalCount, "quorum_required": approval.QuorumRequired},
	})
	if err != nil {
		slog.ErrorContext(ctx, "append approval decision audit event", "approval_id", approval.ID, "error", err)
	}
}

func (u *UseCases) emitReview(ctx context.Context, approval domain.Approval, actor domain.DecisionActor) {
	if u.audit == nil {
		return
	}
	_, err := u.audit.Append(ctx, approval.TenantID, auditdomain.AppendInput{
		VirployeeID: approval.RequesterID, SubjectType: "approval", SubjectID: approval.ID.String(),
		EventType: auditdomain.EventBreakGlassReviewed, ActorType: "human", ActorID: actor.ID,
		Summary: "break-glass approval reviewed",
		Data:    map[string]any{"governance_check_id": approval.GovernanceCheckID.String(), "binding_hash": approval.BindingHash, "actor_role": actor.Role},
	})
	if err != nil {
		slog.ErrorContext(ctx, "append break-glass review audit event", "approval_id", approval.ID, "error", err)
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func encodeListCursor(item domain.Approval) (string, error) {
	return pagination.EncodeTimeIDCursor(pagination.TimeIDCursor{
		CreatedAt: item.CreatedAt.UTC(),
		ID:        item.ID.String(),
	})
}

func decodeListCursor(raw string) (*domain.ListCursor, error) {
	cursor, ok, err := pagination.DecodeTimeIDCursor(raw)
	if err != nil {
		return nil, domainerr.Validation("invalid approval cursor")
	}
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(cursor.ID)
	if err != nil {
		return nil, domainerr.Validation("invalid approval cursor")
	}
	return &domain.ListCursor{
		CreatedAt: cursor.CreatedAt.UTC(),
		ID:        id,
	}, nil
}
