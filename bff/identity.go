package main

import (
	"context"
	"errors"
	"strings"

	authn "github.com/devpablocristo/platform/authn/go"
)

var errIdentityProviderNotConfigured = errors.New("identity provider is not configured")

type HumanIdentityProvider interface {
	PrincipalFromClaims(context.Context, authn.Principal) (authn.Principal, error)
	SyncPrincipal(context.Context, authn.Principal) error
	SyncOrgMembers(context.Context, string) error
	CreateOrg(context.Context, string, IAMOrg) (IAMOrg, error)
	UpdateOrg(context.Context, string, IAMOrg) (IAMOrg, error)
	DeleteOrg(context.Context, string) error
	CreateUser(context.Context, string, IAMUser) (IAMUser, error)
	UpdateUser(context.Context, string, string, IAMUser) (IAMUser, error)
	DeleteUser(context.Context, string, string) error
	UpsertMember(context.Context, IAMMember) (IAMMember, error)
	UpdateMember(context.Context, string, string, IAMMember) (IAMMember, error)
	DeleteMember(context.Context, string, string) error
	CreateInvitation(context.Context, IAMInvitation) (IAMInvitation, error)
	UpdateInvitationStatus(context.Context, string, string, string) (IAMInvitation, error)
	HandleWebhook(context.Context, string, map[string]any) error
}

func (s *server) createIAMOrg(ctx context.Context, actor string, input IAMOrg) (IAMOrg, error) {
	if s.identity != nil {
		return s.identity.CreateOrg(ctx, actor, input)
	}
	return s.iam.CreateOrg(ctx, input, actor)
}

func (s *server) updateIAMOrg(ctx context.Context, orgID string, input IAMOrg) (IAMOrg, error) {
	if s.identity != nil {
		return s.identity.UpdateOrg(ctx, orgID, input)
	}
	return s.iam.UpdateOrg(ctx, orgID, input)
}

func (s *server) deleteIAMOrg(ctx context.Context, orgID string) error {
	if s.identity != nil {
		return s.identity.DeleteOrg(ctx, orgID)
	}
	return s.iam.DeleteOrg(ctx, orgID)
}

func (s *server) createIAMUser(ctx context.Context, orgID string, input IAMUser) (IAMUser, error) {
	if s.identity != nil {
		return s.identity.CreateUser(ctx, orgID, input)
	}
	user, err := s.iam.CreateUser(ctx, input)
	if err != nil {
		return IAMUser{}, err
	}
	if orgID != "" && input.Role != "" {
		_, err = s.iam.UpsertMember(ctx, IAMMember{OrgID: orgID, UserID: user.ID, Role: input.Role, Status: "active"})
	}
	return user, err
}

func (s *server) updateIAMUser(ctx context.Context, orgID string, userID string, input IAMUser) (IAMUser, error) {
	if s.identity != nil {
		return s.identity.UpdateUser(ctx, orgID, userID, input)
	}
	user, err := s.iam.UpdateUser(ctx, userID, input)
	if err != nil {
		return IAMUser{}, err
	}
	if orgID != "" && input.Role != "" {
		_, err = s.iam.UpdateMember(ctx, orgID, user.ID, IAMMember{Role: input.Role, Status: "active"})
		if errors.Is(err, errNotFound) {
			_, err = s.iam.UpsertMember(ctx, IAMMember{OrgID: orgID, UserID: user.ID, Role: input.Role, Status: "active"})
		}
		if err != nil {
			return IAMUser{}, err
		}
	}
	return user, nil
}

func (s *server) deleteIAMUser(ctx context.Context, orgID string, userID string) error {
	if s.identity != nil {
		return s.identity.DeleteUser(ctx, orgID, userID)
	}
	return s.iam.DeleteUser(ctx, userID)
}

func (s *server) upsertIAMMember(ctx context.Context, orgID string, userID string, role string) error {
	if s.identity != nil {
		_, err := s.identity.UpsertMember(ctx, IAMMember{OrgID: orgID, UserID: userID, Role: role, Status: "active"})
		return err
	}
	_, err := s.iam.UpsertMember(ctx, IAMMember{OrgID: orgID, UserID: userID, Role: role, Status: "active"})
	return err
}

func axisPrincipalFromIdentity(p authn.Principal) authn.Principal {
	axisRole := normalizedRole(claimRole(p.Claims, "axis_role"))
	orgRole := normalizedRole(claimRole(p.Claims, "org_role"))
	if axisRole != "" {
		p.Role = axisRole
	} else {
		p.Role = orgRole
	}
	p.Scopes = scopesForIdentityRoles(axisRole, orgRole)
	return p
}

func scopesForIdentityRoles(axisRole string, orgRole string) []string {
	switch axisRole {
	case "owner":
		return defaultAdminScopes()
	case "admin":
		return withoutScopes(defaultAdminScopes(), "axis:iam:purge")
	}
	switch orgRole {
	case "owner", "admin":
		return orgAdminScopes()
	case "member":
		return orgMemberScopes()
	default:
		return nil
	}
}

// isPlatformAdmin reports whether any of the user's platform roles grants
// Control Plane / super-admin access (orthogonal to the active tenant).
func isPlatformAdmin(platformRoles []string) bool {
	// Platform roles are a distinct vocabulary from tenant roles, so we must NOT
	// run them through normalizedRole (which only recognizes owner/admin/member
	// and would erase "platform_admin" to ""). Compare the raw role directly.
	for _, r := range platformRoles {
		switch strings.TrimSpace(strings.ToLower(r)) {
		case "platform_admin", "super_admin", "owner":
			return true
		}
	}
	return false
}

// scopesForTenant derives scopes from the user's role IN THE ACTIVE TENANT plus
// their platform roles — the authz source of truth is Axis, NOT Clerk metadata.
// A platform admin gets full cross-org admin (control plane). Otherwise the
// tenant role maps to per-tenant admin/member scopes.
func scopesForTenant(tenantRole string, platformRoles []string) []string {
	if isPlatformAdmin(platformRoles) {
		return defaultAdminScopes()
	}
	switch normalizedRole(tenantRole) {
	case "owner", "admin":
		return orgAdminScopes()
	case "member":
		return orgMemberScopes()
	default:
		return nil
	}
}

func orgAdminScopes() []string {
	return []string{
		"axis:orgs:read",
		"axis:users:read",
		"axis:users:write",
		"axis:users:admin",
		"axis:agents:read",
		"axis:agents:write",
		"axis:agents:admin",
		"axis:virployees:read",
		"axis:virployees:write",
		"axis:virployees:admin",
		"axis:products:read",
		"axis:products:write",
		"axis:products:admin",
		"companion:tasks:read",
		"companion:tasks:write",
		"companion:assist:read",
		"companion:assist:write",
		"companion:virployee_profiles:read",
		"companion:capabilities:read",
		"companion:capabilities:admin",
		"companion:products:read",
		"companion:virployees:read",
		"companion:virployees:write",
		"companion:virployees:admin",
		"companion:agents:read",
		"companion:memory:read",
		"companion:memory:write",
		"companion:memory:admin",
		"companion:observability:read",
		"companion:costs:read",
		"nexus:requests:read",
		"nexus:requests:write",
		"nexus:approvals:decide",
		"nexus:dashboard:read",
	}
}

func orgMemberScopes() []string {
	return []string{
		"companion:tasks:read",
		"companion:assist:read",
		"companion:assist:write",
		"companion:products:read",
		"nexus:requests:read",
		"nexus:dashboard:read",
	}
}

func withoutScopes(scopes []string, denied ...string) []string {
	blocked := make(map[string]struct{}, len(denied))
	for _, scope := range denied {
		blocked[scope] = struct{}{}
	}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if _, ok := blocked[scope]; !ok {
			out = append(out, scope)
		}
	}
	return out
}

func claimRole(claims map[string]any, key string) string {
	return normalizedRole(firstClaim(claims, key))
}

func normalizedRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	role = strings.TrimPrefix(role, "org:")
	role = strings.TrimPrefix(role, "axis:")
	switch role {
	case "owner", "admin", "member":
		return role
	default:
		return ""
	}
}
