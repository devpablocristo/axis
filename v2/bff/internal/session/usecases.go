package session

import (
	"context"
	"strings"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
)

type Defaults struct {
	PrincipalID    string
	PrincipalEmail string
	PrincipalName  string
	OrgID          string
}

type IdentityPort interface {
	Ensure(ctx context.Context, input userdomain.EnsureInput) (userdomain.User, error)
}

type TenancyPort interface {
	EnsureDefaultTenant(ctx context.Context, orgID, orgName, userID string) (tenantdomain.Tenant, error)
	ListForPrincipal(ctx context.Context, userID string) ([]tenantdomain.Tenant, error)
}

type UseCases struct {
	identity IdentityPort
	tenancy  TenancyPort
	defaults Defaults
}

func NewUseCases(identity IdentityPort, tenancy TenancyPort, defaults Defaults) *UseCases {
	return &UseCases{identity: identity, tenancy: tenancy, defaults: defaults}
}

func (u *UseCases) Resolve(ctx context.Context, input sessiondomain.ResolveInput) (sessiondomain.Session, error) {
	input = u.applyDefaults(input)
	normalized, err := sessiondomain.NormalizeResolveInput(input)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	user, err := u.identity.Ensure(ctx, userdomain.EnsureInput{
		ID:    normalized.PrincipalID,
		Email: normalized.Email,
		Name:  normalized.Name,
	})
	if err != nil {
		return sessiondomain.Session{}, err
	}
	if _, err := u.tenancy.EnsureDefaultTenant(ctx, normalized.OrgID, normalized.OrgID, user.ID); err != nil {
		return sessiondomain.Session{}, err
	}
	tenants, err := u.tenancy.ListForPrincipal(ctx, user.ID)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	return sessiondomain.Session{
		PrincipalID: user.ID,
		OrgID:       normalized.OrgID,
		AuthMethod:  "dev",
		User:        user,
		Tenants:     tenants,
	}, nil
}

func (u *UseCases) applyDefaults(input sessiondomain.ResolveInput) sessiondomain.ResolveInput {
	if strings.TrimSpace(input.PrincipalID) == "" {
		input.PrincipalID = u.defaults.PrincipalID
	}
	if strings.TrimSpace(input.Email) == "" {
		input.Email = u.defaults.PrincipalEmail
	}
	if strings.TrimSpace(input.Name) == "" {
		input.Name = u.defaults.PrincipalName
	}
	if strings.TrimSpace(input.OrgID) == "" {
		input.OrgID = u.defaults.OrgID
	}
	return input
}
