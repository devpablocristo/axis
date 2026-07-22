package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/orgs/usecases/domain"
)

type OrgResponse struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Provider      string     `json:"provider"`
	ProviderOrgID string     `json:"provider_org_id"`
	Status        string     `json:"status"`
	State         string     `json:"state"`
	ProductCount  int        `json:"product_count"`
	HasProducts   bool       `json:"has_products"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ArchivedAt    *time.Time `json:"archived_at"`
	TrashedAt     *time.Time `json:"trashed_at"`
	PurgeAfter    *time.Time `json:"purge_after"`
}

type ListOrgsResponse struct {
	Data []OrgResponse `json:"data"`
}

func OrgFromDomain(org domain.Org) OrgResponse {
	return OrgResponse{
		ID:            org.ID,
		Name:          org.Name,
		Provider:      org.Provider,
		ProviderOrgID: org.ProviderOrgID,
		Status:        org.Status,
		State:         org.State(),
		ProductCount:  org.ProductCount,
		HasProducts:   org.HasProducts(),
		CreatedAt:     org.CreatedAt,
		UpdatedAt:     org.UpdatedAt,
		ArchivedAt:    org.ArchivedAt,
		TrashedAt:     org.TrashedAt,
		PurgeAfter:    org.PurgeAfter,
	}
}

func OrgsFromDomain(items []domain.Org) ListOrgsResponse {
	data := make([]OrgResponse, 0, len(items))
	for _, item := range items {
		data = append(data, OrgFromDomain(item))
	}
	return ListOrgsResponse{Data: data}
}
