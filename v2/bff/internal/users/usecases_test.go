package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devpablocristo/bff-v2/internal/identity"
	identitydomain "github.com/devpablocristo/bff-v2/internal/identity/usecases/domain"
	productdomain "github.com/devpablocristo/bff-v2/internal/products/usecases/domain"
	userdomain "github.com/devpablocristo/bff-v2/internal/users/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestUsersListAllowsAnyActiveMember(t *testing.T) {
	orgID := uuid.New()
	repo := newFakeUsersRepo(orgID)
	uc := newUsersUC(repo, &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleMember})

	out, err := uc.List(context.Background(), userdomain.ListInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 0 || repo.lastListState != userdomain.StateActive {
		t.Fatalf("unexpected list output=%+v state=%q", out, repo.lastListState)
	}
}

func TestUsersCreateRequiresAdminOrOwner(t *testing.T) {
	orgID := uuid.New()
	uc := newUsersUC(newFakeUsersRepo(orgID), &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleMember})

	_, err := uc.Create(context.Background(), userdomain.CreateInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
		Email:       "user@example.com",
		Role:        userdomain.RoleMember,
	})
	if !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden for member mutation, got %v", err)
	}
}

func TestUsersAdminCannotAssignOwner(t *testing.T) {
	orgID := uuid.New()
	uc := newUsersUC(newFakeUsersRepo(orgID), &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleAdmin})

	_, err := uc.Create(context.Background(), userdomain.CreateInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
		Email:       "owner@example.com",
		Role:        userdomain.RoleOwner,
	})
	if !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden assigning owner, got %v", err)
	}
}

func TestUsersOwnerCanAssignOwner(t *testing.T) {
	orgID := uuid.New()
	uc := newUsersUC(newFakeUsersRepo(orgID), &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleOwner})

	out, err := uc.Create(context.Background(), userdomain.CreateInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
		Email:       "owner@example.com",
		Role:        userdomain.RoleOwner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Role != userdomain.RoleOwner {
		t.Fatalf("expected owner role, got %+v", out)
	}
}

func TestUsersEnsureActiveRequiresActiveMembership(t *testing.T) {
	orgID := uuid.New()
	repo := newFakeUsersRepo(orgID)
	activeUserID := uuid.NewString()
	repo.active[activeUserID] = true
	uc := newUsersUC(repo, &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleAdmin})

	if err := uc.EnsureActive(context.Background(), orgID.String(), activeUserID); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := uc.EnsureActive(context.Background(), orgID.String(), uuid.NewString()); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing active membership, got %v", err)
	}
}

func TestUsersUnknownProviderEmailCreatesProviderUserMembership(t *testing.T) {
	orgID := uuid.New()
	repo := newFakeUsersRepo(orgID)
	provider := &fakeUsersProvider{missing: true}
	uc := newUsersUCWithProvider(repo, &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleOwner}, provider)

	out, err := uc.Create(context.Background(), userdomain.CreateInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
		Email:       "unknown@example.com",
		Role:        userdomain.RoleMember,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Kind != userdomain.KindUser || out.State != userdomain.StateActive {
		t.Fatalf("expected active organization user, got %+v", out)
	}
	if out.Email != "unknown@example.com" {
		t.Fatalf("expected created provider user email, got %+v", out)
	}
	if !provider.created {
		t.Fatalf("expected provider user creation")
	}
}

func TestUsersUpdateSyncsClerkEmailAndMembership(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.NewString()
	repo := newFakeUsersRepo(orgID)
	repo.rows[userID] = userdomain.User{
		ID:    userID,
		Kind:  userdomain.KindUser,
		Email: "old@example.com",
		Role:  userdomain.RoleMember,
		OrgID: orgID,
		State: userdomain.StateActive,
	}
	repo.active[userID] = true
	identityUC := &fakeUsersIdentity{users: map[string]identitydomain.User{
		userID: {
			ID:             userID,
			Provider:       identitydomain.ProviderClerk,
			ProviderUserID: "user_clerk",
			Email:          "old@example.com",
			Status:         identitydomain.StatusActive,
		},
	}}
	provider := &fakeUsersProvider{}
	uc := NewUseCases(repo, &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleOwner}, identityUC, provider, provider, provider, Options{})

	out, err := uc.Update(context.Background(), userdomain.UpdateInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
		UserID:      userID,
		Email:       "new@example.com",
		Role:        userdomain.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Email != "new@example.com" || out.Role != userdomain.RoleAdmin {
		t.Fatalf("unexpected update output: %+v", out)
	}
	if provider.updatedProviderUserID != "user_clerk" || provider.updatedEmail != "new@example.com" {
		t.Fatalf("expected Clerk email update, provider=%+v", provider)
	}
	if !provider.ensureMembershipCalled {
		t.Fatalf("expected Clerk membership role sync")
	}
}

func TestUsersPurgeDeletesClerkMembershipOnly(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.NewString()
	repo := newFakeUsersRepo(orgID)
	repo.rows[userID] = userdomain.User{
		ID:    userID,
		Kind:  userdomain.KindUser,
		Email: "delete@example.com",
		Role:  userdomain.RoleMember,
		OrgID: orgID,
		State: userdomain.StateTrashed,
	}
	identityUC := &fakeUsersIdentity{users: map[string]identitydomain.User{
		userID: {
			ID:             userID,
			Provider:       identitydomain.ProviderClerk,
			ProviderUserID: "user_clerk_delete",
			Email:          "delete@example.com",
			Status:         identitydomain.StatusActive,
		},
	}}
	provider := &fakeUsersProvider{}
	uc := NewUseCases(repo, &fakeUsersOrganizationAccess{orgID: orgID, role: productdomain.RoleOwner}, identityUC, provider, provider, provider, Options{})

	if err := uc.Purge(context.Background(), userdomain.LifecycleInput{
		OrgID:       orgID.String(),
		PrincipalID: "principal",
		UserID:      userID,
	}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if provider.deletedMembershipUserID != "user_clerk_delete" {
		t.Fatalf("expected Clerk membership delete, provider=%+v", provider)
	}
	if provider.deletedProviderUserID != "" {
		t.Fatalf("must not delete global Clerk user, provider=%+v", provider)
	}
	if identityUC.deletedID != "" {
		t.Fatalf("must not delete local identity mirror, got %q", identityUC.deletedID)
	}
	if _, ok := repo.rows[userID]; ok {
		t.Fatalf("expected organization user membership purged")
	}
}

func newUsersUC(repo *fakeUsersRepo, products *fakeUsersOrganizationAccess) *UseCases {
	return newUsersUCWithProvider(repo, products, &fakeUsersProvider{})
}

func newUsersUCWithProvider(repo *fakeUsersRepo, products *fakeUsersOrganizationAccess, provider *fakeUsersProvider) *UseCases {
	identityUC := &fakeUsersIdentity{}
	return NewUseCases(repo, products, identityUC, provider, provider, provider, Options{})
}

type fakeUsersRepo struct {
	orgID         uuid.UUID
	rows          map[string]userdomain.User
	active        map[string]bool
	lastListState string
}

func newFakeUsersRepo(orgID uuid.UUID) *fakeUsersRepo {
	return &fakeUsersRepo{
		orgID:  orgID,
		rows:   map[string]userdomain.User{},
		active: map[string]bool{},
	}
}

func (r *fakeUsersRepo) List(_ context.Context, input userdomain.NormalizedListInput) ([]userdomain.User, error) {
	r.lastListState = input.State
	out := []userdomain.User{}
	for _, row := range r.rows {
		if row.State == input.State {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeUsersRepo) Get(_ context.Context, orgID uuid.UUID, userID string) (userdomain.User, error) {
	row, ok := r.rows[userID]
	if !ok || row.OrgID != orgID {
		return userdomain.User{}, domainerr.NotFound("organization user not found")
	}
	return row, nil
}

func (r *fakeUsersRepo) UpsertMembership(_ context.Context, input UpsertMembershipInput) (userdomain.User, error) {
	now := time.Now().UTC()
	user := userdomain.User{
		ID:        input.UserID,
		Kind:      userdomain.KindUser,
		Email:     input.Email,
		Role:      input.Role,
		OrgID:     input.OrgID,
		State:     userdomain.StateActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.rows[user.ID] = user
	r.active[user.ID] = true
	return user, nil
}

func (r *fakeUsersRepo) UpsertInvitation(_ context.Context, input UpsertInvitationInput) (userdomain.User, error) {
	now := time.Now().UTC()
	user := userdomain.User{
		ID:        "invitation:" + uuid.NewString(),
		Kind:      userdomain.KindInvitation,
		Email:     input.Email,
		Role:      input.Role,
		OrgID:     input.OrgID,
		State:     userdomain.StatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.rows[user.ID] = user
	return user, nil
}

func (r *fakeUsersRepo) Update(_ context.Context, input userdomain.NormalizedUpdateInput) (userdomain.User, error) {
	row, ok := r.rows[input.UserID]
	if !ok {
		return userdomain.User{}, domainerr.NotFound("organization user not found")
	}
	row.Email = input.Email
	row.Role = input.Role
	row.UpdatedAt = time.Now().UTC()
	r.rows[row.ID] = row
	return row, nil
}

func (r *fakeUsersRepo) Archive(_ context.Context, input userdomain.NormalizedLifecycleInput) error {
	return r.setState(input.UserID, userdomain.StateArchived)
}

func (r *fakeUsersRepo) Unarchive(_ context.Context, input userdomain.NormalizedLifecycleInput) error {
	return r.setState(input.UserID, userdomain.StateActive)
}

func (r *fakeUsersRepo) Trash(_ context.Context, input userdomain.NormalizedLifecycleInput) error {
	return r.setState(input.UserID, userdomain.StateTrashed)
}

func (r *fakeUsersRepo) Restore(_ context.Context, input userdomain.NormalizedLifecycleInput) error {
	return r.setState(input.UserID, userdomain.StateActive)
}

func (r *fakeUsersRepo) Purge(_ context.Context, input userdomain.NormalizedLifecycleInput) error {
	delete(r.rows, input.UserID)
	delete(r.active, input.UserID)
	return nil
}

func (r *fakeUsersRepo) ActiveMembershipExists(_ context.Context, input userdomain.NormalizedEnsureActiveInput) (bool, error) {
	return input.OrgID == r.orgID && r.active[input.UserID], nil
}

func (r *fakeUsersRepo) setState(userID, state string) error {
	row, ok := r.rows[userID]
	if !ok {
		return domainerr.NotFound("organization user not found")
	}
	row.State = state
	r.rows[userID] = row
	r.active[userID] = state == userdomain.StateActive
	return nil
}

type fakeUsersOrganizationAccess struct {
	orgID uuid.UUID
	role  string
	err   error
}

func (f *fakeUsersOrganizationAccess) ResolveOrganizationAccess(context.Context, string, string) (productdomain.Org, productdomain.OrgMember, error) {
	if f.err != nil {
		return productdomain.Org{}, productdomain.OrgMember{}, f.err
	}
	return productdomain.Org{
			ID:            f.orgID.String(),
			Provider:      identitydomain.ProviderDev,
			ProviderOrgID: "dev-org",
			Name:          "Dev Org",
			Status:        productdomain.StatusActive,
		}, productdomain.OrgMember{
			OrgID:  f.orgID,
			UserID: uuid.NewString(),
			Role:   f.role,
			Status: productdomain.StatusActive,
		}, nil
}

type fakeUsersIdentity struct {
	users     map[string]identitydomain.User
	deletedID string
}

func (f *fakeUsersIdentity) EnsureProviderUser(_ context.Context, user identitydomain.ProviderUser) (identitydomain.User, error) {
	if f.users == nil {
		f.users = map[string]identitydomain.User{}
	}
	for id, existing := range f.users {
		if existing.Provider == user.Provider && existing.ProviderUserID == user.ProviderUserID {
			existing.Email = user.Email
			existing.Status = identitydomain.StatusActive
			existing.UpdatedAt = time.Now().UTC()
			f.users[id] = existing
			return existing, nil
		}
	}
	now := time.Now().UTC()
	out := identitydomain.User{
		ID:             uuid.NewString(),
		Provider:       user.Provider,
		ProviderUserID: user.ProviderUserID,
		Email:          user.Email,
		Status:         identitydomain.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	f.users[out.ID] = out
	return out, nil
}

func (f *fakeUsersIdentity) Get(_ context.Context, id string) (identitydomain.User, error) {
	if f.users != nil {
		if user, ok := f.users[id]; ok {
			return user, nil
		}
	}
	return identitydomain.User{ID: id, Provider: identitydomain.ProviderDev, ProviderUserID: id}, nil
}

func (f *fakeUsersIdentity) Delete(_ context.Context, id string) error {
	f.deletedID = id
	delete(f.users, id)
	return nil
}

type fakeUsersProvider struct {
	missing                 bool
	created                 bool
	findCalled              bool
	ensureMembershipCalled  bool
	updatedProviderUserID   string
	updatedEmail            string
	deletedMembershipUserID string
	deletedProviderUserID   string
}

func (f *fakeUsersProvider) FindUserByEmail(_ context.Context, email string) (identitydomain.ProviderUser, error) {
	f.findCalled = true
	if f.missing {
		return identitydomain.ProviderUser{}, identity.ErrProviderUserNotFound
	}
	return identitydomain.ProviderUser{
		Provider:       identitydomain.ProviderDev,
		ProviderUserID: email,
		Email:          email,
		Status:         identitydomain.StatusActive,
	}, nil
}

func (f *fakeUsersProvider) CreateUser(_ context.Context, email string) (identitydomain.ProviderUser, error) {
	f.created = true
	return identitydomain.ProviderUser{
		Provider:       identitydomain.ProviderDev,
		ProviderUserID: "created:" + email,
		Email:          email,
		Status:         identitydomain.StatusActive,
	}, nil
}

func (f *fakeUsersProvider) UpdateUserEmail(_ context.Context, providerUserID, email string) (identitydomain.ProviderUser, error) {
	f.updatedProviderUserID = providerUserID
	f.updatedEmail = email
	return identitydomain.ProviderUser{
		Provider:       identitydomain.ProviderClerk,
		ProviderUserID: providerUserID,
		Email:          email,
		Status:         identitydomain.StatusActive,
	}, nil
}

func (f *fakeUsersProvider) DeleteUser(_ context.Context, providerUserID string) error {
	f.deletedProviderUserID = providerUserID
	return nil
}

func (f *fakeUsersProvider) GetUser(context.Context, string) (identitydomain.ProviderUser, error) {
	return identitydomain.ProviderUser{}, errors.New("not implemented")
}

func (f *fakeUsersProvider) ListUserOrgMemberships(context.Context, string) ([]identitydomain.ProviderOrgMembership, error) {
	return nil, nil
}

func (f *fakeUsersProvider) CreateOrg(context.Context, string) (identitydomain.ProviderOrg, error) {
	return identitydomain.ProviderOrg{}, errors.New("not implemented")
}

func (f *fakeUsersProvider) UpdateOrg(context.Context, string, string) (identitydomain.ProviderOrg, error) {
	return identitydomain.ProviderOrg{}, errors.New("not implemented")
}

func (f *fakeUsersProvider) DeleteOrg(context.Context, string) error {
	return errors.New("not implemented")
}

func (f *fakeUsersProvider) EnsureOrgMembership(context.Context, string, string, string) error {
	f.ensureMembershipCalled = true
	return nil
}

func (f *fakeUsersProvider) DeleteOrgMembership(_ context.Context, _, providerUserID string) error {
	f.deletedMembershipUserID = providerUserID
	return nil
}

func (f *fakeUsersProvider) CreateOrgInvitation(_ context.Context, input identity.CreateOrgInvitationInput) (identitydomain.ProviderInvitation, error) {
	return identitydomain.ProviderInvitation{
		Provider:             identitydomain.ProviderDev,
		ProviderInvitationID: "invite_" + input.Email,
		Email:                input.Email,
		Role:                 input.Role,
		Status:               identitydomain.InvitationStatusPending,
	}, nil
}
