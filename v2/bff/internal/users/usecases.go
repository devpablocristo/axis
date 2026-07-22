package users

import (
	"context"
	"errors"

	"github.com/devpablocristo/bff-v2/internal/identity"
	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	"github.com/devpablocristo/bff-v2/internal/users/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	Get(ctx context.Context, orgID uuid.UUID, userID string) (domain.User, error)
	List(ctx context.Context, input domain.NormalizedListInput) ([]domain.User, error)
	UpsertMembership(ctx context.Context, input UpsertMembershipInput) (domain.User, error)
	UpsertInvitation(ctx context.Context, input UpsertInvitationInput) (domain.User, error)
	Update(ctx context.Context, input domain.NormalizedUpdateInput) (domain.User, error)
	Archive(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Unarchive(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Trash(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Restore(ctx context.Context, input domain.NormalizedLifecycleInput) error
	Purge(ctx context.Context, input domain.NormalizedLifecycleInput) error
	ActiveMembershipExists(ctx context.Context, input domain.NormalizedEnsureActiveInput) (bool, error)
}

type OrganizationAccessPort interface {
	ResolveOrganizationAccess(ctx context.Context, orgID, principalID string) (productdomain.Org, productdomain.OrgMember, error)
}

type IdentityPort interface {
	Get(ctx context.Context, id string) (identitydomain.User, error)
	Delete(ctx context.Context, id string) error
	EnsureProviderUser(ctx context.Context, user identitydomain.ProviderUser) (identitydomain.User, error)
}

type UseCases struct {
	repo        RepositoryPort
	products    OrganizationAccessPort
	identity    IdentityPort
	idp         identity.IdentityProviderPort
	orgProvider identity.OrgProviderPort
	invitations identity.InvitationProviderPort
	redirectURL string
}

type Options struct {
	InvitationRedirectURL string
}

type UpsertMembershipInput struct {
	OrgID  uuid.UUID
	UserID string
	Email  string
	Role   string
}

type UpsertInvitationInput struct {
	OrgID                uuid.UUID
	Provider             string
	ProviderInvitationID string
	Email                string
	Role                 string
	Status               string
}

func NewUseCases(
	repo RepositoryPort,
	products OrganizationAccessPort,
	identityUC IdentityPort,
	idp identity.IdentityProviderPort,
	orgProvider identity.OrgProviderPort,
	invitations identity.InvitationProviderPort,
	options Options,
) *UseCases {
	return &UseCases{
		repo:        repo,
		products:    products,
		identity:    identityUC,
		idp:         idp,
		orgProvider: orgProvider,
		invitations: invitations,
		redirectURL: options.InvitationRedirectURL,
	}
}

func (u *UseCases) List(ctx context.Context, input domain.ListInput) ([]domain.User, error) {
	normalized, err := domain.NormalizeListInput(input)
	if err != nil {
		return nil, err
	}
	if _, _, err := u.products.ResolveOrganizationAccess(ctx, normalized.OrgID.String(), normalized.PrincipalID); err != nil {
		return nil, err
	}
	return u.repo.List(ctx, normalized)
}

func (u *UseCases) Create(ctx context.Context, input domain.CreateInput) (domain.User, error) {
	normalized, err := domain.NormalizeCreateInput(input)
	if err != nil {
		return domain.User{}, err
	}
	org, member, err := u.requireMutator(ctx, normalized.OrgID.String(), normalized.PrincipalID, normalized.Role)
	if err != nil {
		return domain.User{}, err
	}
	if !domain.CanAssignRole(member.Role, normalized.Role) {
		return domain.User{}, domainerr.Forbidden("principal cannot assign requested role")
	}
	providerUser, err := u.idp.FindUserByEmail(ctx, normalized.Email)
	if err != nil {
		if errors.Is(err, identity.ErrProviderUserNotFound) {
			providerUser, err = u.idp.CreateUser(ctx, normalized.Email)
			if err != nil {
				return domain.User{}, err
			}
		} else {
			return domain.User{}, err
		}
	}
	axisUser, err := u.identity.EnsureProviderUser(ctx, providerUser)
	if err != nil {
		return domain.User{}, err
	}
	return u.upsertProviderProductUser(ctx, normalized.OrgID, axisUser, providerUser, org, normalized.Role)
}

func (u *UseCases) Update(ctx context.Context, input domain.UpdateInput) (domain.User, error) {
	normalized, err := domain.NormalizeUpdateInput(input)
	if err != nil {
		return domain.User{}, err
	}
	org, member, err := u.requireMutator(ctx, normalized.OrgID.String(), normalized.PrincipalID, normalized.Role)
	if err != nil {
		return domain.User{}, err
	}
	if !domain.CanAssignRole(member.Role, normalized.Role) {
		return domain.User{}, domainerr.Forbidden("principal cannot assign requested role")
	}
	if domain.KindFromID(normalized.UserID) == domain.KindUser {
		axisUser, err := u.identity.Get(ctx, normalized.UserID)
		if err != nil {
			return domain.User{}, err
		}
		if axisUser.Provider == identitydomain.ProviderClerk {
			if axisUser.Email != normalized.Email {
				providerUser, err := u.idp.UpdateUserEmail(ctx, axisUser.ProviderUserID, normalized.Email)
				if err != nil {
					return domain.User{}, err
				}
				if providerUser.Email != "" {
					normalized.Email = providerUser.Email
				}
				if _, err := u.identity.EnsureProviderUser(ctx, providerUser); err != nil {
					return domain.User{}, err
				}
			}
			if err := u.orgProvider.EnsureOrgMembership(ctx, org.ProviderOrgID, axisUser.ProviderUserID, normalized.Role); err != nil {
				return domain.User{}, err
			}
		}
	}
	return u.repo.Update(ctx, normalized)
}

func (u *UseCases) upsertProviderProductUser(
	ctx context.Context,
	orgID uuid.UUID,
	axisUser identitydomain.User,
	providerUser identitydomain.ProviderUser,
	org productdomain.Org,
	role string,
) (domain.User, error) {
	if providerUser.Provider == identitydomain.ProviderClerk {
		if err := u.orgProvider.EnsureOrgMembership(ctx, org.ProviderOrgID, providerUser.ProviderUserID, role); err != nil {
			return domain.User{}, err
		}
	}
	return u.repo.UpsertMembership(ctx, UpsertMembershipInput{
		OrgID:  orgID,
		UserID: axisUser.ID,
		Email:  axisUser.Email,
		Role:   role,
	})
}

func (u *UseCases) Archive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	return u.repo.Archive(ctx, normalized)
}

func (u *UseCases) Unarchive(ctx context.Context, input domain.LifecycleInput) error {
	normalized, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	return u.repo.Unarchive(ctx, normalized)
}

func (u *UseCases) Trash(ctx context.Context, input domain.LifecycleInput) error {
	normalized, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	return u.repo.Trash(ctx, normalized)
}

func (u *UseCases) Restore(ctx context.Context, input domain.LifecycleInput) error {
	normalized, err := u.normalizeLifecycleMutation(ctx, input)
	if err != nil {
		return err
	}
	return u.repo.Restore(ctx, normalized)
}

func (u *UseCases) Purge(ctx context.Context, input domain.LifecycleInput) error {
	normalized, org, err := u.normalizeLifecycleMutationWithOrg(ctx, input)
	if err != nil {
		return err
	}
	if domain.KindFromID(normalized.UserID) == domain.KindUser {
		user, err := u.repo.Get(ctx, normalized.OrgID, normalized.UserID)
		if err != nil {
			return err
		}
		if user.State != domain.StateTrashed {
			return domainerr.NotFound("organization user not found")
		}
		axisUser, err := u.identity.Get(ctx, normalized.UserID)
		if err != nil {
			return err
		}
		if axisUser.Provider == identitydomain.ProviderClerk {
			if err := u.orgProvider.DeleteOrgMembership(ctx, org.ProviderOrgID, axisUser.ProviderUserID); err != nil {
				return err
			}
		}
		return u.repo.Purge(ctx, normalized)
	}
	return u.repo.Purge(ctx, normalized)
}

func (u *UseCases) EnsureActive(ctx context.Context, orgID, userID string) error {
	normalized, err := domain.NormalizeEnsureActiveInput(domain.EnsureActiveInput{
		OrgID:  orgID,
		UserID: userID,
	})
	if err != nil {
		return err
	}
	exists, err := u.repo.ActiveMembershipExists(ctx, normalized)
	if err != nil {
		return err
	}
	if !exists {
		return domainerr.Validation("user_id must reference an active organization user")
	}
	return nil
}

func (u *UseCases) normalizeLifecycleMutation(ctx context.Context, input domain.LifecycleInput) (domain.NormalizedLifecycleInput, error) {
	normalized, _, err := u.normalizeLifecycleMutationWithOrg(ctx, input)
	return normalized, err
}

func (u *UseCases) normalizeLifecycleMutationWithOrg(ctx context.Context, input domain.LifecycleInput) (domain.NormalizedLifecycleInput, productdomain.Org, error) {
	normalized, err := domain.NormalizeLifecycleInput(input)
	if err != nil {
		return domain.NormalizedLifecycleInput{}, productdomain.Org{}, err
	}
	org, _, err := u.requireMutator(ctx, normalized.OrgID.String(), normalized.PrincipalID, "")
	if err != nil {
		return domain.NormalizedLifecycleInput{}, productdomain.Org{}, err
	}
	return normalized, org, nil
}

func (u *UseCases) requireMutator(ctx context.Context, orgID, principalID, _ string) (productdomain.Org, productdomain.OrgMember, error) {
	org, member, err := u.products.ResolveOrganizationAccess(ctx, orgID, principalID)
	if err != nil {
		return productdomain.Org{}, productdomain.OrgMember{}, err
	}
	if !domain.CanMutate(member.Role) {
		return productdomain.Org{}, productdomain.OrgMember{}, domainerr.Forbidden("principal cannot mutate organization users")
	}
	return org, member, nil
}
