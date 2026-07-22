package gateway

import (
	"context"
	"net/url"
	"strings"

	gatewaydomain "github.com/devpablocristo/bff-v2/internal/gateway/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
)

type OrganizationAccessPort interface {
	ResolveOrgAccess(ctx context.Context, orgID, productSurface, principalID string) (productdomain.Product, productdomain.OrgMember, error)
}

type UseCases struct {
	products         OrganizationAccessPort
	companionBaseURL *url.URL
	nexusBaseURL     *url.URL
}

func NewUseCases(products OrganizationAccessPort, companionBaseURL string, nexusBaseURL string) (*UseCases, error) {
	companionURL, err := parseBaseURL(companionBaseURL)
	if err != nil {
		return nil, err
	}
	nexusURL, err := parseBaseURL(nexusBaseURL)
	if err != nil {
		return nil, err
	}
	return &UseCases{products: products, companionBaseURL: companionURL, nexusBaseURL: nexusURL}, nil
}

func (u *UseCases) Resolve(ctx context.Context, input gatewaydomain.ResolveInput) (gatewaydomain.ResolvedContext, error) {
	normalized, err := gatewaydomain.NormalizeResolveInput(input)
	if err != nil {
		return gatewaydomain.ResolvedContext{}, err
	}
	product, member, err := u.products.ResolveOrgAccess(ctx, normalized.OrgID, normalized.ProductSurface, normalized.PrincipalID)
	if err != nil {
		return gatewaydomain.ResolvedContext{}, err
	}
	return gatewaydomain.ResolvedContext{
		PrincipalID:    member.UserID,
		OrgID:          product.OrgID,
		ProductSurface: product.ProductSurface,
		MembershipRole: member.Role,
		Product:        product,
		Member:         member,
	}, nil
}

func (u *UseCases) TargetURL(requestPath, rawQuery string) string {
	return targetURL(u.companionBaseURL, requestPath, rawQuery)
}

func (u *UseCases) NexusTargetURL(requestPath, rawQuery string) string {
	return targetURL(u.nexusBaseURL, requestPath, rawQuery)
}

func parseBaseURL(value string) (*url.URL, error) {
	return url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
}

func targetURL(baseURL *url.URL, requestPath, rawQuery string) string {
	target := *baseURL
	appPath := strings.TrimPrefix(requestPath, "/api")
	if appPath == requestPath {
		appPath = requestPath
	}
	if appPath == "" || appPath == "/" {
		appPath = "/"
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/v1" + appPath
	target.RawQuery = rawQuery
	return target.String()
}
