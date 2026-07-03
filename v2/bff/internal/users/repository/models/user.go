package models

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/users/usecases/domain"
	"github.com/google/uuid"
)

type TenantUser struct {
	ID       string
	Kind     string
	Email    string
	Role     string
	TenantID uuid.UUID
	State    string

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

func (u TenantUser) ToDomain() domain.User {
	return domain.User{
		ID:         u.ID,
		Kind:       u.Kind,
		Email:      u.Email,
		Role:       u.Role,
		TenantID:   u.TenantID,
		State:      stateOrLifecycle(u.State, u.ArchivedAt, u.TrashedAt),
		CreatedAt:  u.CreatedAt,
		UpdatedAt:  u.UpdatedAt,
		ArchivedAt: u.ArchivedAt,
		TrashedAt:  u.TrashedAt,
		PurgeAfter: u.PurgeAfter,
	}
}

func stateOrLifecycle(state string, archivedAt, trashedAt *time.Time) string {
	if state != "" {
		return state
	}
	return domain.StateFromLifecycle(archivedAt, trashedAt)
}
