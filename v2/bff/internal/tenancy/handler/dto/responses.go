package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
)

type TenantResponse struct {
	ID             string     `json:"id"`
	OrgID          string     `json:"org_id"`
	ProductSurface string     `json:"product_surface"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

type ListTenantsResponse struct {
	Data []TenantResponse `json:"data"`
}

type TenantMemberResponse struct {
	TenantID   string     `json:"tenant_id"`
	UserID     string     `json:"user_id"`
	Role       string     `json:"role"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
	TrashedAt  *time.Time `json:"trashed_at"`
	PurgeAfter *time.Time `json:"purge_after"`
}

func TenantFromDomain(tenant domain.Tenant) TenantResponse {
	return TenantResponse{
		ID:             tenant.ID.String(),
		OrgID:          tenant.OrgID,
		ProductSurface: tenant.ProductSurface,
		Name:           tenant.Name,
		Status:         tenant.Status,
		CreatedAt:      tenant.CreatedAt,
		UpdatedAt:      tenant.UpdatedAt,
		ArchivedAt:     tenant.ArchivedAt,
		TrashedAt:      tenant.TrashedAt,
		PurgeAfter:     tenant.PurgeAfter,
	}
}

func TenantsFromDomain(items []domain.Tenant) ListTenantsResponse {
	data := make([]TenantResponse, 0, len(items))
	for _, item := range items {
		data = append(data, TenantFromDomain(item))
	}
	return ListTenantsResponse{Data: data}
}

func TenantMemberFromDomain(member domain.TenantMember) TenantMemberResponse {
	return TenantMemberResponse{
		TenantID:   member.TenantID.String(),
		UserID:     member.UserID,
		Role:       member.Role,
		Status:     member.Status,
		CreatedAt:  member.CreatedAt,
		UpdatedAt:  member.UpdatedAt,
		ArchivedAt: member.ArchivedAt,
		TrashedAt:  member.TrashedAt,
		PurgeAfter: member.PurgeAfter,
	}
}
