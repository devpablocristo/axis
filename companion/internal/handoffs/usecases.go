package handoffs

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (uc *Usecases) List(ctx context.Context, tenantID, orgID, productSurface string, status Status, limit int) ([]Handoff, error) {
	if status != "" && !validStatus(status) {
		return nil, ErrValidation
	}
	return uc.repo.List(ctx, tenantID, orgID, productSurface, status, limit)
}

func (uc *Usecases) Get(ctx context.Context, tenantID, orgID, productSurface, handoffID string) (Handoff, error) {
	if strings.TrimSpace(handoffID) == "" {
		return Handoff{}, ErrValidation
	}
	return uc.repo.Get(ctx, tenantID, orgID, productSurface, handoffID)
}

func (uc *Usecases) Create(ctx context.Context, handoff Handoff) (Handoff, error) {
	handoff = normalize(handoff)
	if err := validate(handoff); err != nil {
		return Handoff{}, fmt.Errorf("%w: invalid employee handoff", err)
	}
	return uc.repo.Create(ctx, handoff)
}

func (uc *Usecases) Update(ctx context.Context, tenantID, orgID, productSurface, handoffID string, patch Handoff) (Handoff, error) {
	current, err := uc.repo.Get(ctx, tenantID, orgID, productSurface, handoffID)
	if err != nil {
		return Handoff{}, err
	}
	if patch.TaskID != nil {
		current.TaskID = patch.TaskID
	}
	if patch.FromEmployeeID != nil {
		current.FromEmployeeID = patch.FromEmployeeID
	}
	if patch.ToEmployeeID != uuid.Nil {
		current.ToEmployeeID = patch.ToEmployeeID
	}
	if strings.TrimSpace(patch.Reason) != "" {
		current.Reason = patch.Reason
	}
	if patch.Status != "" {
		current.Status = patch.Status
	}
	if strings.TrimSpace(patch.CreatedBy) != "" {
		current.CreatedBy = patch.CreatedBy
	}
	current.OrgID = orgID
	current.ProductSurface = productSurface
	if err := validate(normalize(current)); err != nil {
		return Handoff{}, fmt.Errorf("%w: invalid employee handoff", err)
	}
	return uc.repo.Update(ctx, current)
}

func validStatus(status Status) bool {
	switch status {
	case StatusPending, StatusAccepted, StatusRejected, StatusCancelled:
		return true
	default:
		return false
	}
}
