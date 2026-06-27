package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	authn "github.com/devpablocristo/platform/authn/go"
)

// clerkAPIError carries the HTTP status of a failed Clerk Backend API call so
// callers can tolerate specific outcomes (e.g. a 404 when an Axis-only org id
// is not a Clerk org).
type clerkAPIError struct {
	StatusCode int
	Body       string
}

func (e *clerkAPIError) Error() string {
	return fmt.Sprintf("identity provider request failed: status %d", e.StatusCode)
}

// clerkStatus returns the HTTP status code of a clerkAPIError, or 0 otherwise.
func clerkStatus(err error) int {
	var e *clerkAPIError
	if errors.As(err, &e) {
		return e.StatusCode
	}
	return 0
}

type clerkIdentityAdapter struct {
	secretKey string
	baseURL   string
	client    *http.Client
	store     IAMStore
}

func newClerkIdentityAdapter(secretKey string, baseURL string, client *http.Client, store IAMStore) HumanIdentityProvider {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.clerk.com/v1"
	}
	return &clerkIdentityAdapter{
		secretKey: strings.TrimSpace(secretKey),
		baseURL:   baseURL,
		client:    client,
		store:     store,
	}
}

func (a *clerkIdentityAdapter) PrincipalFromClaims(_ context.Context, p authn.Principal) (authn.Principal, error) {
	p.Actor = firstClaim(p.Claims, "sub")
	p.OrgID = firstClaim(p.Claims, "org_id", "orgId")
	p.AuthMethod = "clerk"
	return axisPrincipalFromIdentity(p), nil
}

func (a *clerkIdentityAdapter) SyncPrincipal(ctx context.Context, p authn.Principal) error {
	if strings.TrimSpace(p.Actor) == "" {
		return nil
	}
	email := firstNonEmpty(firstClaim(p.Claims, "email", "primary_email_address"), p.Actor)
	if _, err := a.store.CreateUser(ctx, IAMUser{
		ID:             p.Actor,
		ExternalID:     p.Actor,
		Provider:       "clerk",
		ProviderUserID: p.Actor,
		Email:          email,
		Name:           firstNonEmpty(firstClaim(p.Claims, "name", "username"), email),
		AxisRole:       claimRole(p.Claims, "axis_role"),
		Status:         "active",
	}); err != nil {
		return err
	}
	// Bridge: a Clerk axis_role=owner becomes a platform_admin role stored in
	// Axis (the Control Plane source of truth). This is the migration path off
	// Clerk metadata — once seeded, authz no longer depends on the Clerk claim.
	if normalizedRole(claimRole(p.Claims, "axis_role")) == "owner" {
		if err := a.store.SetPlatformRole(ctx, p.Actor, "platform_admin"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(p.OrgID) == "" {
		return nil
	}
	org := IAMOrg{
		ID:            p.OrgID,
		ExternalID:    p.OrgID,
		Provider:      "clerk",
		ProviderOrgID: p.OrgID,
		Name:          firstNonEmpty(firstClaim(p.Claims, "org_name", "org_slug"), p.OrgID),
		Slug:          firstNonEmpty(firstClaim(p.Claims, "org_slug"), slugify(p.OrgID)),
		Status:        "active",
	}
	if _, err := a.store.CreateOrg(ctx, org, ""); err != nil {
		return err
	}
	role := claimRole(p.Claims, "org_role")
	if role == "" {
		return nil
	}
	_, err := a.store.UpsertMember(ctx, IAMMember{OrgID: p.OrgID, UserID: p.Actor, Role: role, Status: "active"})
	return err
}

// clerkOrgID resolves an Axis org id to its Clerk organization id. Axis stores
// orgs by a business id (e.g. "cristo.tech"); Clerk addresses orgs by their own
// id (e.g. "org_3Fh0a1..."), kept in provider_org_id. Every Clerk org-scoped
// call MUST use this, never the Axis id, or Clerk returns 404.
func (a *clerkIdentityAdapter) clerkOrgID(ctx context.Context, axisOrgID string) string {
	axisOrgID = strings.TrimSpace(axisOrgID)
	if axisOrgID == "" || strings.HasPrefix(axisOrgID, "org_") {
		return axisOrgID
	}
	orgs, err := a.store.ListOrgsForActor(ctx, "", true)
	if err == nil {
		for _, o := range orgs {
			if o.ID == axisOrgID && strings.TrimSpace(o.ProviderOrgID) != "" {
				return o.ProviderOrgID
			}
		}
	}
	return axisOrgID
}

func (a *clerkIdentityAdapter) SyncOrgMembers(ctx context.Context, orgID string) error {
	if err := a.ensureRemote(); err != nil {
		return err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil
	}
	clerkOrg := a.clerkOrgID(ctx, orgID)
	const limit = 100
	for offset := 0; ; offset += limit {
		var payload map[string]any
		path := fmt.Sprintf("/organizations/%s/memberships?limit=%d&offset=%d", url.PathEscape(clerkOrg), limit, offset)
		if err := a.json(ctx, http.MethodGet, path, nil, &payload); err != nil {
			return err
		}
		items := clerkDataItems(payload)
		for _, item := range items {
			member := clerkMembership(item)
			// Store under the Axis org id (the Clerk response carries the Clerk
			// org id in member.OrgID); skip rows without a user.
			member.OrgID = orgID
			if member.UserID == "" {
				continue
			}
			user := clerkMembershipUser(item)
			if user.ID != "" {
				if _, err := a.store.CreateUser(ctx, user); err != nil {
					return err
				}
			}
			if _, err := a.store.UpsertMember(ctx, member); err != nil {
				return err
			}
		}
		total := clerkInt(payload, "total_count")
		if len(items) < limit || total <= offset+len(items) {
			return nil
		}
	}
}

func (a *clerkIdentityAdapter) CreateOrg(ctx context.Context, actor string, input IAMOrg) (IAMOrg, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMOrg{}, err
	}
	var payload map[string]any
	body := map[string]any{
		"name": firstNonEmpty(input.Name, input.Slug, input.ID),
	}
	if strings.TrimSpace(input.Slug) != "" {
		body["slug"] = strings.TrimSpace(input.Slug)
	}
	if err := a.json(ctx, http.MethodPost, "/organizations", body, &payload); err != nil {
		return IAMOrg{}, err
	}
	org := clerkOrg(payload, input)
	org.Status = firstNonEmpty(input.Status, "active")
	return a.store.CreateOrg(ctx, org, actor)
}

func (a *clerkIdentityAdapter) UpdateOrg(ctx context.Context, orgID string, input IAMOrg) (IAMOrg, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMOrg{}, err
	}
	if strings.TrimSpace(input.Name) != "" || strings.TrimSpace(input.Slug) != "" {
		var payload map[string]any
		body := map[string]any{}
		if strings.TrimSpace(input.Name) != "" {
			body["name"] = strings.TrimSpace(input.Name)
		}
		if strings.TrimSpace(input.Slug) != "" {
			body["slug"] = strings.TrimSpace(input.Slug)
		}
		if err := a.json(ctx, http.MethodPatch, "/organizations/"+url.PathEscape(a.clerkOrgID(ctx, orgID)), body, &payload); err != nil {
			return IAMOrg{}, err
		}
		remote := clerkOrg(payload, input)
		if strings.TrimSpace(input.Status) != "" {
			remote.Status = strings.TrimSpace(input.Status)
		}
		return a.store.UpdateOrg(ctx, orgID, remote)
	}
	return a.store.UpdateOrg(ctx, orgID, input)
}

func (a *clerkIdentityAdapter) DeleteOrg(ctx context.Context, orgID string) error {
	if err := a.ensureRemote(); err != nil {
		return err
	}
	if err := a.json(ctx, http.MethodDelete, "/organizations/"+url.PathEscape(a.clerkOrgID(ctx, orgID)), nil, nil); err != nil {
		// The org may not exist in Clerk (e.g. an Axis-only org id like
		// "local-dev-org"). Axis is the source of truth for tenancy, so a Clerk
		// 404 must not block removing the local record.
		if clerkStatus(err) != http.StatusNotFound {
			return err
		}
	}
	return a.store.DeleteOrg(ctx, orgID)
}

func (a *clerkIdentityAdapter) CreateUser(ctx context.Context, orgID string, input IAMUser) (IAMUser, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMUser{}, err
	}
	email := strings.TrimSpace(input.Email)
	if email == "" {
		return IAMUser{}, fmt.Errorf("email is required")
	}
	axisRole := normalizedRole(firstNonEmpty(input.AxisRole, input.Role))
	body := map[string]any{
		"email_address":             []string{email},
		"skip_password_requirement": true,
		"skip_password_checks":      true,
	}
	if orgID == "" && axisRole != "" {
		body["private_metadata"] = map[string]any{"axis_role": axisRole}
	}
	var payload map[string]any
	if err := a.json(ctx, http.MethodPost, "/users", body, &payload); err != nil {
		return IAMUser{}, err
	}
	user := clerkUser(payload, input)
	user.Role = input.Role
	if orgID == "" {
		user.AxisRole = axisRole
	}
	created, err := a.store.CreateUser(ctx, user)
	if err != nil {
		return IAMUser{}, err
	}
	if orgID != "" && input.Role != "" {
		if _, err := a.UpsertMember(ctx, IAMMember{OrgID: orgID, UserID: created.ID, Role: input.Role, Status: "active"}); err != nil {
			return IAMUser{}, err
		}
	}
	return created, nil
}

func (a *clerkIdentityAdapter) UpdateUser(ctx context.Context, orgID string, userID string, input IAMUser) (IAMUser, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMUser{}, err
	}
	axisRole := normalizedRole(firstNonEmpty(input.AxisRole, input.Role))
	if strings.TrimSpace(input.Email) != "" || strings.TrimSpace(input.Name) != "" || (orgID == "" && axisRole != "") {
		body := map[string]any{}
		if strings.TrimSpace(input.Email) != "" {
			body["email_address"] = []string{strings.TrimSpace(input.Email)}
		}
		if strings.TrimSpace(input.Name) != "" {
			body["first_name"] = strings.TrimSpace(input.Name)
		}
		if orgID == "" && axisRole != "" {
			body["private_metadata"] = map[string]any{"axis_role": axisRole}
		}
		var payload map[string]any
		if err := a.json(ctx, http.MethodPatch, "/users/"+url.PathEscape(userID), body, &payload); err != nil {
			return IAMUser{}, err
		}
		input = clerkUser(payload, input)
	}
	if orgID == "" && axisRole != "" {
		input.AxisRole = axisRole
	}
	user, err := a.store.UpdateUser(ctx, userID, input)
	if err != nil {
		return IAMUser{}, err
	}
	if orgID != "" && input.Role != "" {
		if _, err := a.UpdateMember(ctx, orgID, userID, IAMMember{Role: input.Role, Status: "active"}); err != nil {
			return IAMUser{}, err
		}
	}
	return user, nil
}

func (a *clerkIdentityAdapter) DeleteUser(ctx context.Context, orgID string, userID string) error {
	if orgID != "" {
		return a.DeleteMember(ctx, orgID, userID)
	}
	if err := a.ensureRemote(); err != nil {
		return err
	}
	if err := a.json(ctx, http.MethodDelete, "/users/"+url.PathEscape(userID), nil, nil); err != nil {
		// Already gone from Clerk (drift / prior delete): still remove locally so
		// a purge isn't blocked. Axis is the source of truth for the membership.
		if clerkStatus(err) != http.StatusNotFound {
			return err
		}
	}
	return a.store.DeleteUser(ctx, userID)
}

func (a *clerkIdentityAdapter) UpsertMember(ctx context.Context, member IAMMember) (IAMMember, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMMember{}, err
	}
	role := clerkRole(member.Role)
	if a.memberExists(ctx, member.OrgID, member.UserID) {
		return a.UpdateMember(ctx, member.OrgID, member.UserID, IAMMember{Role: normalizedRole(role), Status: firstNonEmpty(member.Status, "active")})
	}
	var payload map[string]any
	if err := a.json(ctx, http.MethodPost, "/organizations/"+url.PathEscape(a.clerkOrgID(ctx, member.OrgID))+"/memberships", map[string]any{
		"user_id": member.UserID,
		"role":    role,
	}, &payload); err != nil {
		return IAMMember{}, err
	}
	return a.store.UpsertMember(ctx, IAMMember{OrgID: member.OrgID, UserID: member.UserID, Role: normalizedRole(role), Status: firstNonEmpty(member.Status, "active")})
}

func (a *clerkIdentityAdapter) memberExists(ctx context.Context, orgID string, userID string) bool {
	members, err := a.store.ListMembers(ctx, orgID)
	if err != nil {
		return false
	}
	for _, member := range members {
		if member.UserID == userID {
			return true
		}
	}
	return false
}

func (a *clerkIdentityAdapter) UpdateMember(ctx context.Context, orgID string, userID string, member IAMMember) (IAMMember, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMMember{}, err
	}
	role := clerkRole(member.Role)
	if role != "" {
		var payload map[string]any
		if err := a.json(ctx, http.MethodPatch, "/organizations/"+url.PathEscape(a.clerkOrgID(ctx, orgID))+"/memberships/"+url.PathEscape(userID), map[string]any{"role": role}, &payload); err != nil {
			return IAMMember{}, err
		}
	}
	return a.store.UpdateMember(ctx, orgID, userID, IAMMember{Role: normalizedRole(role), Status: firstNonEmpty(member.Status, "active")})
}

func (a *clerkIdentityAdapter) DeleteMember(ctx context.Context, orgID string, userID string) error {
	if err := a.ensureRemote(); err != nil {
		return err
	}
	if err := a.json(ctx, http.MethodDelete, "/organizations/"+url.PathEscape(a.clerkOrgID(ctx, orgID))+"/memberships/"+url.PathEscape(userID), nil, nil); err != nil {
		// Membership already gone from Clerk: still remove the local row.
		if clerkStatus(err) != http.StatusNotFound {
			return err
		}
	}
	return a.store.DeleteMember(ctx, orgID, userID)
}

func (a *clerkIdentityAdapter) CreateInvitation(ctx context.Context, invite IAMInvitation) (IAMInvitation, error) {
	if err := a.ensureRemote(); err != nil {
		return IAMInvitation{}, err
	}
	var payload map[string]any
	if err := a.json(ctx, http.MethodPost, "/organizations/"+url.PathEscape(a.clerkOrgID(ctx, invite.OrgID))+"/invitations", map[string]any{
		"email_address": invite.Email,
		"role":          clerkRole(invite.Role),
	}, &payload); err != nil {
		return IAMInvitation{}, err
	}
	invite.ID = firstNonEmpty(clerkString(payload, "id"), invite.ID)
	invite.Provider = "clerk"
	invite.ProviderID = firstNonEmpty(clerkString(payload, "id"), invite.ProviderID)
	invite.Status = firstNonEmpty(clerkString(payload, "status"), "pending")
	return a.store.CreateInvitation(ctx, invite)
}

func (a *clerkIdentityAdapter) UpdateInvitationStatus(ctx context.Context, id string, status string, actor string) (IAMInvitation, error) {
	return a.store.UpdateInvitationStatus(ctx, id, status, actor)
}

func (a *clerkIdentityAdapter) HandleWebhook(ctx context.Context, eventType string, data map[string]any) error {
	switch strings.TrimSpace(eventType) {
	case "user.created", "user.updated":
		_, err := a.store.CreateUser(ctx, clerkUser(data, IAMUser{Status: "active"}))
		return err
	case "organization.created", "organization.updated":
		_, err := a.store.CreateOrg(ctx, clerkOrg(data, IAMOrg{Status: "active"}), "")
		return err
	case "user.deleted":
		// Keep Axis in sync when a user is removed in Clerk (the IdP). Without
		// this the axis_users row would orphan: Clerk is the source of truth for
		// identity existence, while Axis owns authz (memberships/roles).
		id := clerkString(data, "id")
		if id == "" {
			return nil
		}
		return a.store.DeleteUser(ctx, id)
	case "organization.deleted":
		id := clerkString(data, "id")
		if id == "" {
			return nil
		}
		return a.store.DeleteOrg(ctx, id)
	case "organizationMembership.created", "organizationMembership.updated", "organization_membership.created", "organization_membership.updated":
		member := clerkMembership(data)
		if member.OrgID == "" || member.UserID == "" {
			return nil
		}
		_, err := a.store.UpsertMember(ctx, member)
		return err
	case "organizationMembership.deleted", "organization_membership.deleted":
		member := clerkMembership(data)
		if member.OrgID == "" || member.UserID == "" {
			return nil
		}
		return a.store.DeleteMember(ctx, member.OrgID, member.UserID)
	default:
		return nil
	}
}

func (a *clerkIdentityAdapter) ensureRemote() error {
	if a == nil || a.secretKey == "" {
		return errIdentityProviderNotConfigured
	}
	return nil
}

func (a *clerkIdentityAdapter) json(ctx context.Context, method string, path string, body any, out any) error {
	if err := a.ensureRemote(); err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.secretKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &clerkAPIError{StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func clerkOrg(data map[string]any, fallback IAMOrg) IAMOrg {
	now := time.Now().UTC()
	return IAMOrg{
		ID:            firstNonEmpty(clerkString(data, "id"), fallback.ID),
		ExternalID:    firstNonEmpty(clerkString(data, "id"), fallback.ExternalID),
		Provider:      "clerk",
		ProviderOrgID: firstNonEmpty(clerkString(data, "id"), fallback.ProviderOrgID),
		Name:          firstNonEmpty(clerkString(data, "name"), fallback.Name, fallback.ID),
		Slug:          firstNonEmpty(clerkString(data, "slug"), fallback.Slug, slugify(firstNonEmpty(fallback.Name, fallback.ID))),
		Status:        firstNonEmpty(fallback.Status, "active"),
		CreatedAt:     firstNonZeroTime(clerkTime(data, "created_at"), fallback.CreatedAt, now),
		UpdatedAt:     firstNonZeroTime(clerkTime(data, "updated_at"), fallback.UpdatedAt, now),
	}
}

func clerkUser(data map[string]any, fallback IAMUser) IAMUser {
	now := time.Now().UTC()
	email := firstNonEmpty(clerkString(data, "email_address"), firstClerkEmail(data), fallback.Email, fallback.ID)
	name := firstNonEmpty(clerkString(data, "username"), clerkString(data, "first_name"), fallback.Name, email)
	return IAMUser{
		ID:             firstNonEmpty(clerkString(data, "id"), fallback.ID),
		ExternalID:     firstNonEmpty(clerkString(data, "id"), fallback.ExternalID),
		Provider:       "clerk",
		ProviderUserID: firstNonEmpty(clerkString(data, "id"), fallback.ProviderUserID),
		Email:          email,
		Name:           name,
		Role:           fallback.Role,
		AxisRole:       firstNonEmpty(clerkMetadataString(data, "private_metadata", "axis_role"), fallback.AxisRole),
		Status:         firstNonEmpty(fallback.Status, "active"),
		CreatedAt:      firstNonZeroTime(clerkTime(data, "created_at"), fallback.CreatedAt, now),
		UpdatedAt:      firstNonZeroTime(clerkTime(data, "updated_at"), fallback.UpdatedAt, now),
	}
}

func clerkMembershipUser(data map[string]any) IAMUser {
	publicUser, _ := data["public_user_data"].(map[string]any)
	userID := firstNonEmpty(
		clerkString(data, "user_id"),
		clerkString(publicUser, "user_id"),
		clerkString(publicUser, "identifier"),
	)
	email := firstNonEmpty(clerkString(publicUser, "identifier"), userID)
	name := firstNonEmpty(
		strings.TrimSpace(strings.Join([]string{clerkString(publicUser, "first_name"), clerkString(publicUser, "last_name")}, " ")),
		email,
		userID,
	)
	return IAMUser{
		ID:             userID,
		ExternalID:     userID,
		Provider:       "clerk",
		ProviderUserID: userID,
		Email:          email,
		Name:           name,
		Status:         "active",
		CreatedAt:      firstNonZeroTime(clerkTime(data, "created_at"), time.Time{}, time.Now().UTC()),
		UpdatedAt:      firstNonZeroTime(clerkTime(data, "updated_at"), time.Time{}, time.Now().UTC()),
	}
}

func clerkMetadataString(data map[string]any, objectKey string, key string) string {
	if nested, ok := data[objectKey].(map[string]any); ok {
		return normalizeClaimString(nested[key])
	}
	return ""
}

func clerkMembership(data map[string]any) IAMMember {
	orgID := clerkString(data, "organization_id")
	if orgID == "" {
		if org, ok := data["organization"].(map[string]any); ok {
			orgID = clerkString(org, "id")
		}
	}
	userID := clerkString(data, "user_id")
	if userID == "" {
		if user, ok := data["public_user_data"].(map[string]any); ok {
			userID = firstNonEmpty(clerkString(user, "user_id"), clerkString(user, "identifier"))
		}
	}
	return IAMMember{
		OrgID:     orgID,
		UserID:    userID,
		Role:      normalizedRole(clerkString(data, "role")),
		Status:    "active",
		CreatedAt: firstNonZeroTime(clerkTime(data, "created_at"), time.Time{}, time.Now().UTC()),
		UpdatedAt: firstNonZeroTime(clerkTime(data, "updated_at"), time.Time{}, time.Now().UTC()),
	}
}

func clerkRole(role string) string {
	role = normalizedRole(role)
	if role == "" {
		role = "member"
	}
	return "org:" + role
}

func clerkString(data map[string]any, key string) string {
	return normalizeClaimString(data[key])
}

func clerkTime(data map[string]any, key string) time.Time {
	switch value := data[key].(type) {
	case float64:
		if value > 1_000_000_000_000 {
			return time.UnixMilli(int64(value)).UTC()
		}
		if value > 0 {
			return time.Unix(int64(value), 0).UTC()
		}
	case json.Number:
		if n, err := value.Int64(); err == nil {
			if n > 1_000_000_000_000 {
				return time.UnixMilli(n).UTC()
			}
			if n > 0 {
				return time.Unix(n, 0).UTC()
			}
		}
	}
	return time.Time{}
}

func clerkDataItems(data map[string]any) []map[string]any {
	raw, ok := data["data"].([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mapped, ok := item.(map[string]any); ok {
			items = append(items, mapped)
		}
	}
	return items
}

func clerkInt(data map[string]any, key string) int {
	switch value := data[key].(type) {
	case float64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	case int:
		return value
	}
	return 0
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
