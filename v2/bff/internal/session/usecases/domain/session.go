package domain

import (
	"strings"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type ResolveInput struct {
	PrincipalID string
	Email       string
	Name        string
	OrgID       string
}

type Session struct {
	PrincipalID string
	OrgID       string
	AuthMethod  string
	User        userdomain.User
	Tenants     []tenantdomain.Tenant
}

func NormalizeResolveInput(in ResolveInput) (ResolveInput, error) {
	out := ResolveInput{
		PrincipalID: strings.TrimSpace(in.PrincipalID),
		Email:       strings.TrimSpace(in.Email),
		Name:        strings.TrimSpace(in.Name),
		OrgID:       strings.TrimSpace(in.OrgID),
	}
	if out.PrincipalID == "" {
		return ResolveInput{}, domainerr.Validation("principal_id is required")
	}
	if out.OrgID == "" {
		return ResolveInput{}, domainerr.Validation("org_id is required")
	}
	if out.Email == "" {
		out.Email = out.PrincipalID
	}
	if out.Name == "" {
		out.Name = out.Email
	}
	return out, nil
}
