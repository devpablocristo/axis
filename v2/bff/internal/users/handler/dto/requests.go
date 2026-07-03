package dto

import "github.com/devpablocristo/bff-v2/internal/users/usecases/domain"

type CreateUserRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role"`
}

func (r CreateUserRequest) ToDomain(tenantID, principalID string) domain.CreateInput {
	return domain.CreateInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
		Email:       r.Email,
		Role:        r.Role,
	}
}

type UpdateUserRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role"`
}

func (r UpdateUserRequest) ToDomain(tenantID, principalID, userID string) domain.UpdateInput {
	return domain.UpdateInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
		UserID:      userID,
		Email:       r.Email,
		Role:        r.Role,
	}
}
