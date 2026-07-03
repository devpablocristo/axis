package domain

import (
	"net/mail"
	"strings"
	"time"

	tenantdomain "github.com/devpablocristo/bff-v2/internal/tenancy/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StateActive   = "active"
	StateArchived = "archived"
	StateTrashed  = "trashed"
	StatePending  = "pending"

	KindUser       = "user"
	KindInvitation = "invitation"

	RoleOwner  = tenantdomain.RoleOwner
	RoleAdmin  = tenantdomain.RoleAdmin
	RoleMember = tenantdomain.RoleMember
)

type User struct {
	ID       string
	Kind     string
	Email    string
	Role     string
	TenantID uuid.UUID
	State    string

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type ListInput struct {
	TenantID    string
	PrincipalID string
	State       string
}

type CreateInput struct {
	TenantID    string
	PrincipalID string
	Email       string
	Role        string
}

type UpdateInput struct {
	TenantID    string
	PrincipalID string
	UserID      string
	Email       string
	Role        string
}

type LifecycleInput struct {
	TenantID    string
	PrincipalID string
	UserID      string
}

type EnsureActiveInput struct {
	TenantID string
	UserID   string
}

type NormalizedListInput struct {
	TenantID    uuid.UUID
	PrincipalID string
	State       string
}

type NormalizedCreateInput struct {
	TenantID    uuid.UUID
	PrincipalID string
	Email       string
	Role        string
}

type NormalizedUpdateInput struct {
	TenantID    uuid.UUID
	PrincipalID string
	UserID      string
	Email       string
	Role        string
}

type NormalizedLifecycleInput struct {
	TenantID    uuid.UUID
	PrincipalID string
	UserID      string
}

type NormalizedEnsureActiveInput struct {
	TenantID uuid.UUID
	UserID   string
}

func NormalizeListInput(in ListInput) (NormalizedListInput, error) {
	tenantID, err := tenantdomain.ParseTenantID(in.TenantID)
	if err != nil {
		return NormalizedListInput{}, err
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedListInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedListInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
		State:       NormalizeState(in.State),
	}, nil
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	tenantID, principalID, err := normalizeAccess(in.TenantID, in.PrincipalID)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	role, err := NormalizeRole(in.Role)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	return NormalizedCreateInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
		Email:       email,
		Role:        role,
	}, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	tenantID, principalID, err := normalizeAccess(in.TenantID, in.PrincipalID)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("user_id is required")
	}
	if !isInvitationID(userID) {
		parsed, err := uuid.Parse(userID)
		if err != nil || parsed == uuid.Nil {
			return NormalizedUpdateInput{}, domainerr.Validation("user_id must be a valid UUID")
		}
		userID = parsed.String()
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	role, err := NormalizeRole(in.Role)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	return NormalizedUpdateInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
		UserID:      userID,
		Email:       email,
		Role:        role,
	}, nil
}

func NormalizeLifecycleInput(in LifecycleInput) (NormalizedLifecycleInput, error) {
	tenantID, principalID, err := normalizeAccess(in.TenantID, in.PrincipalID)
	if err != nil {
		return NormalizedLifecycleInput{}, err
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		return NormalizedLifecycleInput{}, domainerr.Validation("user_id is required")
	}
	if !isInvitationID(userID) {
		parsed, err := uuid.Parse(userID)
		if err != nil || parsed == uuid.Nil {
			return NormalizedLifecycleInput{}, domainerr.Validation("user_id must be a valid UUID")
		}
		userID = parsed.String()
	}
	return NormalizedLifecycleInput{
		TenantID:    tenantID,
		PrincipalID: principalID,
		UserID:      userID,
	}, nil
}

func NormalizeEnsureActiveInput(in EnsureActiveInput) (NormalizedEnsureActiveInput, error) {
	tenantID, err := tenantdomain.ParseTenantID(in.TenantID)
	if err != nil {
		return NormalizedEnsureActiveInput{}, err
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		return NormalizedEnsureActiveInput{}, domainerr.Validation("user_id is required")
	}
	parsedUserID, err := uuid.Parse(userID)
	if err != nil || parsedUserID == uuid.Nil {
		return NormalizedEnsureActiveInput{}, domainerr.Validation("user_id must be a valid UUID")
	}
	return NormalizedEnsureActiveInput{
		TenantID: tenantID,
		UserID:   parsedUserID.String(),
	}, nil
}

func NormalizeState(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case StateArchived:
		return StateArchived
	case "trash", StateTrashed:
		return StateTrashed
	default:
		return StateActive
	}
}

func NormalizeRole(raw string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return RoleMember, nil
	case RoleOwner:
		return RoleOwner, nil
	case RoleAdmin:
		return RoleAdmin, nil
	case RoleMember:
		return RoleMember, nil
	default:
		return "", domainerr.Validation("role must be one of owner, admin, member")
	}
}

func CanMutate(role string) bool {
	return role == RoleOwner || role == RoleAdmin
}

func CanAssignRole(actorRole, targetRole string) bool {
	if targetRole == RoleOwner {
		return actorRole == RoleOwner
	}
	return actorRole == RoleOwner || actorRole == RoleAdmin
}

func StateFromLifecycle(archivedAt, trashedAt *time.Time) string {
	if trashedAt != nil {
		return StateTrashed
	}
	if archivedAt != nil {
		return StateArchived
	}
	return StateActive
}

func KindFromID(id string) string {
	if isInvitationID(id) {
		return KindInvitation
	}
	return KindUser
}

func normalizeAccess(rawTenantID, rawPrincipalID string) (uuid.UUID, string, error) {
	tenantID, err := tenantdomain.ParseTenantID(rawTenantID)
	if err != nil {
		return uuid.Nil, "", err
	}
	principalID := strings.TrimSpace(rawPrincipalID)
	if principalID == "" {
		return uuid.Nil, "", domainerr.Validation("principal_id is required")
	}
	return tenantID, principalID, nil
}

func isInvitationID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "invitation:")
}

func normalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(strings.ToLower(raw))
	if email == "" {
		return "", domainerr.Validation("email is required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", domainerr.Validation("email must be valid")
	}
	return email, nil
}
