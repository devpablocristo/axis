package models

import (
	"database/sql"
	"time"

	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
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

type Tenant struct {
	ID             uuid.UUID
	OrgID          string
	OrgName        string
	ProductSurface string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

type TenantMember struct {
	TenantID  uuid.UUID
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

func (m Tenant) ToDomain() domain.Tenant {
	return domain.Tenant{
		ID:             m.ID,
		OrgID:          m.OrgID,
		OrgName:        m.OrgName,
		ProductSurface: m.ProductSurface,
		Status:         m.Status,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		ArchivedAt:     nullTimePtr(m.ArchivedAt),
		TrashedAt:      nullTimePtr(m.TrashedAt),
		PurgeAfter:     nullTimePtr(m.PurgeAfter),
	}
}

func (m TenantMember) ToDomain() domain.TenantMember {
	return domain.TenantMember{
		TenantID:   m.TenantID,
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
