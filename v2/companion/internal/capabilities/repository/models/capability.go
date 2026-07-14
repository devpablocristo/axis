package models

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type Capability struct {
	ID                    uuid.UUID
	TenantID              string
	CapabilityKey         string
	Name                  string
	Description           string
	RequiredAutonomy      virployeedomain.AutonomyLevel
	RiskClass             string
	SideEffectClass       string
	RequiresNexusApproval bool
	EvidenceRequired      bool
	RollbackCapabilityKey string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ArchivedAt            *time.Time
	TrashedAt             *time.Time
	PurgeAfter            *time.Time
}

func (m Capability) ToDomain() domain.Capability {
	return domain.Capability{
		ID:                    m.ID,
		TenantID:              m.TenantID,
		CapabilityKey:         m.CapabilityKey,
		Name:                  m.Name,
		Description:           m.Description,
		RequiredAutonomy:      m.RequiredAutonomy,
		RiskClass:             m.RiskClass,
		SideEffectClass:       m.SideEffectClass,
		RequiresNexusApproval: m.RequiresNexusApproval,
		EvidenceRequired:      m.EvidenceRequired,
		RollbackCapabilityKey: m.RollbackCapabilityKey,
		CreatedAt:             m.CreatedAt,
		UpdatedAt:             m.UpdatedAt,
		ArchivedAt:            m.ArchivedAt,
		TrashedAt:             m.TrashedAt,
		PurgeAfter:            m.PurgeAfter,
	}
}
