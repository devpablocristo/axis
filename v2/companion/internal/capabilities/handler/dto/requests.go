package dto

import "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"

type CreateCapabilityRequest struct {
	CapabilityKey         string `json:"capability_key" binding:"required"`
	Name                  string `json:"name" binding:"required"`
	Description           string `json:"description"`
	RequiredAutonomy      string `json:"required_autonomy" binding:"required"`
	RiskClass             string `json:"risk_class"`
	SideEffectClass       string `json:"side_effect_class"`
	RequiresNexusApproval *bool  `json:"requires_nexus_approval"`
	EvidenceRequired      bool   `json:"evidence_required"`
	RollbackCapabilityKey string `json:"rollback_capability_key"`
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
	return domain.GovernanceInput{
		RiskClass:             r.RiskClass,
		SideEffectClass:       r.SideEffectClass,
		RequiresNexusApproval: r.RequiresNexusApproval,
		EvidenceRequired:      r.EvidenceRequired,
		RollbackCapabilityKey: r.RollbackCapabilityKey,
	}
}

type UpdateCapabilityRequest struct {
	Name                  string `json:"name" binding:"required"`
	Description           string `json:"description"`
	RequiredAutonomy      string `json:"required_autonomy" binding:"required"`
	RiskClass             string `json:"risk_class"`
	SideEffectClass       string `json:"side_effect_class"`
	RequiresNexusApproval *bool  `json:"requires_nexus_approval"`
	EvidenceRequired      bool   `json:"evidence_required"`
	RollbackCapabilityKey string `json:"rollback_capability_key"`
}

func (r UpdateCapabilityRequest) ToDomain() domain.UpdateInput {
	return domain.UpdateInput{
		Name:             r.Name,
		Description:      r.Description,
		RequiredAutonomy: r.RequiredAutonomy,
		Governance: domain.GovernanceInput{
			RiskClass:             r.RiskClass,
			SideEffectClass:       r.SideEffectClass,
			RequiresNexusApproval: r.RequiresNexusApproval,
			EvidenceRequired:      r.EvidenceRequired,
			RollbackCapabilityKey: r.RollbackCapabilityKey,
		},
	}
}

type LifecycleRequest struct {
	Reason string `json:"reason"`
}
