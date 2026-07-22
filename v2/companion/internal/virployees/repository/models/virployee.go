package models

import (
	"database/sql"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type Virployee struct {
	ID                uuid.UUID
	Name              string
	JobRoleID         uuid.UUID
	ProfileTemplateID uuid.UUID
	CapabilityIDs     []uuid.UUID
	Description       string
	SupervisorUserID  string
	Autonomy          domain.AutonomyLevel
	GroundingMode     domain.GroundingMode

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

func (m Virployee) ToDomain() domain.Virployee {
	return domain.Virployee{
		ID:                m.ID,
		Name:              m.Name,
		JobRoleID:         m.JobRoleID,
		ProfileTemplateID: m.ProfileTemplateID,
		CapabilityIDs:     m.CapabilityIDs,
		Description:       m.Description,
		SupervisorUserID:  m.SupervisorUserID,
		Autonomy:          m.Autonomy,
		GroundingMode:     m.GroundingMode,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
		ArchivedAt:        nullTimePtr(m.ArchivedAt),
		TrashedAt:         nullTimePtr(m.TrashedAt),
		PurgeAfter:        nullTimePtr(m.PurgeAfter),
	}
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
