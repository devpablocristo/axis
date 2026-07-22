package dto

import (
	"time"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
)

type SessionResponse struct {
	PrincipalID   string                 `json:"principal_id"`
	ActorID       string                 `json:"actor_id"`
	OrgID         string                 `json:"org_id"`
	AuthMethod    string                 `json:"auth_method"`
	User          UserResponse           `json:"user"`
	Organizations []OrganizationResponse `json:"organizations"`
}

type UserResponse struct {
	ID             string     `json:"id"`
	Provider       string     `json:"provider"`
	ProviderUserID string     `json:"provider_user_id"`
	Email          string     `json:"email"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

type OrganizationResponse struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Products []ProductResponse `json:"products"`
}

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

func SessionFromDomain(session sessiondomain.Session) SessionResponse {
	organizations := make([]OrganizationResponse, 0)
	positions := make(map[string]int)
	for _, product := range session.Products {
		position, ok := positions[product.OrgID]
		if !ok {
			position = len(organizations)
			positions[product.OrgID] = position
			organizations = append(organizations, OrganizationResponse{ID: product.OrgID, Name: product.OrgName, Products: []ProductResponse{}})
		}
		organizations[position].Products = append(organizations[position].Products, ProductFromDomain(product))
	}
	return SessionResponse{
		PrincipalID:   session.PrincipalID,
		ActorID:       session.PrincipalID,
		OrgID:         session.OrgID,
		AuthMethod:    session.AuthMethod,
		User:          UserFromDomain(session.User),
		Organizations: organizations,
	}
}

func UserFromDomain(user userdomain.User) UserResponse {
	return UserResponse{
		ID:             user.ID,
		Provider:       user.Provider,
		ProviderUserID: user.ProviderUserID,
		Email:          user.Email,
		Status:         user.Status,
		CreatedAt:      user.CreatedAt,
		UpdatedAt:      user.UpdatedAt,
		ArchivedAt:     user.ArchivedAt,
		TrashedAt:      user.TrashedAt,
		PurgeAfter:     user.PurgeAfter,
	}
}

func ProductFromDomain(product productdomain.Product) ProductResponse {
	return ProductResponse{
		ID:             product.ID.String(),
		ProductSurface: product.ProductSurface,
		Name:           product.ProductName,
		Status:         product.Status,
		State:          product.State(),
		CreatedAt:      product.CreatedAt,
		UpdatedAt:      product.UpdatedAt,
		ArchivedAt:     product.ArchivedAt,
		TrashedAt:      product.TrashedAt,
		PurgeAfter:     product.PurgeAfter,
	}
}
