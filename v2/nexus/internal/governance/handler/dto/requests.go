package dto

import "github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"

type CheckRequest struct {
	RequesterType    string         `json:"requester_type"`
	RequesterID      string         `json:"requester_id" binding:"required"`
	SupervisorUserID string         `json:"supervisor_user_id"`
	ActionType       string         `json:"action_type" binding:"required"`
	TargetSystem     string         `json:"target_system"`
	TargetResource   string         `json:"target_resource"`
	Params           map[string]any `json:"params"`
	Reason           string         `json:"reason"`
	Context          string         `json:"context"`
	BindingHash      string         `json:"binding_hash"`
}

func (r CheckRequest) ToDomain() domain.CheckInput {
	return domain.CheckInput{
		RequesterType:    r.RequesterType,
		RequesterID:      r.RequesterID,
		SupervisorUserID: r.SupervisorUserID,
		ActionType:       r.ActionType,
		TargetSystem:     r.TargetSystem,
		TargetResource:   r.TargetResource,
		Params:           r.Params,
		Reason:           r.Reason,
		Context:          r.Context,
		BindingHash:      r.BindingHash,
	}
}

type ExecutionResultRequest struct {
	BindingHash        string         `json:"binding_hash" binding:"required"`
	Status             string         `json:"status" binding:"required"`
	DurationMS         int64          `json:"duration_ms"`
	Result             map[string]any `json:"result"`
	AttestationVersion string         `json:"attestation_version" binding:"required"`
	ExecutorVersion    string         `json:"executor_version" binding:"required"`
	Attestation        string         `json:"attestation" binding:"required"`
}

func (r ExecutionResultRequest) ToDomain(idempotencyKey string) domain.ExecutionResultInput {
	return domain.ExecutionResultInput{
		IdempotencyKey:     idempotencyKey,
		BindingHash:        r.BindingHash,
		Status:             r.Status,
		DurationMS:         r.DurationMS,
		Result:             r.Result,
		AttestationVersion: r.AttestationVersion,
		ExecutorVersion:    r.ExecutorVersion,
		Attestation:        r.Attestation,
	}
}
