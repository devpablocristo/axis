package domain

import (
	"strings"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type ResolveInput struct {
	PrincipalID   string
	Email         string
	OrgID         string
	Authorization string
}

type Session struct {
	PrincipalID string
	OrgID       string
	AuthMethod  string
	User        userdomain.User
	Products    []productdomain.Product
}

func NormalizeResolveInput(in ResolveInput) (ResolveInput, error) {
	out := ResolveInput{
		PrincipalID: strings.TrimSpace(in.PrincipalID),
		Email:       strings.TrimSpace(in.Email),
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
	return out, nil
}
