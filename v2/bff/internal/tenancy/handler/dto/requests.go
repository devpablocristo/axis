package dto

import "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"

type CreateTenantRequest struct {
	OrgID          string `json:"org_id" binding:"required"`
	ProductSurface string `json:"product_surface" binding:"required"`
	Name           string `json:"name"`
	OwnerUserID    string `json:"owner_user_id"`
}

func (r CreateTenantRequest) ToDomain() domain.CreateTenantInput {
	return domain.CreateTenantInput{
		OrgID:          r.OrgID,
		ProductSurface: r.ProductSurface,
		Name:           r.Name,
		OwnerUserID:    r.OwnerUserID,
	}
}

type AddTenantMemberRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Role   string `json:"role"`
}

func (r AddTenantMemberRequest) ToDomain(tenantID string) domain.AddMemberInput {
	return domain.AddMemberInput{
		TenantID: tenantID,
		UserID:   r.UserID,
		Role:     r.Role,
	}
}
