package virployees

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("virployee not found")
	ErrValidation = errors.New("virployee validation failed")
	ErrConflict   = errors.New("virployee conflict")
)

type VirployeeStatus string

const (
	VirployeeStatusDraft     VirployeeStatus = "draft"
	VirployeeStatusActive    VirployeeStatus = "active"
	VirployeeStatusDisabled  VirployeeStatus = "disabled"
	VirployeeStatusSuspended VirployeeStatus = "suspended"
	VirployeeStatusArchived  VirployeeStatus = "archived"
	VirployeeStatusTrashed   VirployeeStatus = "trashed"
	VirployeeStatusError     VirployeeStatus = "error"
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

type Virployee struct {
	VirployeeID      uuid.UUID       `json:"virployee_id"`
	TenantID         uuid.UUID       `json:"tenant_id"`
	OrgID            string          `json:"-"`
	ProductSurface   string          `json:"-"`
	Name             string          `json:"name"`
	SupervisorUserID uuid.UUID       `json:"supervisor_user_id"`
	Status           VirployeeStatus `json:"status"`
	JobRoleID        uuid.UUID       `json:"job_role_id"`
	ProfileID        uuid.UUID       `json:"profile_id"`
	Autonomy         AutonomyLevel   `json:"autonomy"`
	CapabilityIDs    []uuid.UUID     `json:"capability_ids"`
	MemoryID         *uuid.UUID      `json:"memory_id,omitempty"`

	createdBy string
	version   int
}

func normalizeVirployee(virployee Virployee) Virployee {
	virployee.Name = strings.TrimSpace(virployee.Name)
	virployee.OrgID = strings.TrimSpace(virployee.OrgID)
	virployee.ProductSurface = strings.TrimSpace(strings.ToLower(virployee.ProductSurface))
	if virployee.Status == "" {
		virployee.Status = VirployeeStatusDraft
	}
	if virployee.Autonomy == "" {
		virployee.Autonomy = AutonomyA1
	}
	virployee.CapabilityIDs = dedupeUUIDs(virployee.CapabilityIDs)
	return virployee
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
