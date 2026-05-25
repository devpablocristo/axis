package business

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Repository interface {
	GetActive(ctx context.Context, orgID, productSurface string) (Model, error)
	Save(ctx context.Context, model Model) (Model, error)
}

type Usecases struct {
	repo   Repository
	memory BusinessMemoryProjector
}

type BusinessMemoryProjector interface {
	ProjectBusinessModel(ctx context.Context, model Model) error
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) WithMemoryProjector(projector BusinessMemoryProjector) *Usecases {
	u.memory = projector
	return u
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
	saved, err := u.repo.Save(ctx, model)
	if err != nil {
		return Model{}, err
	}
	if u.memory != nil {
		if err := u.memory.ProjectBusinessModel(ctx, saved); err != nil {
			return Model{}, fmt.Errorf("project business model to memory: %w", err)
		}
	}
	return saved, nil
}

func BusinessModelMemoryPayload(model Model) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"model_id":         model.ID,
		"version":          model.Version,
		"organization":     model.Organization,
		"areas":            model.Areas,
		"roles":            model.Roles,
		"workflows":        model.Workflows,
		"rules":            model.Rules,
		"vocabulary":       model.Vocabulary,
		"slas":             model.SLAs,
		"relationships":    model.Relationships,
		"projected_source": "business_model",
	})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
