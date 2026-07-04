package dto

import (
	"time"

	"github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
)

type ProfileTemplateResponse struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	SystemPrompt string     `json:"system_prompt"`
	MaxAutonomy  string     `json:"max_autonomy"`
	State        string     `json:"state"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ArchivedAt   *time.Time `json:"archived_at"`
	TrashedAt    *time.Time `json:"trashed_at"`
	PurgeAfter   *time.Time `json:"purge_after"`
}

type ListProfileTemplatesResponse struct {
	Data []ProfileTemplateResponse `json:"data"`
}

func ProfileTemplateFromDomain(profile domain.ProfileTemplate) ProfileTemplateResponse {
	return ProfileTemplateResponse{
		ID:           profile.ID.String(),
		TenantID:     profile.TenantID,
		Name:         profile.Name,
		Description:  profile.Description,
		SystemPrompt: profile.SystemPrompt,
		MaxAutonomy:  string(profile.MaxAutonomy),
		State:        string(profile.State()),
		CreatedAt:    profile.CreatedAt,
		UpdatedAt:    profile.UpdatedAt,
		ArchivedAt:   profile.ArchivedAt,
		TrashedAt:    profile.TrashedAt,
		PurgeAfter:   profile.PurgeAfter,
	}
}

func ListProfileTemplatesFromDomain(items []domain.ProfileTemplate) ListProfileTemplatesResponse {
	data := make([]ProfileTemplateResponse, 0, len(items))
	for _, item := range items {
		data = append(data, ProfileTemplateFromDomain(item))
	}
	return ListProfileTemplatesResponse{Data: data}
}
