package dto

import "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"

type CreateProductRequest struct {
	OrgID          string `json:"org_id"`
	OrgName        string `json:"org_name"`
	ProductSurface string `json:"product_surface" binding:"required"`
}

func (r CreateProductRequest) ToDomain(principalID string) domain.CreateProductInput {
	return domain.CreateProductInput{
		OrgID:          r.OrgID,
		OrgName:        r.OrgName,
		ProductSurface: r.ProductSurface,
		PrincipalID:    principalID,
		OwnerUserID:    principalID,
	}
}

type UpdateProductRequest struct {
	OrgName string `json:"org_name" binding:"required"`
}

func (r UpdateProductRequest) ToDomain(productID, principalID string) domain.UpdateProductInput {
	return domain.UpdateProductInput{
		ProductID:   productID,
		OrgName:     r.OrgName,
		PrincipalID: principalID,
	}
}

type AddOrgMemberRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Role   string `json:"role"`
}

func (r AddOrgMemberRequest) ToDomain(productID string) domain.AddMemberInput {
	return domain.AddMemberInput{
		ProductID: productID,
		UserID:    r.UserID,
		Role:      r.Role,
	}
}
