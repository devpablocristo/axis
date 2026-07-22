package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	userdomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	sessiondomain "github.com/devpablocristo/bff-v2/internal/session/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type Defaults struct {
	PrincipalID    string
	PrincipalEmail string
	OrgID          string
}

type IdentityPort interface {
	Ensure(ctx context.Context, input userdomain.EnsureInput) (userdomain.User, error)
}

type OrganizationAccessPort interface {
	EnsureDefaultProduct(ctx context.Context, orgID, orgName, userID string) (productdomain.Product, error)
	EnsureProviderDefaultProduct(ctx context.Context, input productdomain.EnsureOrgInput, userID string) (productdomain.Product, error)
	EnsureProviderDefaultProductWithRole(ctx context.Context, input productdomain.EnsureOrgInput, userID, role string) (productdomain.Product, error)
	ListForPrincipal(ctx context.Context, userID string) ([]productdomain.Product, error)
}

type OrgProviderPort interface {
	ListUserOrgMemberships(ctx context.Context, providerUserID string) ([]userdomain.ProviderOrgMembership, error)
}

type TokenVerifierPort interface {
	VerifyToken(ctx context.Context, token string) (map[string]any, error)
}

type UseCases struct {
	identity      IdentityPort
	products      OrganizationAccessPort
	defaults      Defaults
	tokenVerifier TokenVerifierPort
	orgProvider   OrgProviderPort
}

func NewUseCases(identity IdentityPort, products OrganizationAccessPort, defaults Defaults, tokenVerifier TokenVerifierPort, orgProvider OrgProviderPort) *UseCases {
	return &UseCases{identity: identity, products: products, defaults: defaults, tokenVerifier: tokenVerifier, orgProvider: orgProvider}
}

func (u *UseCases) Resolve(ctx context.Context, input sessiondomain.ResolveInput) (sessiondomain.Session, error) {
	token := bearerToken(input.Authorization)
	if u.tokenVerifier != nil {
		if token == "" {
			return sessiondomain.Session{}, domainerr.Unauthorized("session token is required")
		}
		return u.resolveClerk(ctx, token)
	}
	if token != "" {
		return sessiondomain.Session{}, domainerr.Unauthorized("session token verification is not configured")
	}
	input = u.applyDefaults(input)
	normalized, err := sessiondomain.NormalizeResolveInput(input)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	user, err := u.identity.Ensure(ctx, userdomain.EnsureInput{
		Provider:       userdomain.ProviderDev,
		ProviderUserID: normalized.PrincipalID,
		Email:          normalized.Email,
	})
	if err != nil {
		return sessiondomain.Session{}, err
	}
	product, err := u.products.EnsureDefaultProduct(ctx, normalized.OrgID, normalized.OrgID, user.ID)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	products, err := u.products.ListForPrincipal(ctx, user.ID)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	return sessiondomain.Session{
		PrincipalID: user.ID,
		OrgID:       product.OrgID,
		AuthMethod:  "dev",
		User:        user,
		Products:    products,
	}, nil
}

func (u *UseCases) resolveClerk(ctx context.Context, token string) (sessiondomain.Session, error) {
	claims, err := u.tokenVerifier.VerifyToken(ctx, token)
	if err != nil {
		return sessiondomain.Session{}, domainerr.Unauthorized("invalid session token")
	}
	providerUserID := firstClaim(claims, "sub")
	if providerUserID == "" {
		return sessiondomain.Session{}, domainerr.Unauthorized("session token is missing subject")
	}
	email := firstNonEmpty(firstClaim(claims, "email", "primary_email_address"), providerUserID)
	now := time.Now().UTC()
	user, err := u.identity.Ensure(ctx, userdomain.EnsureInput{
		Provider:       userdomain.ProviderClerk,
		ProviderUserID: providerUserID,
		Email:          email,
		Status:         userdomain.StatusActive,
		SyncedAt:       &now,
	})
	if err != nil {
		return sessiondomain.Session{}, err
	}

	providerOrgID := firstClaim(claims, "org_id", "orgId")
	memberships, err := u.providerOrgMemberships(ctx, providerUserID, providerOrgID, claims, now)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	orgID := ""
	for _, membership := range memberships {
		product, err := u.products.EnsureProviderDefaultProductWithRole(ctx, productdomain.EnsureOrgInput{
			Provider:      membership.Org.Provider,
			ProviderOrgID: membership.Org.ProviderOrgID,
			Name:          membership.Org.Name,
			Slug:          membership.Org.Slug,
			Status:        membership.Org.Status,
			SyncedAt:      membership.Org.SyncedAt,
		}, user.ID, providerProductRole(membership.Role))
		if err != nil {
			return sessiondomain.Session{}, err
		}
		if orgID == "" {
			orgID = product.OrgID
		}
	}
	if orgID == "" && strings.TrimSpace(u.defaults.OrgID) != "" {
		product, err := u.products.EnsureDefaultProduct(ctx, u.defaults.OrgID, u.defaults.OrgID, user.ID)
		if err != nil {
			return sessiondomain.Session{}, err
		}
		orgID = product.OrgID
	}
	products, err := u.products.ListForPrincipal(ctx, user.ID)
	if err != nil {
		return sessiondomain.Session{}, err
	}
	return sessiondomain.Session{
		PrincipalID: user.ID,
		OrgID:       orgID,
		AuthMethod:  "clerk",
		User:        user,
		Products:    products,
	}, nil
}

func (u *UseCases) providerOrgMemberships(
	ctx context.Context,
	providerUserID string,
	activeProviderOrgID string,
	claims map[string]any,
	now time.Time,
) ([]userdomain.ProviderOrgMembership, error) {
	var memberships []userdomain.ProviderOrgMembership
	if u.orgProvider != nil {
		out, err := u.orgProvider.ListUserOrgMemberships(ctx, providerUserID)
		if err != nil {
			return nil, err
		}
		memberships = out
	}
	if len(memberships) == 0 {
		if membership, ok := membershipFromClaims(activeProviderOrgID, claims, now); ok {
			memberships = append(memberships, membership)
		}
	}
	return activeOrgFirst(memberships, activeProviderOrgID), nil
}

func (u *UseCases) applyDefaults(input sessiondomain.ResolveInput) sessiondomain.ResolveInput {
	if strings.TrimSpace(input.PrincipalID) == "" {
		input.PrincipalID = u.defaults.PrincipalID
	}
	if strings.TrimSpace(input.Email) == "" {
		input.Email = u.defaults.PrincipalEmail
	}
	if strings.TrimSpace(input.OrgID) == "" {
		input.OrgID = u.defaults.OrgID
	}
	return input
}

func bearerToken(header string) string {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return parts[1]
	}
	return ""
}

func firstClaim(claims map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := claims[key]; ok {
			if out := claimString(value); out != "" {
				return out
			}
		}
	}
	return ""
}

func claimString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func membershipFromClaims(providerOrgID string, claims map[string]any, now time.Time) (userdomain.ProviderOrgMembership, bool) {
	providerOrgID = strings.TrimSpace(providerOrgID)
	if providerOrgID == "" {
		return userdomain.ProviderOrgMembership{}, false
	}
	return userdomain.ProviderOrgMembership{
		Org: userdomain.ProviderOrg{
			Provider:      userdomain.ProviderClerk,
			ProviderOrgID: providerOrgID,
			Name:          firstNonEmpty(firstClaim(claims, "org_name", "org_slug"), providerOrgID),
			Slug:          firstClaim(claims, "org_slug"),
			Status:        userdomain.StatusActive,
			SyncedAt:      &now,
		},
		Role: providerProductRole(firstClaim(claims, "org_role", "orgRole")),
	}, true
}

func activeOrgFirst(memberships []userdomain.ProviderOrgMembership, activeProviderOrgID string) []userdomain.ProviderOrgMembership {
	activeProviderOrgID = strings.TrimSpace(activeProviderOrgID)
	if activeProviderOrgID == "" || len(memberships) < 2 {
		return memberships
	}
	out := append([]userdomain.ProviderOrgMembership(nil), memberships...)
	for i, membership := range out {
		if strings.TrimSpace(membership.Org.ProviderOrgID) != activeProviderOrgID {
			continue
		}
		out[0], out[i] = out[i], out[0]
		return out
	}
	return out
}

func providerProductRole(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "owner", "org:owner":
		return productdomain.RoleOwner
	case "admin", "org:admin":
		return productdomain.RoleAdmin
	case "member", "org:member":
		return productdomain.RoleMember
	default:
		return productdomain.RoleMember
	}
}
