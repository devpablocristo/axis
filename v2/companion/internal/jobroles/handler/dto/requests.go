package dto

import "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"

type CreateJobRoleRequest struct {
	Name    string `json:"name" binding:"required"`
	Slug    string `json:"slug"`
	Mission string `json:"mission"`
}

func (r CreateJobRoleRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		Name:    r.Name,
		Slug:    r.Slug,
		Mission: r.Mission,
	}
}

type UpdateJobRoleRequest struct {
	Name    string `json:"name" binding:"required"`
	Slug    string `json:"slug"`
	Mission string `json:"mission"`
}

func (r UpdateJobRoleRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:    r.Name,
		Slug:    r.Slug,
		Mission: r.Mission,
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}
