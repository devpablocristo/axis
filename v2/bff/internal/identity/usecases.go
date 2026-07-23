package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

var ErrProviderUserNotFound = errors.New("provider user not found")

type RepositoryPort interface {
	Ensure(ctx context.Context, input domain.EnsureInput) (domain.User, error)
	Get(ctx context.Context, id string) (domain.User, error)
	Delete(ctx context.Context, id string) error
	FindByProviderUserID(ctx context.Context, provider, providerUserID string) (domain.User, error)
	FindByEmail(ctx context.Context, email string) (domain.User, error)
	MarkDeletedByProviderUserID(ctx context.Context, provider, providerUserID string) error
}

type IdentityProviderPort interface {
	FindUserByEmail(ctx context.Context, email string) (domain.ProviderUser, error)
	CreateUser(ctx context.Context, email string) (domain.ProviderUser, error)
	UpdateUserEmail(ctx context.Context, providerUserID, email string) (domain.ProviderUser, error)
	DeleteUser(ctx context.Context, providerUserID string) error
	GetUser(ctx context.Context, providerUserID string) (domain.ProviderUser, error)
}

type OrgProviderPort interface {
	CreateOrg(ctx context.Context, name string) (domain.ProviderOrg, error)
	UpdateOrg(ctx context.Context, providerOrgID, name string) (domain.ProviderOrg, error)
	DeleteOrg(ctx context.Context, providerOrgID string) error
	ListUserOrgMemberships(ctx context.Context, providerUserID string) ([]domain.ProviderOrgMembership, error)
	ListOrganizationMemberships(ctx context.Context, providerOrgID string) ([]domain.ProviderOrgMembership, error)
	EnsureOrgMembership(ctx context.Context, providerOrgID, providerUserID, role string) error
	DeleteOrgMembership(ctx context.Context, providerOrgID, providerUserID string) error
}

type InvitationProviderPort interface {
	CreateOrgInvitation(ctx context.Context, input CreateOrgInvitationInput) (domain.ProviderInvitation, error)
}

type CreateOrgInvitationInput struct {
	ProviderOrgID         string
	Email                 string
	Role                  string
	InviterProviderUserID string
	RedirectURL           string
}

type UseCases struct {
	repo RepositoryPort
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo}
}

func (u *UseCases) Ensure(ctx context.Context, input domain.EnsureInput) (domain.User, error) {
	normalized, err := domain.NormalizeEnsureInput(input)
	if err != nil {
		return domain.User{}, err
	}
	return u.repo.Ensure(ctx, normalized)
}

func (u *UseCases) Get(ctx context.Context, id string) (domain.User, error) {
	return u.repo.Get(ctx, id)
}

func (u *UseCases) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if _, err := uuid.Parse(id); err != nil {
		return domainerr.Validation("axis_user_id must be a valid UUID")
	}
	return u.repo.Delete(ctx, id)
}

func (u *UseCases) FindByProviderUserID(ctx context.Context, provider, providerUserID string) (domain.User, error) {
	return u.repo.FindByProviderUserID(ctx, domain.NormalizeProvider(provider), providerUserID)
}

func (u *UseCases) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	return u.repo.FindByEmail(ctx, email)
}

func (u *UseCases) EnsureProviderUser(ctx context.Context, user domain.ProviderUser) (domain.User, error) {
	return u.Ensure(ctx, domain.ProviderUserEnsureInput(user))
}

func (u *UseCases) MarkDeletedByProviderUserID(ctx context.Context, provider, providerUserID string) error {
	provider = domain.NormalizeProvider(provider)
	providerUserID = strings.TrimSpace(providerUserID)
	if providerUserID == "" {
		return domainerr.Validation("provider_user_id is required")
	}
	err := u.repo.MarkDeletedByProviderUserID(ctx, provider, providerUserID)
	if domainerr.IsNotFound(err) {
		return nil
	}
	return err
}
