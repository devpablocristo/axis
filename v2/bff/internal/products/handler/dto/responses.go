package dto

import (
	"time"

	"github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
)

type ProductResponse struct {
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

type ListProductsResponse struct {
	Data []ProductResponse `json:"data"`
}

func ProductFromDomain(product domain.Product) ProductResponse {
	return ProductResponse{
		ID:             product.ID.String(),
		ProductSurface: product.ProductSurface,
		Name:           product.Name,
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
