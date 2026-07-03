package dto

import "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"

type CreateTenantRequest struct {
	OrgID          string `json:"org_id"`
	OrgName        string `json:"org_name"`
	ProductSurface string `json:"product_surface" binding:"required"`
}

func (r CreateTenantRequest) ToDomain(principalID string) domain.CreateTenantInput {
	return domain.CreateTenantInput{
		OrgID:          r.OrgID,
		OrgName:        r.OrgName,
		ProductSurface: r.ProductSurface,
		PrincipalID:    principalID,
		OwnerUserID:    principalID,
	}
}

type UpdateTenantRequest struct {
	OrgName string `json:"org_name" binding:"required"`
}

func (r UpdateTenantRequest) ToDomain(tenantID, principalID string) domain.UpdateTenantInput {
	return domain.UpdateTenantInput{
		TenantID:    tenantID,
		OrgName:     r.OrgName,
		PrincipalID: principalID,
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
