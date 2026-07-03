package models

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/google/uuid"
)

type Product struct {
	ID             uuid.UUID
	ProductSurface string
	Name           string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

func (p Product) ToDomain() domain.Product {
	return domain.Product{
		ID:             p.ID,
		ProductSurface: p.ProductSurface,
		Name:           p.Name,
		Status:         p.Status,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
		ArchivedAt:     p.ArchivedAt,
		TrashedAt:      p.TrashedAt,
		PurgeAfter:     p.PurgeAfter,
	}
}
