package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
)

type TenantResponse struct {
	ID             string     `json:"id"`
	OrgID          string     `json:"org_id"`
	OrgName        string     `json:"org_name"`
	ProductSurface string     `json:"product_surface"`
	Status         string     `json:"status"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

type ListTenantsResponse struct {
	Data []TenantResponse `json:"data"`
}

type ProductResponse struct {
	ProductSurface string `json:"product_surface"`
	Name           string `json:"name"`
}

type ListProductsResponse struct {
	Data []ProductResponse `json:"data"`
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
		OrgName:        tenant.OrgName,
		ProductSurface: tenant.ProductSurface,
		Status:         tenant.Status,
		State:          tenant.State(),
		CreatedAt:      tenant.CreatedAt,
		UpdatedAt:      tenant.UpdatedAt,
		ArchivedAt:     tenant.ArchivedAt,
		TrashedAt:      tenant.TrashedAt,
		PurgeAfter:     tenant.PurgeAfter,
	}
}

func ProductsFromDomain(items []domain.Product) ListProductsResponse {
	data := make([]ProductResponse, 0, len(items))
	for _, item := range items {
		data = append(data, ProductResponse{
			ProductSurface: item.ProductSurface,
			Name:           item.Name,
		})
	}
	return ListProductsResponse{Data: data}
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
