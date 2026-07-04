package dto

import "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"

type CreateCapabilityRequest struct {
	CapabilityKey    string `json:"capability_key" binding:"required"`
	Name             string `json:"name" binding:"required"`
	Description      string `json:"description"`
	RequiredAutonomy string `json:"required_autonomy" binding:"required"`
}

func (r CreateCapabilityRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		CapabilityKey:    r.CapabilityKey,
		Name:             r.Name,
		Description:      r.Description,
		RequiredAutonomy: r.RequiredAutonomy,
	}
}

type UpdateCapabilityRequest struct {
	Name             string `json:"name" binding:"required"`
	Description      string `json:"description"`
	RequiredAutonomy string `json:"required_autonomy" binding:"required"`
}

func (r UpdateCapabilityRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:             r.Name,
		Description:      r.Description,
		RequiredAutonomy: r.RequiredAutonomy,
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}
