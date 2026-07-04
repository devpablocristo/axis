package dto

import "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"

type CreateProfileTemplateRequest struct {
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt" binding:"required"`
	MaxAutonomy  string `json:"max_autonomy" binding:"required"`
}

func (r CreateProfileTemplateRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		Name:         r.Name,
		Description:  r.Description,
		SystemPrompt: r.SystemPrompt,
		MaxAutonomy:  r.MaxAutonomy,
	}
}

type UpdateProfileTemplateRequest struct {
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt" binding:"required"`
	MaxAutonomy  string `json:"max_autonomy" binding:"required"`
}

func (r UpdateProfileTemplateRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:         r.Name,
		Description:  r.Description,
		SystemPrompt: r.SystemPrompt,
		MaxAutonomy:  r.MaxAutonomy,
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}
