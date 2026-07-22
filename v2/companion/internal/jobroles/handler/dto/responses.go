package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
)

type JobRoleResponse struct {
	ID               string                     `json:"id"`
	OrgID            string                     `json:"org_id"`
	Name             string                     `json:"name"`
	Slug             string                     `json:"slug"`
	Mission          string                     `json:"mission"`
	Responsibilities []ResponsibilityResponse   `json:"responsibilities"`
	SuccessCriteria  []SuccessCriterionResponse `json:"success_criteria"`
	State            string                     `json:"state"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at"`
	ArchivedAt       *time.Time                 `json:"archived_at"`
	TrashedAt        *time.Time                 `json:"trashed_at"`
	PurgeAfter       *time.Time                 `json:"purge_after"`
}

type ResponsibilityResponse struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expected_outcome"`
	Priority        int    `json:"priority"`
}

type SuccessCriterionResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TargetValue string `json:"target_value"`
	Priority    int    `json:"priority"`
}

type ListJobRolesResponse struct {
	Data []JobRoleResponse `json:"data"`
}

func JobRoleFromDomain(role domain.JobRole) JobRoleResponse {
	responsibilities := make([]ResponsibilityResponse, 0, len(role.Responsibilities))
	for _, item := range role.Responsibilities {
		responsibilities = append(responsibilities, ResponsibilityResponse{Title: item.Title, Description: item.Description, ExpectedOutcome: item.ExpectedOutcome, Priority: item.Priority})
	}
	successCriteria := make([]SuccessCriterionResponse, 0, len(role.SuccessCriteria))
	for _, item := range role.SuccessCriteria {
		successCriteria = append(successCriteria, SuccessCriterionResponse{Title: item.Title, Description: item.Description, TargetValue: item.TargetValue, Priority: item.Priority})
	}
	return JobRoleResponse{
		ID:               role.ID.String(),
		OrgID:            role.OrgID,
		Name:             role.Name,
		Slug:             role.Slug,
		Mission:          role.Mission,
		Responsibilities: responsibilities,
		SuccessCriteria:  successCriteria,
		State:            string(role.State()),
		CreatedAt:        role.CreatedAt,
		UpdatedAt:        role.UpdatedAt,
		ArchivedAt:       role.ArchivedAt,
		TrashedAt:        role.TrashedAt,
		PurgeAfter:       role.PurgeAfter,
	}
}

func ListJobRolesFromDomain(items []domain.JobRole) ListJobRolesResponse {
	data := make([]JobRoleResponse, 0, len(items))
	for _, item := range items {
		data = append(data, JobRoleFromDomain(item))
	}
	return ListJobRolesResponse{Data: data}
}
