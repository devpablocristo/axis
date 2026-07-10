package approvals

import (
	"context"
	"strings"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes"
	"github.com/devpablocristo/nexus-v2/internal/approvals/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/pagination"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	List(ctx context.Context, tenantID string, status domain.Status, limit int, after *domain.ListCursor) ([]domain.Approval, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.Approval, error)
	Decide(ctx context.Context, tenantID string, id uuid.UUID, status domain.Status, actorID string, note string) (domain.Approval, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

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
