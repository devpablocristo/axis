package models

import (
	"database/sql"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
)

type User struct {
	ID        string
	Email     string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt sql.NullTime
	TrashedAt  sql.NullTime
	PurgeAfter sql.NullTime
}

func (m User) ToDomain() domain.User {
	return domain.User{
		ID:         m.ID,
		Email:      m.Email,
		Name:       m.Name,
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
