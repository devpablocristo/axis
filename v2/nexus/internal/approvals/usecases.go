package approvals

import (
	"context"
	"strings"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes"
	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	List(ctx context.Context, tenantID string, status domain.Status, limit int) ([]domain.Approval, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error)
	Decide(ctx context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID string, note string) (domain.Approval, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) List(ctx context.Context, tenantID string, statusRaw string, limit int) ([]domain.Approval, error) {
	status, err := domain.NormalizeListStatus(statusRaw)
	if err != nil {
		return nil, err
	}
	return u.repo.List(ctx, actiontypes.NormalizeTenantID(tenantID), status, normalizeLimit(limit))
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error) {
	return u.repo.Get(ctx, actiontypes.NormalizeTenantID(tenantID), id)
}

func (u *UseCases) Approve(ctx context.Context, tenantID string, id uuid.UUID, actorID string, input domain.DecisionInput) (domain.Approval, error) {
	return u.decide(ctx, tenantID, id, actorID, input, domain.StatusApproved)
}

func (u *UseCases) Reject(ctx context.Context, tenantID string, id uuid.UUID, actorID string, input domain.DecisionInput) (domain.Approval, error) {
	return u.decide(ctx, tenantID, id, actorID, input, domain.StatusRejected)
}

func (u *UseCases) decide(ctx context.Context, tenantID string, id uuid.UUID, actorID string, input domain.DecisionInput, status domain.Status) (domain.Approval, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return domain.Approval{}, domainerr.Validation("actor is required")
	}
	return u.repo.Decide(ctx, actiontypes.NormalizeTenantID(tenantID), id, status, actorID, domain.NormalizeDecisionNote(input))
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}
