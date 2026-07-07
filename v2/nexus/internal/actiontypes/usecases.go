package actiontypes

import (
	"context"
	"strings"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const DefaultTenantID = "default"

type RepositoryPort interface {
	Create(ctx context.Context, tenantID string, input domain.NormalizedCreateInput) (domain.ActionType, error)
	List(ctx context.Context, tenantID string) ([]domain.ActionType, error)
	Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.ActionType, error)
	GetByKey(ctx context.Context, tenantID string, key string) (domain.ActionType, error)
	Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.ActionType, error)
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) Create(ctx context.Context, tenantID string, input domain.CreateInput) (domain.ActionType, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.ActionType{}, err
	}
	return u.repo.Create(ctx, NormalizeTenantID(tenantID), normalized)
}

func (u *UseCases) List(ctx context.Context, tenantID string) ([]domain.ActionType, error) {
	return u.repo.List(ctx, NormalizeTenantID(tenantID))
}

func (u *UseCases) Get(ctx context.Context, tenantID string, id uuid.UUID) (domain.ActionType, error) {
	return u.repo.Get(ctx, NormalizeTenantID(tenantID), id)
}

func (u *UseCases) GetByKey(ctx context.Context, tenantID string, key string) (domain.ActionType, error) {
	normalizedTenantID := NormalizeTenantID(tenantID)
	normalizedKey := strings.TrimSpace(key)
	out, err := u.repo.GetByKey(ctx, normalizedTenantID, normalizedKey)
	if err == nil || !domainerr.IsNotFound(err) || normalizedTenantID == DefaultTenantID {
		return out, err
	}
	return u.repo.GetByKey(ctx, DefaultTenantID, normalizedKey)
}

func (u *UseCases) Update(ctx context.Context, tenantID string, id uuid.UUID, input domain.UpdateInput) (domain.ActionType, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.ActionType{}, err
	}
	return u.repo.Update(ctx, NormalizeTenantID(tenantID), id, normalized)
}

func NormalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return DefaultTenantID
	}
	return tenantID
}
