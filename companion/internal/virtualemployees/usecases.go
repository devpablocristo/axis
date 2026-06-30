package virtualemployees

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Repository interface {
	ListEmployees(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]VirtualEmployee, error)
	GetEmployee(ctx context.Context, tenantID, orgID, productSurface, employeeID string) (VirtualEmployee, error)
	CreateEmployee(ctx context.Context, employee VirtualEmployee, actorID string) (VirtualEmployee, error)
	UpdateEmployee(ctx context.Context, employee VirtualEmployee, actorID string) (VirtualEmployee, error)
	SetEmployeeStatus(ctx context.Context, tenantID, orgID, productSurface, employeeID string, status EmployeeStatus, actorID string) (VirtualEmployee, error)
	ValidateReferences(ctx context.Context, employee VirtualEmployee) error
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListEmployees(ctx context.Context, tenantID, orgID, productSurface string, lifecycle string) ([]VirtualEmployee, error) {
	if tenantID == "" || orgID == "" || productSurface == "" {
		return nil, fmt.Errorf("%w: tenant_id, org_id and product_surface are required", ErrValidation)
	}
	return u.repo.ListEmployees(ctx, tenantID, orgID, productSurface, normalizeLifecycle(lifecycle))
}

func (u *Usecases) GetEmployee(ctx context.Context, tenantID, orgID, productSurface, employeeID string) (VirtualEmployee, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || employeeID == "" {
		return VirtualEmployee{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and employee_id are required", ErrValidation)
	}
	return u.repo.GetEmployee(ctx, tenantID, orgID, productSurface, employeeID)
}

func (u *Usecases) CreateEmployee(ctx context.Context, employee VirtualEmployee, actorID string) (VirtualEmployee, error) {
	employee = normalizeEmployee(employee)
	if err := validateEmployee(employee, false); err != nil {
		return VirtualEmployee{}, err
	}
	if employee.Status == EmployeeStatusArchived || employee.Status == EmployeeStatusTrashed {
		return VirtualEmployee{}, fmt.Errorf("%w: create cannot set archived or trashed status", ErrValidation)
	}
	if err := u.repo.ValidateReferences(ctx, employee); err != nil {
		return VirtualEmployee{}, err
	}
	return u.repo.CreateEmployee(ctx, employee, actorID)
}

func (u *Usecases) UpdateEmployee(ctx context.Context, employee VirtualEmployee, actorID string) (VirtualEmployee, error) {
	employee = normalizeEmployee(employee)
	if err := validateEmployee(employee, true); err != nil {
		return VirtualEmployee{}, err
	}
	if employee.Status == EmployeeStatusArchived || employee.Status == EmployeeStatusTrashed {
		return VirtualEmployee{}, fmt.Errorf("%w: update cannot set archived or trashed status; use status endpoint", ErrValidation)
	}
	if err := u.repo.ValidateReferences(ctx, employee); err != nil {
		return VirtualEmployee{}, err
	}
	return u.repo.UpdateEmployee(ctx, employee, actorID)
}

func (u *Usecases) SetEmployeeStatus(ctx context.Context, tenantID, orgID, productSurface, employeeID string, status EmployeeStatus, actorID string) (VirtualEmployee, error) {
	if tenantID == "" || orgID == "" || productSurface == "" || employeeID == "" {
		return VirtualEmployee{}, fmt.Errorf("%w: tenant_id, org_id, product_surface and employee_id are required", ErrValidation)
	}
	if !validStatus(status) {
		return VirtualEmployee{}, fmt.Errorf("%w: invalid employee status", ErrValidation)
	}
	return u.repo.SetEmployeeStatus(ctx, tenantID, orgID, productSurface, employeeID, status, actorID)
}

func validateEmployee(employee VirtualEmployee, requireID bool) error {
	if requireID && employee.EmployeeID == uuid.Nil {
		return fmt.Errorf("%w: employee_id is required", ErrValidation)
	}
	if employee.TenantID == uuid.Nil || employee.OrgID == "" || employee.ProductSurface == "" {
		return fmt.Errorf("%w: tenant_id, org_id and product_surface are required", ErrValidation)
	}
	if employee.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if employee.SupervisorUserID == uuid.Nil {
		return fmt.Errorf("%w: supervisor_user_id is required", ErrValidation)
	}
	if employee.JobRoleID == uuid.Nil || employee.ProfileID == uuid.Nil {
		return fmt.Errorf("%w: job_role_id and profile_id are required", ErrValidation)
	}
	if !validStatus(employee.Status) {
		return fmt.Errorf("%w: invalid employee status", ErrValidation)
	}
	if !validAutonomy(employee.Autonomy) {
		return fmt.Errorf("%w: invalid autonomy", ErrValidation)
	}
	return nil
}

func validStatus(status EmployeeStatus) bool {
	switch status {
	case EmployeeStatusDraft, EmployeeStatusActive, EmployeeStatusDisabled, EmployeeStatusSuspended, EmployeeStatusArchived, EmployeeStatusTrashed, EmployeeStatusError:
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
