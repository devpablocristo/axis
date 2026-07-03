package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/users/usecases/domain"
)

type UserResponse struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	TenantID   string     `json:"tenant_id"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
	TrashedAt  *time.Time `json:"trashed_at"`
	PurgeAfter *time.Time `json:"purge_after"`
}

type ListUsersResponse struct {
	Data []UserResponse `json:"data"`
}

func UserFromDomain(user domain.User) UserResponse {
	return UserResponse{
		ID:         user.ID,
		Kind:       user.Kind,
		Email:      user.Email,
		Role:       user.Role,
		TenantID:   user.TenantID.String(),
		State:      user.State,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
		ArchivedAt: user.ArchivedAt,
		TrashedAt:  user.TrashedAt,
		PurgeAfter: user.PurgeAfter,
	}
}

func UsersFromDomain(items []domain.User) ListUsersResponse {
	data := make([]UserResponse, 0, len(items))
	for _, item := range items {
		data = append(data, UserFromDomain(item))
	}
	return ListUsersResponse{Data: data}
}
