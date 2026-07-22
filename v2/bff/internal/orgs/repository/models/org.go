package models

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/orgs/usecases/domain"
)

type Org struct {
	ID            string
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      *time.Time
	ProductCount  int
	CreatedAt     time.Time
	UpdatedAt     time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

func (o Org) ToDomain() domain.Org {
	return domain.Org{
		ID:            o.ID,
		Provider:      o.Provider,
		ProviderOrgID: o.ProviderOrgID,
		Name:          o.Name,
		Slug:          o.Slug,
		Status:        o.Status,
		SyncedAt:      o.SyncedAt,
		ProductCount:  o.ProductCount,
		CreatedAt:     o.CreatedAt,
		UpdatedAt:     o.UpdatedAt,
		ArchivedAt:    o.ArchivedAt,
		TrashedAt:     o.TrashedAt,
		PurgeAfter:    o.PurgeAfter,
	}
}
