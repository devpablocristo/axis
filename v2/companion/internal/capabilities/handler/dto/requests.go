package dto

import (
	"encoding/json"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
)

type CreateCapabilityRequest struct {
	CapabilityKey              string `json:"capability_key"`
	Name                       string `json:"name" binding:"required"`
	Description                string `json:"description"`
	RequiredAutonomy           string `json:"required_autonomy" binding:"required"`
	RiskClass                  string `json:"risk_class"`
	SideEffectClass            string `json:"side_effect_class"`
	RequiresGovernanceApproval *bool  `json:"requires_governance_approval"`
	RequiresNexusApproval      *bool  `json:"requires_nexus_approval"`
	EvidenceRequired           bool   `json:"evidence_required"`
	RollbackCapabilityKey      string `json:"rollback_capability_key"`
}

func (r CreateCapabilityRequest) ToDomain() domain.CreateInput {
	return domain.CreateInput{
		CapabilityKey:    r.CapabilityKey,
		Name:             r.Name,
		Description:      r.Description,
		RequiredAutonomy: r.RequiredAutonomy,
		Governance:       r.governance(),
	}
}

func (r CreateCapabilityRequest) governance() domain.GovernanceInput {
	requiresApproval := r.RequiresGovernanceApproval
	if requiresApproval == nil {
		requiresApproval = r.RequiresNexusApproval
	}
	return domain.GovernanceInput{
		RiskClass:                  r.RiskClass,
		SideEffectClass:            r.SideEffectClass,
		RequiresGovernanceApproval: requiresApproval,
		EvidenceRequired:           r.EvidenceRequired,
		RollbackCapabilityKey:      r.RollbackCapabilityKey,
	}
}

type UpdateCapabilityRequest struct {
	Name                       string `json:"name" binding:"required"`
	Description                string `json:"description"`
	RequiredAutonomy           string `json:"required_autonomy" binding:"required"`
	RiskClass                  string `json:"risk_class"`
	SideEffectClass            string `json:"side_effect_class"`
	RequiresGovernanceApproval *bool  `json:"requires_governance_approval"`
	RequiresNexusApproval      *bool  `json:"requires_nexus_approval"`
	EvidenceRequired           bool   `json:"evidence_required"`
	RollbackCapabilityKey      string `json:"rollback_capability_key"`
}

func (r UpdateCapabilityRequest) ToDomain() domain.UpdateInput {
	requiresApproval := r.RequiresGovernanceApproval
	if requiresApproval == nil {
		requiresApproval = r.RequiresNexusApproval
	}
	return domain.UpdateInput{
		Name:             r.Name,
		Description:      r.Description,
		RequiredAutonomy: r.RequiredAutonomy,
		Governance: domain.GovernanceInput{
			RiskClass:                  r.RiskClass,
			SideEffectClass:            r.SideEffectClass,
			RequiresGovernanceApproval: requiresApproval,
			EvidenceRequired:           r.EvidenceRequired,
			RollbackCapabilityKey:      r.RollbackCapabilityKey,
		},
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}

type CapabilityManifestRequest struct {
	Version             string                     `json:"version"`
	ProductSurface      string                     `json:"product_surface"`
	InputSchema         json.RawMessage            `json:"input_schema"`
	OutputSchema        json.RawMessage            `json:"output_schema"`
	RequiredScopes      []string                   `json:"required_scopes"`
	Idempotency         domain.IdempotencyContract `json:"idempotency"`
	RollbackMode        string                     `json:"rollback_mode"`
	TimeoutMS           int                        `json:"timeout_ms"`
	Retry               domain.RetryContract       `json:"retry"`
	Postconditions      []string                   `json:"postconditions"`
	QuotaAreas          []string                   `json:"quota_areas"`
	SecretRefs          []string                   `json:"secret_refs"`
	AttestationRequired bool                       `json:"attestation_required"`
	CostClass           string                     `json:"cost_class"`
	ExecutorBindingID   string                     `json:"executor_binding_id,omitempty"`
	Operation           string                     `json:"operation,omitempty"`
}

func (r CapabilityManifestRequest) ToDomain() domain.ManifestInput {
	return domain.ManifestInput{
		Version: r.Version, ProductSurface: r.ProductSurface, InputSchema: r.InputSchema, OutputSchema: r.OutputSchema,
		RequiredScopes: r.RequiredScopes, Idempotency: r.Idempotency, RollbackMode: r.RollbackMode,
		TimeoutMS: r.TimeoutMS, Retry: r.Retry, Postconditions: r.Postconditions, QuotaAreas: r.QuotaAreas,
		SecretRefs: r.SecretRefs, AttestationRequired: r.AttestationRequired, CostClass: r.CostClass,
		ExecutorBindingID: r.ExecutorBindingID, Operation: r.Operation,
	}
}
