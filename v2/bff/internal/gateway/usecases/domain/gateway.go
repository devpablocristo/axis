package domain

import (
	"strings"

	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type ResolveInput struct {
	TenantID    string
	PrincipalID string
}

type ResolvedContext struct {
	PrincipalID    string
	TenantID       string
	OrgID          string
	ProductSurface string
	MembershipRole string
	Tenant         tenantdomain.Tenant
	Member         tenantdomain.TenantMember
}

func NormalizeResolveInput(in ResolveInput) (ResolveInput, error) {
	out := ResolveInput{
		TenantID:    strings.TrimSpace(in.TenantID),
		PrincipalID: strings.TrimSpace(in.PrincipalID),
	}
	if out.TenantID == "" {
		return ResolveInput{}, domainerr.Validation("tenant_id is required")
	}
	if out.PrincipalID == "" {
		return ResolveInput{}, domainerr.Validation("principal_id is required")
	}
	return out, nil
}
