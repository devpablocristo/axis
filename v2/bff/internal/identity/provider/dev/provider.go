package dev

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity"
	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
)

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) FindUserByEmail(_ context.Context, email string) (domain.ProviderUser, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return domain.ProviderUser{}, identity.ErrProviderUserNotFound
	}
	return p.providerUser(email, email), nil
}

func (p *Provider) CreateUser(_ context.Context, email string) (domain.ProviderUser, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return domain.ProviderUser{}, identity.ErrProviderUserNotFound
	}
	return p.providerUser(email, email), nil
}

func (p *Provider) UpdateUserEmail(_ context.Context, providerUserID, email string) (domain.ProviderUser, error) {
	providerUserID = strings.TrimSpace(providerUserID)
	email = strings.TrimSpace(strings.ToLower(email))
	if providerUserID == "" || email == "" {
		return domain.ProviderUser{}, identity.ErrProviderUserNotFound
	}
	return p.providerUser(providerUserID, email), nil
}

func (p *Provider) DeleteUser(_ context.Context, _ string) error {
	return nil
}

func (p *Provider) providerUser(providerUserID, email string) domain.ProviderUser {
	now := time.Now().UTC()
	return domain.ProviderUser{
		Provider:       domain.ProviderDev,
		ProviderUserID: providerUserID,
		Email:          email,
		Status:         domain.StatusActive,
		SyncedAt:       &now,
	}
}

func (p *Provider) GetUser(_ context.Context, providerUserID string) (domain.ProviderUser, error) {
	providerUserID = strings.TrimSpace(providerUserID)
	if providerUserID == "" {
		return domain.ProviderUser{}, identity.ErrProviderUserNotFound
	}
	email := providerUserID
	if _, err := mail.ParseAddress(email); err != nil {
		email = providerUserID + "@example.local"
	}
	now := time.Now().UTC()
	return domain.ProviderUser{
		Provider:       domain.ProviderDev,
		ProviderUserID: providerUserID,
		Email:          email,
		Status:         domain.StatusActive,
		SyncedAt:       &now,
	}, nil
}

func (p *Provider) ListUserOrgMemberships(context.Context, string) ([]domain.ProviderOrgMembership, error) {
	return nil, nil
}

func (p *Provider) ListOrganizationMemberships(context.Context, string) ([]domain.ProviderOrgMembership, error) {
	return nil, nil
}

func (p *Provider) CreateOrg(_ context.Context, name string) (domain.ProviderOrg, error) {
	now := time.Now().UTC()
	name = strings.TrimSpace(name)
	if name == "" {
		name = "dev-org"
	}
	return domain.ProviderOrg{
		Provider:      domain.ProviderDev,
		ProviderOrgID: name,
		Name:          name,
		Slug:          strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Status:        domain.StatusActive,
		SyncedAt:      &now,
	}, nil
}

func (p *Provider) UpdateOrg(ctx context.Context, providerOrgID, name string) (domain.ProviderOrg, error) {
	out, err := p.CreateOrg(ctx, name)
	if err != nil {
		return domain.ProviderOrg{}, err
	}
	out.ProviderOrgID = strings.TrimSpace(providerOrgID)
	if out.ProviderOrgID == "" {
		out.ProviderOrgID = out.Name
	}
	return out, nil
}

func (p *Provider) DeleteOrg(_ context.Context, _ string) error {
	return nil
}

func (p *Provider) EnsureOrgMembership(_ context.Context, _, _, _ string) error {
	return nil
}

func (p *Provider) DeleteOrgMembership(_ context.Context, _, _ string) error {
	return nil
}

func (p *Provider) CreateOrgInvitation(_ context.Context, input identity.CreateOrgInvitationInput) (domain.ProviderInvitation, error) {
	return domain.ProviderInvitation{
		Provider:             domain.ProviderDev,
		ProviderInvitationID: input.Email,
		Email:                strings.TrimSpace(strings.ToLower(input.Email)),
		Role:                 strings.TrimSpace(strings.ToLower(input.Role)),
		Status:               domain.InvitationStatusPending,
	}, nil
}
