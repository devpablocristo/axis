package actiontypes

import (
	"context"
	"strings"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const DefaultOrgID = "default"

type RepositoryPort interface {
	Create(ctx context.Context, orgID string, input domain.NormalizedCreateInput) (domain.ActionType, error)
	List(ctx context.Context, orgID string) ([]domain.ActionType, error)
	Get(ctx context.Context, orgID string, id uuid.UUID) (domain.ActionType, error)
	GetByKey(ctx context.Context, orgID string, key string) (domain.ActionType, error)
	Update(ctx context.Context, orgID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ActionType, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) Create(ctx context.Context, orgID string, input domain.CreateInput) (domain.ActionType, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.ActionType{}, err
	}
	return u.repo.Create(ctx, NormalizeOrgID(orgID), normalized)
}

func (u *UseCases) List(ctx context.Context, orgID string) ([]domain.ActionType, error) {
	return u.repo.List(ctx, NormalizeOrgID(orgID))
}

func (u *UseCases) Get(ctx context.Context, orgID string, id uuid.UUID) (domain.ActionType, error) {
	return u.repo.Get(ctx, NormalizeOrgID(orgID), id)
}

func (u *UseCases) GetByKey(ctx context.Context, orgID string, key string) (domain.ActionType, error) {
	normalizedOrgID := NormalizeOrgID(orgID)
	normalizedKey := strings.TrimSpace(key)
	out, err := u.repo.GetByKey(ctx, normalizedOrgID, normalizedKey)
	if err == nil || !domainerr.IsNotFound(err) || normalizedOrgID == DefaultOrgID {
		return out, err
	}
	return u.repo.GetByKey(ctx, DefaultOrgID, normalizedKey)
}

func (u *UseCases) Update(ctx context.Context, orgID string, id uuid.UUID, input domain.UpdateInput) (domain.ActionType, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.ActionType{}, err
	}
	return u.repo.Update(ctx, NormalizeOrgID(orgID), id, normalized)
}

func NormalizeOrgID(orgID string) string {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return DefaultOrgID
	}
	return orgID
}
