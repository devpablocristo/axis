package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
)

type UserResponse struct {
	ID             string     `json:"id"`
	Provider       string     `json:"provider"`
	ProviderUserID string     `json:"provider_user_id"`
	Email          string     `json:"email"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

func UserFromDomain(user domain.User) UserResponse {
	return UserResponse{
		ID:             user.ID,
		Provider:       user.Provider,
		ProviderUserID: user.ProviderUserID,
		Email:          user.Email,
		Status:         user.Status,
		CreatedAt:      user.CreatedAt,
		UpdatedAt:      user.UpdatedAt,
		ArchivedAt:     user.ArchivedAt,
		TrashedAt:      user.TrashedAt,
		PurgeAfter:     user.PurgeAfter,
	}
}
