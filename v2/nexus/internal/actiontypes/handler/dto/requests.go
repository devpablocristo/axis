package dto

import "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"

type CreateActionTypeRequest struct {
	ActionTypeKey string `json:"action_type_key" binding:"required"`
	Name          string `json:"name" binding:"required"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	RiskClass     string `json:"risk_class"`
	Enabled       *bool  `json:"enabled"`
}

func (r CreateActionTypeRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		ActionTypeKey: r.ActionTypeKey,
		Name:          r.Name,
		Description:   r.Description,
		Category:      r.Category,
		RiskClass:     r.RiskClass,
		Enabled:       r.Enabled,
	}
}

type UpdateActionTypeRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Category    string `json:"category"`
	RiskClass   string `json:"risk_class"`
	Enabled     *bool  `json:"enabled"`
}

func (r UpdateActionTypeRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:        r.Name,
		Description: r.Description,
		Category:    r.Category,
		RiskClass:   r.RiskClass,
		Enabled:     r.Enabled,
	}
}
