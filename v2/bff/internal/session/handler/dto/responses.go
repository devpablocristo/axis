package dto

import (
	"time"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
)

type SessionResponse struct {
	PrincipalID string           `json:"principal_id"`
	ActorID     string           `json:"actor_id"`
	OrgID       string           `json:"org_id"`
	AuthMethod  string           `json:"auth_method"`
	User        UserResponse     `json:"user"`
	Tenants     []TenantResponse `json:"tenants"`
}

type UserResponse struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
	TrashedAt  *time.Time `json:"trashed_at"`
	PurgeAfter *time.Time `json:"purge_after"`
}

type TenantResponse struct {
	ID             string     `json:"id"`
	OrgID          string     `json:"org_id"`
	ProductSurface string     `json:"product_surface"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
	TrashedAt      *time.Time `json:"trashed_at"`
	PurgeAfter     *time.Time `json:"purge_after"`
}

func SessionFromDomain(session sessiondomain.Session) SessionResponse {
	tenants := make([]TenantResponse, 0, len(session.Tenants))
	for _, tenant := range session.Tenants {
		tenants = append(tenants, TenantFromDomain(tenant))
	}
	return SessionResponse{
		PrincipalID: session.PrincipalID,
		ActorID:     session.PrincipalID,
		OrgID:       session.OrgID,
		AuthMethod:  session.AuthMethod,
		User:        UserFromDomain(session.User),
		Tenants:     tenants,
	}
}

func UserFromDomain(user userdomain.User) UserResponse {
	return UserResponse{
		ID:         user.ID,
		Email:      user.Email,
		Name:       user.Name,
		Status:     user.Status,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
		ArchivedAt: user.ArchivedAt,
		TrashedAt:  user.TrashedAt,
		PurgeAfter: user.PurgeAfter,
	}
}

func TenantFromDomain(tenant tenantdomain.Tenant) TenantResponse {
	return TenantResponse{
		ID:             tenant.ID.String(),
		OrgID:          tenant.OrgID,
		ProductSurface: tenant.ProductSurface,
		Name:           tenant.Name,
		Status:         tenant.Status,
		CreatedAt:      tenant.CreatedAt,
		UpdatedAt:      tenant.UpdatedAt,
		ArchivedAt:     tenant.ArchivedAt,
		TrashedAt:      tenant.TrashedAt,
		PurgeAfter:     tenant.PurgeAfter,
	}
}
