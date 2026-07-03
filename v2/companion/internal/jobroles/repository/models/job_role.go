package models

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/google/uuid"
)

type JobRole struct {
	ID         uuid.UUID
	TenantID   string
	Name       string
	Slug       string
	Mission    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

func (m JobRole) ToDomain() domain.JobRole {
	return domain.JobRole{
		ID:         m.ID,
		TenantID:   m.TenantID,
		Name:       m.Name,
		Slug:       m.Slug,
		Mission:    m.Mission,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
		ArchivedAt: m.ArchivedAt,
		TrashedAt:  m.TrashedAt,
		PurgeAfter: m.PurgeAfter,
	}
}
