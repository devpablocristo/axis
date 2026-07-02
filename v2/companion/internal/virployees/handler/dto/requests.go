package dto

import (
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type CreateVirployeeRequest struct {
	Name             string `json:"name" binding:"required"`
	Role             string `json:"role" binding:"required"`
	Description      string `json:"description"`
	SupervisorUserID string `json:"supervisor_user_id" binding:"required"`
	Autonomy         string `json:"autonomy"`
}

func (r CreateVirployeeRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		Name:             r.Name,
		Role:             r.Role,
		Description:      r.Description,
		SupervisorUserID: r.SupervisorUserID,
		Autonomy:         r.Autonomy,
	}
}

type UpdateVirployeeRequest struct {
	Name             string `json:"name" binding:"required"`
	Role             string `json:"role" binding:"required"`
	Description      string `json:"description"`
	SupervisorUserID string `json:"supervisor_user_id" binding:"required"`
	Autonomy         string `json:"autonomy"`
}

func (r UpdateVirployeeRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:             r.Name,
		Role:             r.Role,
		Description:      r.Description,
		SupervisorUserID: r.SupervisorUserID,
		Autonomy:         r.Autonomy,
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}
