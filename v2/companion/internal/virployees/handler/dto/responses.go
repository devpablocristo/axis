package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type VirployeeResponse struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Role             string     `json:"role"`
	Description      string     `json:"description"`
	SupervisorUserID string     `json:"supervisor_user_id"`
	Autonomy         string     `json:"autonomy"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ArchivedAt       *time.Time `json:"archived_at"`
	TrashedAt        *time.Time `json:"trashed_at"`
	PurgeAfter       *time.Time `json:"purge_after"`
}

type ListVirployeesResponse struct {
	Data []VirployeeResponse `json:"data"`
}

func VirployeeFromDomain(v domain.Virployee) VirployeeResponse {
	return VirployeeResponse{
		ID:               v.ID.String(),
		Name:             v.Name,
		Role:             v.Role,
		Description:      v.Description,
		SupervisorUserID: v.SupervisorUserID.String(),
		Autonomy:         string(v.Autonomy),
		State:            string(v.State()),
		CreatedAt:        v.CreatedAt,
		UpdatedAt:        v.UpdatedAt,
		ArchivedAt:       v.ArchivedAt,
		TrashedAt:        v.TrashedAt,
		PurgeAfter:       v.PurgeAfter,
	}
}

func ListVirployeesFromDomain(items []domain.Virployee) ListVirployeesResponse {
	data := make([]VirployeeResponse, 0, len(items))
	for _, item := range items {
		data = append(data, VirployeeFromDomain(item))
	}
	return ListVirployeesResponse{Data: data}
}
