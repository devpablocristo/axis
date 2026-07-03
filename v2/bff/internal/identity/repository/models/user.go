package models

import (
	"database/sql"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
)

type User struct {
	ID             string
	Provider       string
	ProviderUserID string
	Email          string
	Status         string
	SyncedAt       sql.NullTime
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

func (m User) ToDomain() domain.User {
	return domain.User{
		ID:             m.ID,
		Provider:       m.Provider,
		ProviderUserID: m.ProviderUserID,
		Email:          m.Email,
		Status:         m.Status,
		SyncedAt:       nullTimePtr(m.SyncedAt),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		ArchivedAt:     nullTimePtr(m.ArchivedAt),
		TrashedAt:      nullTimePtr(m.TrashedAt),
		PurgeAfter:     nullTimePtr(m.PurgeAfter),
	}
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
