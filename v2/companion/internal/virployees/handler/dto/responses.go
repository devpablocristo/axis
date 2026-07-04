package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type VirployeeResponse struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	JobRoleID        string     `json:"job_role_id"`
	CapabilityIDs    []string   `json:"capability_ids"`
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

type AutonomyLevelResponse struct {
	Level                    string   `json:"level"`
	Name                     string   `json:"name"`
	Description              string   `json:"description"`
	AllowsRequiredAutonomies []string `json:"allows_required_autonomies"`
}

type ListAutonomyLevelsResponse struct {
	Data []AutonomyLevelResponse `json:"data"`
}

func VirployeeFromDomain(v domain.Virployee) VirployeeResponse {
	return VirployeeResponse{
		ID:               v.ID.String(),
		Name:             v.Name,
		JobRoleID:        v.JobRoleID.String(),
		CapabilityIDs:    uuidStrings(v.CapabilityIDs),
		Description:      v.Description,
		SupervisorUserID: v.SupervisorUserID,
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

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func ListAutonomyLevelsFromDomain(definitions []domain.AutonomyDefinition) ListAutonomyLevelsResponse {
	data := make([]AutonomyLevelResponse, 0, len(definitions))
	for _, definition := range definitions {
		data = append(data, AutonomyLevelResponse{
			Level:                    string(definition.Level),
			Name:                     definition.Name,
			Description:              definition.Description,
			AllowsRequiredAutonomies: allowedRequiredAutonomies(definition.Level, definitions),
		})
	}
	return ListAutonomyLevelsResponse{Data: data}
}

func allowedRequiredAutonomies(
	level domain.AutonomyLevel,
	definitions []domain.AutonomyDefinition,
) []string {
	out := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if level.Allows(definition.Level) {
			out = append(out, string(definition.Level))
		}
	}
	return out
}
