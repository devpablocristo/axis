package dto

import "github.com/devpablocristo/bff-v2/internal/orgs/usecases/domain"

type CreateOrgRequest struct {
	Name string `json:"name" binding:"required"`
}

func (r CreateOrgRequest) ToDomain(principalID string) domain.CreateInput {
	return domain.CreateInput{
		Name:        r.Name,
		PrincipalID: principalID,
	}
}

type UpdateOrgRequest struct {
	Name string `json:"name" binding:"required"`
}

func (r UpdateOrgRequest) ToDomain(orgID, principalID string) domain.UpdateInput {
	return domain.UpdateInput{
		OrgID:       orgID,
		Name:        r.Name,
		PrincipalID: principalID,
	}
}
