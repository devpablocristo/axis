package dto

import (
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type CreateVirployeeRequest struct {
	Name              string   `json:"name" binding:"required"`
	JobRoleID         string   `json:"job_role_id" binding:"required"`
	ProfileTemplateID string   `json:"profile_template_id"`
	CapabilityIDs     []string `json:"capability_ids"`
	Description       string   `json:"description"`
	SupervisorUserID  string   `json:"supervisor_user_id" binding:"required"`
	Autonomy          string   `json:"autonomy"`
}

func (r CreateVirployeeRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		Name:              r.Name,
		JobRoleID:         r.JobRoleID,
		ProfileTemplateID: r.ProfileTemplateID,
		CapabilityIDs:     r.CapabilityIDs,
		Description:       r.Description,
		SupervisorUserID:  r.SupervisorUserID,
		Autonomy:          r.Autonomy,
	}
}

type UpdateVirployeeRequest struct {
	Name              string   `json:"name" binding:"required"`
	JobRoleID         string   `json:"job_role_id" binding:"required"`
	ProfileTemplateID string   `json:"profile_template_id"`
	CapabilityIDs     []string `json:"capability_ids"`
	Description       string   `json:"description"`
	SupervisorUserID  string   `json:"supervisor_user_id" binding:"required"`
	Autonomy          string   `json:"autonomy"`
}

func (r UpdateVirployeeRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:              r.Name,
		JobRoleID:         r.JobRoleID,
		ProfileTemplateID: r.ProfileTemplateID,
		CapabilityIDs:     r.CapabilityIDs,
		Description:       r.Description,
		SupervisorUserID:  r.SupervisorUserID,
		Autonomy:          r.Autonomy,
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}

type DryRunVirployeeRequest struct {
	Input string `json:"input" binding:"required"`
}

type ExecutionGateVirployeeRequest struct {
	Input          string                 `json:"input" binding:"required"`
	ConfirmedDraft *ConfirmedDraftRequest `json:"confirmed_draft"`
}

type ConfirmedDraftRequest struct {
	Action string                       `json:"action"`
	Kind   string                       `json:"kind"`
	Fields []ConfirmedDraftFieldRequest `json:"fields"`
}

type ConfirmedDraftFieldRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (r ExecutionGateVirployeeRequest) ConfirmedDraftToDomain() *executiongate.ConfirmedDraft {
	if r.ConfirmedDraft == nil {
		return nil
	}
	fields := make([]executiongate.ConfirmedDraftField, 0, len(r.ConfirmedDraft.Fields))
	for _, field := range r.ConfirmedDraft.Fields {
		fields = append(fields, executiongate.ConfirmedDraftField{
			Key:   field.Key,
			Value: field.Value,
		})
	}
	return &executiongate.ConfirmedDraft{
		Action: r.ConfirmedDraft.Action,
		Kind:   r.ConfirmedDraft.Kind,
		Fields: fields,
	}
}
