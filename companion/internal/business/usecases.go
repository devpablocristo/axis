package business

import (
	"context"
	"fmt"
	"strings"
)

type Repository interface {
	GetActive(ctx context.Context, orgID, productSurface string) (Model, error)
	Save(ctx context.Context, model Model) (Model, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) Get(ctx context.Context, orgID, productSurface string) (Model, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(productSurface)
	if productSurface == "" {
		productSurface = "companion"
	}
	if orgID == "" {
		return Model{}, fmt.Errorf("org_id is required")
	}
	return u.repo.GetActive(ctx, orgID, productSurface)
}

func (u *Usecases) Save(ctx context.Context, model Model) (Model, error) {
	model.OrgID = strings.TrimSpace(model.OrgID)
	model.ProductSurface = strings.TrimSpace(model.ProductSurface)
	if model.ProductSurface == "" {
		model.ProductSurface = "companion"
	}
	if model.OrgID == "" {
		return Model{}, fmt.Errorf("org_id is required")
	}
	model.Status = "active"
	return u.repo.Save(ctx, model)
}
