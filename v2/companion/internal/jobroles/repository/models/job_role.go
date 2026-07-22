package models

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	"github.com/google/uuid"
)

type JobRole struct {
	ID               uuid.UUID
	OrgID            string
	Name             string
	Slug             string
	Mission          string
	Responsibilities []domain.Responsibility
	SuccessCriteria  []domain.SuccessCriterion
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ArchivedAt       *time.Time
	TrashedAt        *time.Time
	PurgeAfter       *time.Time
}

func (m JobRole) ToDomain() domain.JobRole {
	responsibilities := make([]domain.Responsibility, len(m.Responsibilities))
	copy(responsibilities, m.Responsibilities)
	successCriteria := make([]domain.SuccessCriterion, len(m.SuccessCriteria))
	copy(successCriteria, m.SuccessCriteria)
	return domain.JobRole{
		ID:               m.ID,
		OrgID:            m.OrgID,
		Name:             m.Name,
		Slug:             m.Slug,
		Mission:          m.Mission,
		Responsibilities: responsibilities,
		SuccessCriteria:  successCriteria,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		ArchivedAt:       m.ArchivedAt,
		TrashedAt:        m.TrashedAt,
		PurgeAfter:       m.PurgeAfter,
	}
}
