package dto

import (
	"encoding/json"

	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

// AssistRequest is the "process and respond" body: the product's opaque input
// data and an optional idempotency key (also accepted via the Idempotency-Key
// header). No prompt is sent by the caller — the virployee's profile holds it.
type AssistRequest struct {
	InputJSON            json.RawMessage `json:"input_json"`
	IdempotencyKey       string          `json:"idempotency_key,omitempty"`
	AssistType           string          `json:"assist_type,omitempty"`
	ProductSurface       string          `json:"product_surface,omitempty"`
	SubjectID            string          `json:"subject_id,omitempty"`
	RepositoryGeneration string          `json:"repository_generation,omitempty"`
}

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

type SimulateApprovedExecutionRequest struct {
	ApprovalID string `json:"approval_id" binding:"required"`
}

type ExecuteApprovedActionRequest struct {
	ApprovalID string `json:"approval_id" binding:"required"`
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
