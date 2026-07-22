package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
)

type ProductResponse struct {
	ID             string     `json:"id"`
	OrgID          string     `json:"org_id"`
	OrgName        string     `json:"org_name"`
	ProductSurface string     `json:"product_surface"`
	ProductName    string     `json:"product_name"`
	Status         string     `json:"status"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

type OrganizationProductResponse struct {
	ID             string     `json:"id"`
	ProductSurface string     `json:"product_surface"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

type ListOrganizationProductsResponse struct {
	Data []OrganizationProductResponse `json:"data"`
}

type ListProductsResponse struct {
	Data []ProductResponse `json:"data"`
}

type OrgMemberResponse struct {
	OrgID      string     `json:"org_id"`
	UserID     string     `json:"user_id"`
	Role       string     `json:"role"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
	TrashedAt  *time.Time `json:"trashed_at"`
	PurgeAfter *time.Time `json:"purge_after"`
}

func ProductFromDomain(product domain.Product) ProductResponse {
	return ProductResponse{
		ID:             product.ID.String(),
		OrgID:          product.OrgID,
		OrgName:        product.OrgName,
		ProductSurface: product.ProductSurface,
		ProductName:    product.ProductName,
		Status:         product.Status,
		State:          product.State(),
		CreatedAt:      product.CreatedAt,
		UpdatedAt:      product.UpdatedAt,
		ArchivedAt:     product.ArchivedAt,
		TrashedAt:      product.TrashedAt,
		PurgeAfter:     product.PurgeAfter,
	}
}

func ProductsFromDomain(items []domain.Product) ListProductsResponse {
	data := make([]ProductResponse, 0, len(items))
	for _, item := range items {
		data = append(data, ProductFromDomain(item))
	}
	return ListProductsResponse{Data: data}
}

func OrganizationProductFromDomain(product domain.Product) OrganizationProductResponse {
	return OrganizationProductResponse{
		ID: product.ID.String(), ProductSurface: product.ProductSurface, Name: product.ProductName,
		Status: product.Status, State: product.State(), CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt,
		ArchivedAt: product.ArchivedAt, TrashedAt: product.TrashedAt, PurgeAfter: product.PurgeAfter,
	}
}

func OrganizationProductsFromDomain(items []domain.Product) ListOrganizationProductsResponse {
	data := make([]OrganizationProductResponse, 0, len(items))
	for _, item := range items {
		data = append(data, OrganizationProductFromDomain(item))
	}
	return ListOrganizationProductsResponse{Data: data}
}

func OrgMemberFromDomain(member domain.OrgMember) OrgMemberResponse {
	return OrgMemberResponse{
		OrgID:      member.OrgID.String(),
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
