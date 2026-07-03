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

type AutonomyActionClassResponse struct {
	Class            string `json:"class"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	RequiresApproval bool   `json:"requires_approval"`
}

type AutonomyLevelResponse struct {
	Level                string                        `json:"level"`
	Name                 string                        `json:"name"`
	Description          string                        `json:"description"`
	AllowedActionClasses []AutonomyActionClassResponse `json:"allowed_action_classes"`
}

type ListAutonomyLevelsResponse struct {
	Data []AutonomyLevelResponse `json:"data"`
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

func ListAutonomyLevelsFromDomain(definitions []domain.AutonomyDefinition) ListAutonomyLevelsResponse {
	data := make([]AutonomyLevelResponse, 0, len(definitions))
	actionClasses := domain.ActionClassDefinitions()
	for _, definition := range definitions {
		data = append(data, AutonomyLevelResponse{
			Level:                string(definition.Level),
			Name:                 definition.Name,
			Description:          definition.Description,
			AllowedActionClasses: allowedActionClasses(definition.Level, actionClasses),
		})
	}
	return ListAutonomyLevelsResponse{Data: data}
}

func allowedActionClasses(
	level domain.AutonomyLevel,
	definitions []domain.ActionClassDefinition,
) []AutonomyActionClassResponse {
	out := make([]AutonomyActionClassResponse, 0, len(definitions))
	for _, definition := range definitions {
		decision := domain.EvaluateAutonomy(level, definition.Class)
		if !decision.Allowed {
			continue
		}
		out = append(out, AutonomyActionClassResponse{
			Class:            string(definition.Class),
			Name:             definition.Name,
			Description:      definition.Description,
			RequiresApproval: decision.RequiresApproval,
		})
	}
	return out
}
