package actiontypes

import (
	"context"
	"fmt"

	domain "github.com/devpablocristo/nexus/internal/actiontypes/usecases/domain"
	"github.com/google/uuid"
)

type actionTypeRepository interface {
	Create(ctx context.Context, at domain.ActionType) (domain.ActionType, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.ActionType, error)
	GetByName(ctx context.Context, name string) (domain.ActionType, error)
	GetByNameForOrg(ctx context.Context, name string, orgID *string) (domain.ActionType, error)
	List(ctx context.Context) ([]domain.ActionType, error)
	ListForOrg(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.ActionType, error)
	Update(ctx context.Context, at domain.ActionType) (domain.ActionType, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type Usecases struct {
	repo actionTypeRepository
}

func NewUsecases(repo actionTypeRepository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) Create(ctx context.Context, at domain.ActionType) (domain.ActionType, error) {
	if at.Name == "" {
		return domain.ActionType{}, fmt.Errorf("name is required")
	}
	if at.RiskClass == "" {
		at.RiskClass = domain.RiskClassLow
	}
	if at.Schema == nil {
		at.Schema = make(map[string]any)
	}
	at.Enabled = true
	return u.repo.Create(ctx, at)
}

func (u *Usecases) GetByID(ctx context.Context, id uuid.UUID) (domain.ActionType, error) {
	return u.repo.GetByID(ctx, id)
}

func (u *Usecases) GetByName(ctx context.Context, name string) (domain.ActionType, error) {
	return u.repo.GetByName(ctx, name)
}

func (u *Usecases) GetByNameForOrg(ctx context.Context, name string, orgID *string) (domain.ActionType, error) {
	return u.repo.GetByNameForOrg(ctx, name, orgID)
}

func (u *Usecases) List(ctx context.Context) ([]domain.ActionType, error) {
	return u.repo.List(ctx)
}

func (u *Usecases) ListForOrg(ctx context.Context, orgID *string, includeGlobal bool) ([]domain.ActionType, error) {
	return u.repo.ListForOrg(ctx, orgID, includeGlobal)
}

func (u *Usecases) Update(ctx context.Context, at domain.ActionType) (domain.ActionType, error) {
	return u.repo.Update(ctx, at)
}

func (u *Usecases) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return u.repo.DeleteByID(ctx, id)
}
