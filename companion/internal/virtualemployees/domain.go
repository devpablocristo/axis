package virtualemployees

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("virtual employee not found")
	ErrValidation = errors.New("virtual employee validation failed")
	ErrConflict   = errors.New("virtual employee conflict")
)

type EmployeeStatus string

const (
	EmployeeStatusDraft     EmployeeStatus = "draft"
	EmployeeStatusActive    EmployeeStatus = "active"
	EmployeeStatusDisabled  EmployeeStatus = "disabled"
	EmployeeStatusSuspended EmployeeStatus = "suspended"
	EmployeeStatusArchived  EmployeeStatus = "archived"
	EmployeeStatusTrashed   EmployeeStatus = "trashed"
	EmployeeStatusError     EmployeeStatus = "error"
)

type AutonomyLevel string

const (
	AutonomyA0 AutonomyLevel = "A0"
	AutonomyA1 AutonomyLevel = "A1"
	AutonomyA2 AutonomyLevel = "A2"
	AutonomyA3 AutonomyLevel = "A3"
	AutonomyA4 AutonomyLevel = "A4"
	AutonomyA5 AutonomyLevel = "A5"
)

type VirtualEmployee struct {
	EmployeeID       uuid.UUID      `json:"employee_id"`
	TenantID         uuid.UUID      `json:"tenant_id"`
	OrgID            string         `json:"-"`
	ProductSurface   string         `json:"-"`
	Name             string         `json:"name"`
	SupervisorUserID uuid.UUID      `json:"supervisor_user_id"`
	Status           EmployeeStatus `json:"status"`
	JobRoleID        uuid.UUID      `json:"job_role_id"`
	ProfileID        uuid.UUID      `json:"profile_id"`
	Autonomy         AutonomyLevel  `json:"autonomy"`
	CapabilityIDs    []uuid.UUID    `json:"capability_ids"`
	MemoryID         *uuid.UUID     `json:"memory_id,omitempty"`

	createdBy string
	version   int
}

func normalizeEmployee(employee VirtualEmployee) VirtualEmployee {
	employee.Name = strings.TrimSpace(employee.Name)
	employee.OrgID = strings.TrimSpace(employee.OrgID)
	employee.ProductSurface = strings.TrimSpace(strings.ToLower(employee.ProductSurface))
	if employee.Status == "" {
		employee.Status = EmployeeStatusDraft
	}
	if employee.Autonomy == "" {
		employee.Autonomy = AutonomyA1
	}
	employee.CapabilityIDs = dedupeUUIDs(employee.CapabilityIDs)
	return employee
}

func dedupeUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	out := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
