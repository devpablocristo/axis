package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
)

type JobRoleResponse struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Name       string     `json:"name"`
	Slug       string     `json:"slug"`
	Mission    string     `json:"mission"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
	TrashedAt  *time.Time `json:"trashed_at"`
	PurgeAfter *time.Time `json:"purge_after"`
}

type ListJobRolesResponse struct {
	Data []JobRoleResponse `json:"data"`
}

func JobRoleFromDomain(role domain.JobRole) JobRoleResponse {
	return JobRoleResponse{
		ID:         role.ID.String(),
		TenantID:   role.TenantID,
		Name:       role.Name,
		Slug:       role.Slug,
		Mission:    role.Mission,
		State:      string(role.State()),
		CreatedAt:  role.CreatedAt,
		UpdatedAt:  role.UpdatedAt,
		ArchivedAt: role.ArchivedAt,
		TrashedAt:  role.TrashedAt,
		PurgeAfter: role.PurgeAfter,
	}
}

func ListJobRolesFromDomain(items []domain.JobRole) ListJobRolesResponse {
	data := make([]JobRoleResponse, 0, len(items))
	for _, item := range items {
		data = append(data, JobRoleFromDomain(item))
	}
	return ListJobRolesResponse{Data: data}
}
