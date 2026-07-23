package clerk

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity"
	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	clerkapi "github.com/devpablocristo/platform/sdks/clerk/go"
)

type Config struct {
	SecretKey         string
	BaseURL           string
	InviteRedirectURL string
	Client            *http.Client
}

type Provider struct {
	client            *clerkapi.Client
	inviteRedirectURL string
}

func NewProvider(config Config) *Provider {
	return &Provider{
		client: clerkapi.New(clerkapi.Config{
			SecretKey: config.SecretKey,
			BaseURL:   config.BaseURL,
			Client:    config.Client,
		}),
		inviteRedirectURL: strings.TrimSpace(config.InviteRedirectURL),
	}
}

func (p *Provider) FindUserByEmail(ctx context.Context, email string) (domain.ProviderUser, error) {
	user, ok, err := p.client.FindUserByEmail(ctx, email)
	if err != nil {
		return domain.ProviderUser{}, mapClerkError(err)
	}
	if !ok {
		return domain.ProviderUser{}, identity.ErrProviderUserNotFound
	}
	return providerUser(user), nil
}

func (p *Provider) CreateUser(ctx context.Context, email string) (domain.ProviderUser, error) {
	user, err := p.client.CreateUser(ctx, clerkapi.CreateUserInput{Email: email})
	if err != nil {
		return domain.ProviderUser{}, mapClerkError(err)
	}
	return providerUser(user), nil
}

func (p *Provider) UpdateUserEmail(ctx context.Context, providerUserID, email string) (domain.ProviderUser, error) {
	user, err := p.client.UpdateUserEmail(ctx, providerUserID, email)
	if err != nil {
		return domain.ProviderUser{}, mapClerkError(err)
	}
	return providerUser(user), nil
}

func (p *Provider) DeleteUser(ctx context.Context, providerUserID string) error {
	if err := p.client.DeleteUser(ctx, providerUserID); err != nil {
		return mapClerkError(err)
	}
	return nil
}

func (p *Provider) GetUser(ctx context.Context, providerUserID string) (domain.ProviderUser, error) {
	user, err := p.client.GetUser(ctx, providerUserID)
	if err != nil {
		if clerkapi.IsNotFound(err) {
			return domain.ProviderUser{}, identity.ErrProviderUserNotFound
		}
		return domain.ProviderUser{}, mapClerkError(err)
	}
	return providerUser(user), nil
}

func (p *Provider) CreateOrg(ctx context.Context, name string) (domain.ProviderOrg, error) {
	org, err := p.client.CreateOrganization(ctx, clerkapi.OrganizationInput{Name: name})
	if err != nil {
		return domain.ProviderOrg{}, mapClerkError(err)
	}
	return providerOrg(org), nil
}

func (p *Provider) UpdateOrg(ctx context.Context, providerOrgID, name string) (domain.ProviderOrg, error) {
	org, err := p.client.UpdateOrganization(ctx, providerOrgID, clerkapi.OrganizationInput{Name: name})
	if err != nil {
		return domain.ProviderOrg{}, mapClerkError(err)
	}
	return providerOrg(org), nil
}

func (p *Provider) DeleteOrg(ctx context.Context, providerOrgID string) error {
	if err := p.client.DeleteOrganization(ctx, providerOrgID); err != nil {
		return mapClerkError(err)
	}
	return nil
}

func (p *Provider) ListUserOrgMemberships(ctx context.Context, providerUserID string) ([]domain.ProviderOrgMembership, error) {
	items, err := p.client.ListUserOrgMemberships(ctx, providerUserID)
	if err != nil {
		return nil, mapClerkError(err)
	}
	out := make([]domain.ProviderOrgMembership, 0, len(items))
	for _, item := range items {
		membership := providerOrgMembership(item)
		if membership.Org.ProviderOrgID == "" {
			continue
		}
		out = append(out, membership)
	}
	return out, nil
}

func (p *Provider) ListOrganizationMemberships(ctx context.Context, providerOrgID string) ([]domain.ProviderOrgMembership, error) {
	items, err := p.client.ListOrganizationMemberships(ctx, providerOrgID)
	if err != nil {
		return nil, mapClerkError(err)
	}
	out := make([]domain.ProviderOrgMembership, 0, len(items))
	for _, item := range items {
		membership := providerOrgMembership(item)
		if membership.Org.ProviderOrgID == "" || membership.User.ProviderUserID == "" {
			continue
		}
		out = append(out, membership)
	}
	return out, nil
}

func (p *Provider) EnsureOrgMembership(ctx context.Context, providerOrgID, providerUserID, role string) error {
	input := clerkapi.OrgMembershipInput{
		ProviderOrgID:  providerOrgID,
		ProviderUserID: providerUserID,
		Role:           clerkRole(role),
	}
	err := p.client.CreateOrgMembership(ctx, input)
	if err == nil {
		return nil
	}
	switch clerkapi.StatusCode(err) {
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return mapClerkError(p.client.UpdateOrgMembership(ctx, input))
	case http.StatusBadRequest:
		if isAlreadyMemberError(err) {
			return mapClerkError(p.client.UpdateOrgMembership(ctx, input))
		}
	}
	return mapClerkError(err)
}

func (p *Provider) DeleteOrgMembership(ctx context.Context, providerOrgID, providerUserID string) error {
	if err := p.client.DeleteOrgMembership(ctx, providerOrgID, providerUserID); err != nil {
		if clerkapi.IsNotFound(err) {
			return nil
		}
		return mapClerkError(err)
	}
	return nil
}

func (p *Provider) CreateOrgInvitation(ctx context.Context, input identity.CreateOrgInvitationInput) (domain.ProviderInvitation, error) {
	redirectURL := firstNonEmpty(input.RedirectURL, p.inviteRedirectURL)
	invitation, err := p.client.CreateOrgInvitation(ctx, clerkapi.OrgInvitationInput{
		ProviderOrgID:         input.ProviderOrgID,
		Email:                 input.Email,
		Role:                  clerkRole(input.Role),
		InviterProviderUserID: input.InviterProviderUserID,
		RedirectURL:           redirectURL,
	})
	if err != nil {
		return domain.ProviderInvitation{}, mapClerkError(err)
	}
	status := firstNonEmpty(invitation.Status, domain.InvitationStatusPending)
	return domain.ProviderInvitation{
		Provider:             domain.ProviderClerk,
		ProviderInvitationID: invitation.ID,
		Email:                strings.TrimSpace(strings.ToLower(input.Email)),
		Role:                 strings.TrimSpace(strings.ToLower(input.Role)),
		Status:               status,
	}, nil
}

func providerUser(user clerkapi.User) domain.ProviderUser {
	now := time.Now().UTC()
	return domain.ProviderUser{
		Provider:       domain.ProviderClerk,
		ProviderUserID: user.ID,
		Email:          user.Email,
		Status:         domain.StatusActive,
		SyncedAt:       &now,
	}
}

func providerOrg(org clerkapi.Organization) domain.ProviderOrg {
	now := time.Now().UTC()
	name := firstNonEmpty(org.Name, org.Slug, org.ID)
	return domain.ProviderOrg{
		Provider:      domain.ProviderClerk,
		ProviderOrgID: org.ID,
		Name:          name,
		Slug:          org.Slug,
		Status:        domain.StatusActive,
		SyncedAt:      &now,
	}
}

func providerOrgMembership(item clerkapi.OrganizationMembership) domain.ProviderOrgMembership {
	org := item.Organization
	if org.ID == "" {
		org.ID = item.OrganizationID
	}
	return domain.ProviderOrgMembership{
		Org:  providerOrg(org),
		Role: axisRole(item.Role),
		User: providerUser(item.User),
	}
}

func clerkRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "owner", "admin":
		return "org:admin"
	default:
		return "org:member"
	}
}

func axisRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "org:admin", "admin", "owner":
		return "admin"
	default:
		return "member"
	}
}

func isAlreadyMemberError(err error) bool {
	var apiErr *clerkapi.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	body := strings.ToLower(apiErr.Body)
	return strings.Contains(body, "already") && strings.Contains(body, "member")
}

func mapClerkError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *clerkapi.APIError
	if !errors.As(err, &apiErr) {
		return domainerr.Unavailable("clerk is unavailable")
	}
	switch apiErr.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return domainerr.Validation(apiErr.Message("clerk rejected request"))
	case http.StatusUnauthorized:
		return domainerr.Unauthorized("clerk authentication failed")
	case http.StatusForbidden:
		return domainerr.Forbidden("clerk authorization failed")
	case http.StatusNotFound:
		return domainerr.NotFound("clerk resource not found")
	case http.StatusConflict:
		return domainerr.Conflict("clerk resource already exists")
	case http.StatusTooManyRequests:
		return domainerr.Unavailable("clerk rate limit exceeded")
	default:
		if apiErr.StatusCode >= 500 {
			return domainerr.UpstreamError("clerk upstream error")
		}
		return err
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

var _ identity.IdentityProviderPort = (*Provider)(nil)
var _ identity.OrgProviderPort = (*Provider)(nil)
var _ identity.InvitationProviderPort = (*Provider)(nil)
