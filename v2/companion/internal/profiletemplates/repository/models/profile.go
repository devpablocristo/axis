package models

import (
	"database/sql"
	"time"

	"github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type ProfileTemplate struct {
	ID           uuid.UUID
	OrgID        string
	Name         string
	Description  string
	SystemPrompt string
	MaxAutonomy  virployeedomain.AutonomyLevel
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ArchivedAt   sql.NullTime
	TrashedAt    sql.NullTime
	PurgeAfter   sql.NullTime
}

func (m ProfileTemplate) ToDomain() domain.ProfileTemplate {
	return domain.ProfileTemplate{
		ID:           m.ID,
		OrgID:        m.OrgID,
		Name:         m.Name,
		Description:  m.Description,
		SystemPrompt: m.SystemPrompt,
		MaxAutonomy:  m.MaxAutonomy,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		ArchivedAt:   nullTimePtr(m.ArchivedAt),
		TrashedAt:    nullTimePtr(m.TrashedAt),
		PurgeAfter:   nullTimePtr(m.PurgeAfter),
	}
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
