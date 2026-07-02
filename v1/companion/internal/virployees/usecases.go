package virployees

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Repository interface {
	ListVirployees(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]Virployee, error)
	GetVirployee(ctx context.Context, tenantID, orgID, productSurface, virployeeID string) (Virployee, error)
	CreateVirployee(ctx context.Context, virployee Virployee, actorID string) (Virployee, error)
	UpdateVirployee(ctx context.Context, virployee Virployee, actorID string) (Virployee, error)
	SetVirployeeStatus(ctx context.Context, tenantID, orgID, productSurface, virployeeID string, status VirployeeStatus, actorID string) (Virployee, error)
	ValidateReferences(ctx context.Context, virployee Virployee) error
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListVirployees(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]Virployee, error) {
	if tenantID == "" || orgID == "" || productSurface == "" {
		return nil, fmt.Errorf("%w: tenant_id, org_id and product_surface are required", ErrValidation)
	}
	return u.repo.ListVirployees(ctx, tenantID, orgID, productSurface, normalizeLifecycle(lifecycle))
}

func (u *Usecases) GetVirployee(ctx context.Context, tenantID, orgID, productSurface, virployeeID string) (Virployee, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || virployeeID == "" {
		return Virployee{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and virployee_id are required", ErrValidation)
	}
	return u.repo.GetVirployee(ctx, tenantID, orgID, productSurface, virployeeID)
}

func (u *Usecases) CreateVirployee(ctx context.Context, virployee Virployee, actorID string) (Virployee, error) {
	virployee = normalizeVirployee(virployee)
	if err := validateVirployee(virployee, false); err != nil {
		return Virployee{}, err
	}
	if virployee.Status == VirployeeStatusArchived || virployee.Status == VirployeeStatusTrashed {
		return Virployee{}, fmt.Errorf("%w: create cannot set archived or trashed status", ErrValidation)
	}
	if err := u.repo.ValidateReferences(ctx, virployee); err != nil {
		return Virployee{}, err
	}
	return u.repo.CreateVirployee(ctx, virployee, actorID)
}

func (u *Usecases) UpdateVirployee(ctx context.Context, virployee Virployee, actorID string) (Virployee, error) {
	virployee = normalizeVirployee(virployee)
	if err := validateVirployee(virployee, true); err != nil {
		return Virployee{}, err
	}
	if virployee.Status == VirployeeStatusArchived || virployee.Status == VirployeeStatusTrashed {
		return Virployee{}, fmt.Errorf("%w: update cannot set archived or trashed status; use status endpoint", ErrValidation)
	}
	if err := u.repo.ValidateReferences(ctx, virployee); err != nil {
		return Virployee{}, err
	}
	return u.repo.UpdateVirployee(ctx, virployee, actorID)
}

func (u *Usecases) SetVirployeeStatus(ctx context.Context, tenantID, orgID, productSurface, virployeeID string, status VirployeeStatus, actorID string) (Virployee, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || virployeeID == "" {
		return Virployee{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and virployee_id are required", ErrValidation)
	}
	if !validStatus(status) {
		return Virployee{}, fmt.Errorf("%w: invalid virployee status", ErrValidation)
	}
	return u.repo.SetVirployeeStatus(ctx, tenantID, orgID, productSurface, virployeeID, status, actorID)
}

func validateVirployee(virployee Virployee, requireID bool) error {
	if requireID && virployee.VirployeeID == uuid.Nil {
		return fmt.Errorf("%w: virployee_id is required", ErrValidation)
	}
	if virployee.TenantID == uuid.Nil || virployee.OrgID == "" || virployee.ProductSurface == "" {
		return fmt.Errorf("%w: tenant_id, org_id and product_surface are required", ErrValidation)
	}
	if virployee.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if virployee.SupervisorUserID == "" {
		return fmt.Errorf("%w: supervisor_user_id is required", ErrValidation)
	}
	if virployee.JobRoleID == uuid.Nil || virployee.ProfileID == uuid.Nil {
		return fmt.Errorf("%w: job_role_id and profile_id are required", ErrValidation)
	}
	if !validStatus(virployee.Status) {
		return fmt.Errorf("%w: invalid virployee status", ErrValidation)
	}
	if !validAutonomy(virployee.Autonomy) {
		return fmt.Errorf("%w: invalid autonomy", ErrValidation)
	}
	return nil
}

func validStatus(status VirployeeStatus) bool {
	switch status {
	case VirployeeStatusDraft, VirployeeStatusActive, VirployeeStatusDisabled, VirployeeStatusSuspended, VirployeeStatusArchived, VirployeeStatusTrashed, VirployeeStatusError:
		return true
	default:
		return false
	}
}

func validAutonomy(autonomy AutonomyLevel) bool {
	switch autonomy {
	case AutonomyA0, AutonomyA1, AutonomyA2, AutonomyA3, AutonomyA4, AutonomyA5:
		return true
	default:
		return false
	}
}

func normalizeLifecycle(lifecycle string) string {
	switch lifecycle {
	case "archived", "trashed", "all":
		return lifecycle
	default:
		return "active"
	}
}
