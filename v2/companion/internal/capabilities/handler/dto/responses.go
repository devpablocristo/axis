package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
)

type CapabilityResponse struct {
	ID                    string                   `json:"id"`
	TenantID              string                   `json:"tenant_id"`
	CapabilityKey         string                   `json:"capability_key"`
	Name                  string                   `json:"name"`
	Description           string                   `json:"description"`
	RequiredAutonomy      string                   `json:"required_autonomy"`
	RiskClass             string                   `json:"risk_class"`
	SideEffectClass       string                   `json:"side_effect_class"`
	RequiresNexusApproval bool                     `json:"requires_nexus_approval"`
	EvidenceRequired      bool                     `json:"evidence_required"`
	RollbackCapabilityKey string                   `json:"rollback_capability_key"`
	PromotionState        string                   `json:"promotion_state"`
	Manifest              domain.Manifest          `json:"manifest"`
	ManifestHash          string                   `json:"manifest_hash"`
	ConformedHash         string                   `json:"conformed_hash"`
	ConformanceReport     domain.ConformanceReport `json:"conformance_report"`
	ConformedAt           *time.Time               `json:"conformed_at"`
	ActivatedAt           *time.Time               `json:"activated_at"`
	State                 string                   `json:"state"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
	ArchivedAt            *time.Time               `json:"archived_at"`
	TrashedAt             *time.Time               `json:"trashed_at"`
	PurgeAfter            *time.Time               `json:"purge_after"`
}

type ListCapabilitiesResponse struct {
	Data []CapabilityResponse `json:"data"`
}

func CapabilityFromDomain(capability domain.Capability) CapabilityResponse {
	return CapabilityResponse{
		ID:                    capability.ID.String(),
		TenantID:              capability.TenantID,
		CapabilityKey:         capability.CapabilityKey,
		Name:                  capability.Name,
		Description:           capability.Description,
		RequiredAutonomy:      string(capability.RequiredAutonomy),
		RiskClass:             capability.RiskClass,
		SideEffectClass:       capability.SideEffectClass,
		RequiresNexusApproval: capability.RequiresNexusApproval,
		EvidenceRequired:      capability.EvidenceRequired,
		RollbackCapabilityKey: capability.RollbackCapabilityKey,
		PromotionState:        string(capability.PromotionState),
		Manifest:              capability.Manifest,
		ManifestHash:          capability.ManifestHash,
		ConformedHash:         capability.ConformedHash,
		ConformanceReport:     capability.ConformanceReport,
		ConformedAt:           capability.ConformedAt,
		ActivatedAt:           capability.ActivatedAt,
		State:                 string(capability.State()),
		CreatedAt:             capability.CreatedAt,
		UpdatedAt:             capability.UpdatedAt,
		ArchivedAt:            capability.ArchivedAt,
		TrashedAt:             capability.TrashedAt,
		PurgeAfter:            capability.PurgeAfter,
	}
}

func ListCapabilitiesFromDomain(items []domain.Capability) ListCapabilitiesResponse {
	data := make([]CapabilityResponse, 0, len(items))
	for _, item := range items {
		data = append(data, CapabilityFromDomain(item))
	}
	return ListCapabilitiesResponse{Data: data}
}
