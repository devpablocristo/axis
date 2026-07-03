package dto

import "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"

type CreateProductRequest struct {
	Name           string `json:"name" binding:"required"`
	ProductSurface string `json:"product_surface"`
}

func (r CreateProductRequest) ToDomain(principalID string) domain.CreateInput {
	return domain.CreateInput{
		Name:           r.Name,
		ProductSurface: r.ProductSurface,
		PrincipalID:    principalID,
	}
}

type UpdateProductRequest struct {
	Name string `json:"name" binding:"required"`
}

func (r UpdateProductRequest) ToDomain(productID, principalID string) domain.UpdateInput {
	return domain.UpdateInput{
		ProductID:   productID,
		Name:        r.Name,
		PrincipalID: principalID,
	}
}
