package rbac

import (
	"context"
	"strings"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/nexus/internal/rbac/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type assignmentRepository interface {
	Create(ctx context.Context, a domain.Assignment) (domain.Assignment, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Assignment, error)
	List(ctx context.Context, filter ListFilter) ([]domain.Assignment, error)
	Check(ctx context.Context, orgID, userID string, role domain.Role) (bool, error)
	Archive(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type Usecases struct {
	repo assignmentRepository
}

func NewUsecases(repo assignmentRepository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) Grant(ctx context.Context, a domain.Assignment) (domain.Assignment, error) {
	a.OrgID = strings.TrimSpace(a.OrgID)
	a.UserID = strings.TrimSpace(a.UserID)
	a.GrantedBy = strings.TrimSpace(a.GrantedBy)
	if a.OrgID == "" {
		return domain.Assignment{}, domainerr.Validation("org_id is required")
	}
	if a.UserID == "" {
		return domain.Assignment{}, domainerr.Validation("user_id is required")
	}
	if !a.Role.Valid() {
		return domain.Assignment{}, domainerr.Validation("role must be one of: policy_admin, approver, auditor, delegate")
	}
	return u.repo.Create(ctx, a)
}

func (u *Usecases) GetByID(ctx context.Context, id uuid.UUID) (domain.Assignment, error) {
	return u.repo.GetByID(ctx, id)
}

func (u *Usecases) List(ctx context.Context, filter ListFilter) ([]domain.Assignment, error) {
	filter.OrgID = strings.TrimSpace(filter.OrgID)
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.Role = strings.TrimSpace(filter.Role)
	if filter.Role != "" && !domain.Role(filter.Role).Valid() {
		return nil, domainerr.Validation("invalid role filter")
	}
	return u.repo.List(ctx, filter)
}

func (u *Usecases) Check(ctx context.Context, orgID, userID string, role domain.Role) (bool, error) {
	orgID = strings.TrimSpace(orgID)
	userID = strings.TrimSpace(userID)
	if orgID == "" || userID == "" {
		return false, domainerr.Validation("org_id and user_id are required")
	}
	if !role.Valid() {
		return false, domainerr.Validation("invalid role")
	}
	return u.repo.Check(ctx, orgID, userID, role)
}

func (u *Usecases) Revoke(ctx context.Context, id uuid.UUID) error {
	return u.repo.Archive(ctx, id)
}

func (u *Usecases) Restore(ctx context.Context, id uuid.UUID) error {
	return u.repo.Restore(ctx, id)
}

func (u *Usecases) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return u.repo.DeleteByID(ctx, id)
}
