package dto

import "github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"

type CheckRequest struct {
	RequesterType        string `json:"requester_type"`
	RequesterID          string `json:"requester_id" binding:"required"`
	ProductSurface       string `json:"product_surface"`
	SupervisorUserID     string `json:"supervisor_user_id"`
	ActionType           string `json:"action_type" binding:"required"`
	TargetSystem         string `json:"target_system"`
	TargetResource       string `json:"target_resource"`
	ResourceType         string `json:"resource_type"`
	Reason               string `json:"reason"`
	BindingHash          string `json:"binding_hash"`
	AuthorityBindingHash string `json:"authority_binding_hash"`
	ScopeRevision        int64  `json:"scope_revision"`
	PolicyRevisionHash   string `json:"policy_revision_hash"`
	DelegationRequired   bool   `json:"delegation_required"`
	DelegationID         string `json:"delegation_id"`
	DelegationRevision   int64  `json:"delegation_revision"`
}

func (r CheckRequest) ToDomain(membershipRole string) domain.CheckInput {
	return domain.CheckInput{
		RequesterType:        r.RequesterType,
		RequesterID:          r.RequesterID,
		ProductSurface:       r.ProductSurface,
		SupervisorUserID:     r.SupervisorUserID,
		ActionType:           r.ActionType,
		TargetSystem:         r.TargetSystem,
		TargetResource:       r.TargetResource,
		ResourceType:         r.ResourceType,
		MembershipRole:       membershipRole,
		Reason:               r.Reason,
		BindingHash:          r.BindingHash,
		AuthorityBindingHash: r.AuthorityBindingHash,
		ScopeRevision:        r.ScopeRevision,
		PolicyRevisionHash:   r.PolicyRevisionHash,
		DelegationRequired:   r.DelegationRequired,
		DelegationID:         r.DelegationID,
		DelegationRevision:   r.DelegationRevision,
	}
}

type RevalidationRequest struct {
	BindingHash          string `json:"binding_hash" binding:"required"`
	PolicySnapshotHash   string `json:"policy_snapshot_hash"`
	AuthorityBindingHash string `json:"authority_binding_hash"`
	ScopeRevision        int64  `json:"scope_revision"`
	PolicyRevisionHash   string `json:"policy_revision_hash"`
	DelegationID         string `json:"delegation_id"`
	DelegationRevision   int64  `json:"delegation_revision"`
}

func (r RevalidationRequest) ToDomain() domain.RevalidationInput {
	return domain.RevalidationInput{BindingHash: r.BindingHash, PolicySnapshotHash: r.PolicySnapshotHash, AuthorityBindingHash: r.AuthorityBindingHash, ScopeRevision: r.ScopeRevision,
		PolicyRevisionHash: r.PolicyRevisionHash, DelegationID: r.DelegationID, DelegationRevision: r.DelegationRevision}
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
