package dto

import "github.com/devpablocristo/bff-v2/internal/users/usecases/domain"

type CreateUserRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role"`
}

func (r CreateUserRequest) ToDomain(orgID, principalID string) domain.CreateInput {
	return domain.CreateInput{
		OrgID:       orgID,
		PrincipalID: principalID,
		Email:       r.Email,
		Role:        r.Role,
	}
}

type UpdateUserRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role"`
}

func (r UpdateUserRequest) ToDomain(orgID, principalID, userID string) domain.UpdateInput {
	return domain.UpdateInput{
		OrgID:       orgID,
		PrincipalID: principalID,
		UserID:      userID,
		Email:       r.Email,
		Role:        r.Role,
	}
}
