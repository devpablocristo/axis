package models

import (
	"database/sql"
	"time"

	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/google/uuid"
)

type Org struct {
	ID            string
	Provider      string
	ProviderOrgID string
	Name          string
	Slug          string
	Status        string
	SyncedAt      sql.NullTime
	CreatedAt     time.Time
	UpdatedAt     time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

type Product struct {
	ID             uuid.UUID
	OrgID          string
	OrgName        string
	ProductSurface string
	ProductName    string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

type OrgMember struct {
	OrgID     uuid.UUID
	UserID    string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

func (m Org) ToDomain() domain.Org {
	return domain.Org{
		ID:            m.ID,
		Provider:      m.Provider,
		ProviderOrgID: m.ProviderOrgID,
		Name:          m.Name,
		Slug:          m.Slug,
		Status:        m.Status,
		SyncedAt:      nullTimePtr(m.SyncedAt),
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		ArchivedAt:    nullTimePtr(m.ArchivedAt),
		TrashedAt:     nullTimePtr(m.TrashedAt),
		PurgeAfter:    nullTimePtr(m.PurgeAfter),
	}
}

func (m Product) ToDomain() domain.Product {
	return domain.Product{
		ID:             m.ID,
		OrgID:          m.OrgID,
		OrgName:        m.OrgName,
		ProductSurface: m.ProductSurface,
		ProductName:    m.ProductName,
		Status:         m.Status,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		ArchivedAt:     nullTimePtr(m.ArchivedAt),
		TrashedAt:      nullTimePtr(m.TrashedAt),
		PurgeAfter:     nullTimePtr(m.PurgeAfter),
	}
}

func (m OrgMember) ToDomain() domain.OrgMember {
	return domain.OrgMember{
		OrgID:      m.OrgID,
		UserID:     m.UserID,
		Role:       m.Role,
		Status:     m.Status,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
		ArchivedAt: nullTimePtr(m.ArchivedAt),
		TrashedAt:  nullTimePtr(m.TrashedAt),
		PurgeAfter: nullTimePtr(m.PurgeAfter),
	}
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
