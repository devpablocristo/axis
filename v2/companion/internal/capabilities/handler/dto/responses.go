package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
)

type CapabilityResponse struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	CapabilityKey    string     `json:"capability_key"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	RequiredAutonomy string     `json:"required_autonomy"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ArchivedAt       *time.Time `json:"archived_at"`
	TrashedAt        *time.Time `json:"trashed_at"`
	PurgeAfter       *time.Time `json:"purge_after"`
}

type ListCapabilitiesResponse struct {
	Data []CapabilityResponse `json:"data"`
}

func CapabilityFromDomain(capability domain.Capability) CapabilityResponse {
	return CapabilityResponse{
		ID:               capability.ID.String(),
		TenantID:         capability.TenantID,
		CapabilityKey:    capability.CapabilityKey,
		Name:             capability.Name,
		Description:      capability.Description,
		RequiredAutonomy: string(capability.RequiredAutonomy),
		State:            string(capability.State()),
		CreatedAt:        capability.CreatedAt,
		UpdatedAt:        capability.UpdatedAt,
		ArchivedAt:       capability.ArchivedAt,
		TrashedAt:        capability.TrashedAt,
		PurgeAfter:       capability.PurgeAfter,
	}
}

func ListCapabilitiesFromDomain(items []domain.Capability) ListCapabilitiesResponse {
	data := make([]CapabilityResponse, 0, len(items))
	for _, item := range items {
		data = append(data, CapabilityFromDomain(item))
	}
	return ListCapabilitiesResponse{Data: data}
}
