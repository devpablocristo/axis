package rbac

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	domain "github.com/devpablocristo/nexus/governance/internal/rbac/usecases/domain"
)

type fakeRepo struct {
	createFn  func(ctx context.Context, a domain.Assignment) (domain.Assignment, error)
	getFn     func(ctx context.Context, id uuid.UUID) (domain.Assignment, error)
	listFn    func(ctx context.Context, f ListFilter) ([]domain.Assignment, error)
	checkFn   func(ctx context.Context, orgID, userID string, role domain.Role) (bool, error)
	archiveFn func(ctx context.Context, id uuid.UUID) error
	restoreFn func(ctx context.Context, id uuid.UUID) error
	deleteFn  func(ctx context.Context, id uuid.UUID) error
}

func (f *fakeRepo) Create(ctx context.Context, a domain.Assignment) (domain.Assignment, error) {
	return f.createFn(ctx, a)
}
func (f *fakeRepo) GetByID(ctx context.Context, id uuid.UUID) (domain.Assignment, error) {
	return f.getFn(ctx, id)
}
func (f *fakeRepo) List(ctx context.Context, filter ListFilter) ([]domain.Assignment, error) {
	return f.listFn(ctx, filter)
}
func (f *fakeRepo) Check(ctx context.Context, orgID, userID string, role domain.Role) (bool, error) {
	return f.checkFn(ctx, orgID, userID, role)
}
func (f *fakeRepo) Archive(ctx context.Context, id uuid.UUID) error { return f.archiveFn(ctx, id) }
func (f *fakeRepo) Restore(ctx context.Context, id uuid.UUID) error { return f.restoreFn(ctx, id) }
func (f *fakeRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return f.deleteFn(ctx, id)
}

func TestUsecases_Grant(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		input     domain.Assignment
		repoCreat func(ctx context.Context, a domain.Assignment) (domain.Assignment, error)
		wantErr   func(error) bool
	}{
		{
			name:    "happy path",
			input:   domain.Assignment{OrgID: "org-1", UserID: "user-1", Role: domain.RolePolicyAdmin},
			repoCreat: func(_ context.Context, a domain.Assignment) (domain.Assignment, error) {
				a.ID = uuid.New()
				a.GrantedAt = time.Now().UTC()
				return a, nil
			},
			wantErr: nil,
		},
		{
			name:    "missing org_id",
			input:   domain.Assignment{UserID: "user-1", Role: domain.RoleApprover},
			wantErr: domainerr.IsValidation,
		},
		{
			name:    "missing user_id",
			input:   domain.Assignment{OrgID: "org-1", Role: domain.RoleApprover},
			wantErr: domainerr.IsValidation,
		},
		{
			name:    "invalid role",
			input:   domain.Assignment{OrgID: "org-1", UserID: "user-1", Role: domain.Role("bogus")},
			wantErr: domainerr.IsValidation,
		},
		{
			name:  "repo conflict bubbles up",
			input: domain.Assignment{OrgID: "org-1", UserID: "user-1", Role: domain.RoleAuditor},
			repoCreat: func(_ context.Context, _ domain.Assignment) (domain.Assignment, error) {
				return domain.Assignment{}, ErrAlreadyExists
			},
			wantErr: domainerr.IsConflict,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeRepo{createFn: tc.repoCreat}
			uc := NewUsecases(repo)
			_, err := uc.Grant(context.Background(), tc.input)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr(err) {
				t.Fatalf("error did not match expectation: %v", err)
			}
		})
	}
}

func TestUsecases_Check(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		orgID       string
		userID      string
		role        domain.Role
		repoResult  bool
		wantErr     bool
		wantGranted bool
	}{
		{name: "granted", orgID: "org-1", userID: "user-1", role: domain.RoleApprover, repoResult: true, wantGranted: true},
		{name: "not granted", orgID: "org-1", userID: "user-1", role: domain.RoleApprover, repoResult: false, wantGranted: false},
		{name: "missing org", userID: "user-1", role: domain.RoleApprover, wantErr: true},
		{name: "missing user", orgID: "org-1", role: domain.RoleApprover, wantErr: true},
		{name: "invalid role", orgID: "org-1", userID: "user-1", role: domain.Role(""), wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeRepo{
				checkFn: func(_ context.Context, _, _ string, _ domain.Role) (bool, error) {
					return tc.repoResult, nil
				},
			}
			uc := NewUsecases(repo)
			got, err := uc.Check(context.Background(), tc.orgID, tc.userID, tc.role)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantGranted {
				t.Fatalf("expected granted=%v, got %v", tc.wantGranted, got)
			}
		})
	}
}

func TestUsecases_Revoke_NotFound(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{
		archiveFn: func(_ context.Context, _ uuid.UUID) error { return ErrNotFound },
	}
	uc := NewUsecases(repo)
	err := uc.Revoke(context.Background(), uuid.New())
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUsecases_List_InvalidRoleFilter(t *testing.T) {
	t.Parallel()
	uc := NewUsecases(&fakeRepo{})
	_, err := uc.List(context.Background(), ListFilter{Role: "bogus"})
	if err == nil || !domainerr.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
