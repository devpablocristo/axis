package dto

import (
	"time"

	"github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
)

type ActionTypeResponse struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	ActionTypeKey string    `json:"action_type_key"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Category      string    `json:"category"`
	RiskClass     string    `json:"risk_class"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ListActionTypesResponse struct {
	Data []ActionTypeResponse `json:"data"`
}

func ActionTypeFromDomain(item domain.ActionType) ActionTypeResponse {
	return ActionTypeResponse{
		ID:            item.ID.String(),
		TenantID:      item.TenantID,
		ActionTypeKey: item.ActionTypeKey,
		Name:          item.Name,
		Description:   item.Description,
		Category:      item.Category,
		RiskClass:     string(item.RiskClass),
		Enabled:       item.Enabled,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

func ListActionTypesFromDomain(items []domain.ActionType) ListActionTypesResponse {
	data := make([]ActionTypeResponse, 0, len(items))
	for _, item := range items {
		data = append(data, ActionTypeFromDomain(item))
	}
	return ListActionTypesResponse{Data: data}
}
