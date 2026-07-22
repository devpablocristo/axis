package domain

import (
	"strings"

	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type ResolveInput struct {
	OrgID          string
	ProductSurface string
	PrincipalID    string
}

type ResolvedContext struct {
	PrincipalID    string
	OrgID          string
	ProductSurface string
	MembershipRole string
	Product        productdomain.Product
	Member         productdomain.OrgMember
}

func NormalizeResolveInput(in ResolveInput) (ResolveInput, error) {
	out := ResolveInput{
		OrgID:          strings.TrimSpace(in.OrgID),
		ProductSurface: strings.ToLower(strings.TrimSpace(in.ProductSurface)),
		PrincipalID:    strings.TrimSpace(in.PrincipalID),
	}
	if out.OrgID == "" {
		return ResolveInput{}, domainerr.Validation("org_id is required")
	}
	if out.ProductSurface == "" {
		return ResolveInput{}, domainerr.Validation("product_surface is required")
	}
	if out.PrincipalID == "" {
		return ResolveInput{}, domainerr.Validation("principal_id is required")
	}
	return out, nil
}
