package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type IAMOrg struct {
	ID            string    `json:"id"`
	ExternalID    string    `json:"external_id,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	ProviderOrgID string    `json:"provider_org_id,omitempty"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type IAMUser struct {
	ID             string    `json:"id"`
	ExternalID     string    `json:"external_id,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	ProviderUserID string    `json:"provider_user_id,omitempty"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	Role           string    `json:"role,omitempty"`
	AxisRole       string    `json:"axis_role,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type IAMMember struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      *IAMUser  `json:"user,omitempty"`
}

type IAMInvitation struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"org_id"`
	Email      string    `json:"email"`
	Role       string    `json:"role"`
	Status     string    `json:"status"`
	Provider   string    `json:"provider"`
	ProviderID string    `json:"provider_invitation_id,omitempty"`
	InvitedBy  string    `json:"invited_by,omitempty"`
	AcceptedBy string    `json:"accepted_by,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type IAMAuditEvent struct {
	ID        string         `json:"id"`
	OrgID     string         `json:"org_id,omitempty"`
	Actor     string         `json:"actor,omitempty"`
	Action    string         `json:"action"`
	Target    string         `json:"target"`
	TargetID  string         `json:"target_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type IAMProduct struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	ProductSurface string         `json:"product_surface"`
	Name           string         `json:"name"`
	Status         string         `json:"status"`
	Config         map[string]any `json:"config,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type IAMAgent struct {
	ID                   string         `json:"id"`
	OrgID                string         `json:"org_id"`
	Name                 string         `json:"name"`
	Profile              string         `json:"profile"`
	Autonomy             string         `json:"autonomy"`
	MemoryEnabled        bool           `json:"memory_enabled"`
	Description          string         `json:"description"`
	Capabilities         []string       `json:"capabilities"`
	Tools                []string       `json:"tools"`
	Status               string         `json:"status"`
	SourceSystem         string         `json:"source_system,omitempty"`
	SourceOrgID          string         `json:"source_org_id,omitempty"`
	SourceProductSurface string         `json:"source_product_surface,omitempty"`
	SourceAgentID        string         `json:"source_agent_id,omitempty"`
	ExternalTenantID     string         `json:"external_tenant_id,omitempty"`
	SourceStatus         string         `json:"source_status,omitempty"`
	OriginKind           string         `json:"origin_kind,omitempty"`
	ReviewStatus         string         `json:"review_status,omitempty"`
	ValidationStatus     string         `json:"validation_status,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	LastSyncedAt         *time.Time     `json:"last_synced_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type IAMStore interface {
	ListOrgsForActor(context.Context, string, bool) ([]IAMOrg, error)
	ActorCanAccessOrg(context.Context, string, string) (bool, error)
	CreateOrg(context.Context, IAMOrg, string) (IAMOrg, error)
	UpdateOrg(context.Context, string, IAMOrg) (IAMOrg, error)
	DeleteOrg(context.Context, string) error
	ListUsers(context.Context) ([]IAMUser, error)
	CreateUser(context.Context, IAMUser) (IAMUser, error)
	UpdateUser(context.Context, string, IAMUser) (IAMUser, error)
	DeleteUser(context.Context, string) error
	ListProducts(context.Context, string, string) ([]IAMProduct, error)
	CreateProduct(context.Context, IAMProduct) (IAMProduct, error)
	UpdateProduct(context.Context, string, IAMProduct) (IAMProduct, error)
	DeleteProduct(context.Context, string) error
	ListMembers(context.Context, string) ([]IAMMember, error)
	UpsertMember(context.Context, IAMMember) (IAMMember, error)
	UpdateMember(context.Context, string, string, IAMMember) (IAMMember, error)
	DeleteMember(context.Context, string, string) error
	ListInvitations(context.Context, string) ([]IAMInvitation, error)
	CreateInvitation(context.Context, IAMInvitation) (IAMInvitation, error)
	UpdateInvitationStatus(context.Context, string, string, string) (IAMInvitation, error)
	ListAuditEvents(context.Context, string) ([]IAMAuditEvent, error)
	AppendAuditEvent(context.Context, IAMAuditEvent) error
	// Control Plane tenancy (tenant = org x product).
	ListTenants(context.Context, string) ([]IAMTenant, error)
	TenantByID(context.Context, string) (IAMTenant, error)
	CreateTenant(context.Context, IAMTenant) (IAMTenant, error)
	CreateTenantWithOwner(context.Context, IAMTenant, string) (IAMTenant, error)
	ResolveTenantsForUser(context.Context, string) ([]IAMTenant, error)
	UserInTenant(context.Context, string, string) (bool, error)
	TenantMembership(context.Context, string, string) (IAMTenantMember, error)
	ListTenantMembers(context.Context, string) ([]IAMTenantMember, error)
	UpsertTenantMember(context.Context, IAMTenantMember) (IAMTenantMember, error)
	RemoveTenantMember(context.Context, string, string) error
	PlatformRolesForUser(context.Context, string) ([]string, error)
	SetPlatformRole(context.Context, string, string) error
	RemovePlatformRole(context.Context, string, string) error
	// SetTenantMembership atomically upserts the tenant role and applies the
	// platform-owner op in a single transaction, so an owner promotion/demotion
	// can never half-apply (e.g. global owner granted but tenant member missing).
	SetTenantMembership(ctx context.Context, tenantID, userID, role, status string, op platformRoleOp) error
}

// platformRoleOp controls the global owner platform role within SetTenantMembership.
type platformRoleOp int

const (
	platformRoleKeep        platformRoleOp = iota // leave platform roles untouched
	platformRoleGrantOwner                        // ensure the global owner role
	platformRoleRevokeOwner                       // drop the global owner role
)

func newIAMStore(ctx context.Context, cfg config) (IAMStore, error) {
	if strings.TrimSpace(cfg.ControlDatabaseURL) == "" {
		return newMemoryIAMStore(), nil
	}
	db, err := sql.Open("pgx", cfg.ControlDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open axis control database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping axis control database: %w", err)
	}
	store := &sqlIAMStore{db: db}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

type memoryIAMStore struct {
	mu            sync.RWMutex
	orgs          map[string]IAMOrg
	users         map[string]IAMUser
	products      map[string]IAMProduct
	members       map[string]IAMMember
	invitations   map[string]IAMInvitation
	audit         []IAMAuditEvent
	tenants       map[string]IAMTenant       // by tenant id
	tenantMembers map[string]IAMTenantMember // by tenantMemberKey(tenantID, userID)
	platformRoles map[string][]string        // userID -> roles
}

func newMemoryIAMStore() *memoryIAMStore {
	// Starts empty — no seeded org/user/tenant/platform-role. All data comes from
	// the Control Plane (provisioning) or real identity sync, never from a seed.
	return &memoryIAMStore{
		orgs:          map[string]IAMOrg{},
		users:         map[string]IAMUser{},
		products:      map[string]IAMProduct{},
		members:       map[string]IAMMember{},
		invitations:   map[string]IAMInvitation{},
		audit:         []IAMAuditEvent{},
		tenants:       map[string]IAMTenant{},
		tenantMembers: map[string]IAMTenantMember{},
		platformRoles: map[string][]string{},
	}
}

func (s *memoryIAMStore) ListOrgsForActor(_ context.Context, actor string, cross bool) ([]IAMOrg, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IAMOrg, 0, len(s.orgs))
	for _, org := range s.orgs {
		if cross || s.actorCanAccessOrgLocked(actor, org.ID) {
			out = append(out, org)
		}
	}
	return out, nil
}

func (s *memoryIAMStore) ActorCanAccessOrg(_ context.Context, actor string, orgID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.actorCanAccessOrgLocked(actor, orgID), nil
}

func (s *memoryIAMStore) actorCanAccessOrgLocked(actor string, orgID string) bool {
	actor = strings.TrimSpace(actor)
	for _, user := range s.users {
		if user.ID != actor && user.Email != actor && user.ExternalID != actor && user.ProviderUserID != actor {
			continue
		}
		member, ok := s.members[memberKey(orgID, user.ID)]
		return ok && member.Status == "active"
	}
	return false
}

func (s *memoryIAMStore) CreateOrg(_ context.Context, org IAMOrg, actor string) (IAMOrg, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	org.ID = firstNonEmpty(org.ID, "org_"+randomHex(8))
	org.Name = firstNonEmpty(org.Name, org.ID)
	org.Slug = firstNonEmpty(org.Slug, slugify(org.Name))
	org.Status = firstNonEmpty(org.Status, "active")
	org.CreatedAt = now
	org.UpdatedAt = now
	s.orgs[org.ID] = org
	if user, ok := s.findUserLocked(actor); ok {
		s.members[memberKey(org.ID, user.ID)] = IAMMember{OrgID: org.ID, UserID: user.ID, Role: "owner", Status: "active", CreatedAt: now, UpdatedAt: now}
	}
	return org, nil
}

func (s *memoryIAMStore) UpdateOrg(_ context.Context, orgID string, patch IAMOrg) (IAMOrg, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[orgID]
	if !ok {
		return IAMOrg{}, errNotFound
	}
	if strings.TrimSpace(patch.Name) != "" {
		org.Name = strings.TrimSpace(patch.Name)
	}
	if strings.TrimSpace(patch.Slug) != "" {
		org.Slug = strings.TrimSpace(patch.Slug)
	}
	if strings.TrimSpace(patch.Status) != "" {
		org.Status = strings.TrimSpace(patch.Status)
	}
	org.UpdatedAt = time.Now().UTC()
	s.orgs[org.ID] = org
	return org, nil
}

func (s *memoryIAMStore) DeleteOrg(_ context.Context, orgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return errNotFound
	}
	delete(s.orgs, orgID)
	for key, member := range s.members {
		if member.OrgID == orgID {
			delete(s.members, key)
		}
	}
	for key, invite := range s.invitations {
		if invite.OrgID == orgID {
			delete(s.invitations, key)
		}
	}
	for key, product := range s.products {
		if product.TenantID == orgID {
			delete(s.products, key)
		}
	}
	return nil
}

func (s *memoryIAMStore) ListUsers(context.Context) ([]IAMUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IAMUser, 0, len(s.users))
	for _, user := range s.users {
		out = append(out, user)
	}
	return out, nil
}

func (s *memoryIAMStore) CreateUser(_ context.Context, user IAMUser) (IAMUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	user.ID = firstNonEmpty(user.ID, "user_"+randomHex(8))
	user.ExternalID = firstNonEmpty(user.ExternalID, user.ProviderUserID, user.Email, user.ID)
	user.Email = strings.TrimSpace(user.Email)
	user.Name = firstNonEmpty(user.Name, user.Email, user.ID)
	user.AxisRole = normalizedRole(user.AxisRole)
	user.Status = firstNonEmpty(user.Status, "active")
	user.CreatedAt = now
	user.UpdatedAt = now
	s.users[user.ID] = user
	return user, nil
}

func (s *memoryIAMStore) UpdateUser(_ context.Context, userID string, patch IAMUser) (IAMUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[userID]
	if !ok {
		return IAMUser{}, errNotFound
	}
	if strings.TrimSpace(patch.Email) != "" {
		user.Email = strings.TrimSpace(patch.Email)
	}
	if strings.TrimSpace(patch.Name) != "" {
		user.Name = strings.TrimSpace(patch.Name)
	}
	if strings.TrimSpace(patch.AxisRole) != "" {
		user.AxisRole = normalizedRole(patch.AxisRole)
	}
	if strings.TrimSpace(patch.Status) != "" {
		user.Status = strings.TrimSpace(patch.Status)
	}
	user.UpdatedAt = time.Now().UTC()
	s.users[user.ID] = user
	return user, nil
}

func (s *memoryIAMStore) DeleteUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return errNotFound
	}
	delete(s.users, userID)
	for key, member := range s.members {
		if member.UserID == userID {
			delete(s.members, key)
		}
	}
	// Mirror the SQL FK ON DELETE CASCADE: drop tenant memberships + platform roles.
	for key, member := range s.tenantMembers {
		if member.UserID == userID {
			delete(s.tenantMembers, key)
		}
	}
	delete(s.platformRoles, userID)
	return nil
}

func (s *memoryIAMStore) ListProducts(_ context.Context, tenantID string, status string) ([]IAMProduct, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []IAMProduct{}
	for _, product := range s.products {
		if strings.TrimSpace(tenantID) != "" && product.TenantID != tenantID {
			continue
		}
		if strings.TrimSpace(status) != "" && product.Status != status {
			continue
		}
		out = append(out, product)
	}
	return out, nil
}

func (s *memoryIAMStore) CreateProduct(_ context.Context, product IAMProduct) (IAMProduct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	product.ID = firstNonEmpty(product.ID, "product_"+randomHex(8))
	product.Name = firstNonEmpty(product.Name, product.ProductSurface, product.ID)
	product.ProductSurface = firstNonEmpty(product.ProductSurface, slugify(product.Name))
	product.Status = firstNonEmpty(product.Status, "active")
	product.CreatedAt = now
	product.UpdatedAt = now
	s.products[product.ID] = product
	return product, nil
}

func (s *memoryIAMStore) UpdateProduct(_ context.Context, productID string, patch IAMProduct) (IAMProduct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	product, ok := s.products[productID]
	if !ok {
		return IAMProduct{}, errNotFound
	}
	if strings.TrimSpace(patch.Name) != "" {
		product.Name = strings.TrimSpace(patch.Name)
	}
	if strings.TrimSpace(patch.ProductSurface) != "" {
		product.ProductSurface = slugify(patch.ProductSurface)
	}
	if strings.TrimSpace(patch.Status) != "" {
		product.Status = strings.TrimSpace(patch.Status)
	}
	if patch.Config != nil {
		product.Config = patch.Config
	}
	product.UpdatedAt = time.Now().UTC()
	s.products[product.ID] = product
	return product, nil
}

func (s *memoryIAMStore) DeleteProduct(_ context.Context, productID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.products[productID]; !ok {
		return errNotFound
	}
	delete(s.products, productID)
	return nil
}

func (s *memoryIAMStore) ListMembers(_ context.Context, orgID string) ([]IAMMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []IAMMember{}
	for _, member := range s.members {
		if member.OrgID == orgID {
			user := s.users[member.UserID]
			member.User = &user
			out = append(out, member)
		}
	}
	return out, nil
}

func (s *memoryIAMStore) UpsertMember(_ context.Context, member IAMMember) (IAMMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	member.Role = firstNonEmpty(member.Role, "member")
	member.Status = firstNonEmpty(member.Status, "active")
	member.CreatedAt = now
	member.UpdatedAt = now
	s.members[memberKey(member.OrgID, member.UserID)] = member
	return member, nil
}

func (s *memoryIAMStore) UpdateMember(_ context.Context, orgID string, userID string, patch IAMMember) (IAMMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memberKey(orgID, userID)
	member, ok := s.members[key]
	if !ok {
		return IAMMember{}, errNotFound
	}
	if strings.TrimSpace(patch.Role) != "" {
		member.Role = strings.TrimSpace(patch.Role)
	}
	if strings.TrimSpace(patch.Status) != "" {
		member.Status = strings.TrimSpace(patch.Status)
	}
	member.UpdatedAt = time.Now().UTC()
	s.members[key] = member
	return member, nil
}

func (s *memoryIAMStore) DeleteMember(_ context.Context, orgID string, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memberKey(orgID, userID)
	if _, ok := s.members[key]; !ok {
		return errNotFound
	}
	delete(s.members, key)
	return nil
}

func (s *memoryIAMStore) CreateInvitation(_ context.Context, invite IAMInvitation) (IAMInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	invite.ID = firstNonEmpty(invite.ID, "inv_"+randomHex(8))
	invite.Role = firstNonEmpty(invite.Role, "member")
	invite.Status = firstNonEmpty(invite.Status, "pending")
	invite.Provider = firstNonEmpty(invite.Provider, "identity")
	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = now.Add(7 * 24 * time.Hour)
	}
	invite.CreatedAt = now
	invite.UpdatedAt = now
	s.invitations[invite.ID] = invite
	return invite, nil
}

func (s *memoryIAMStore) ListInvitations(_ context.Context, orgID string) ([]IAMInvitation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IAMInvitation, 0, len(s.invitations))
	for _, invite := range s.invitations {
		if invite.OrgID == orgID {
			out = append(out, invite)
		}
	}
	return out, nil
}

func (s *memoryIAMStore) UpdateInvitationStatus(_ context.Context, id string, status string, actor string) (IAMInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	invite, ok := s.invitations[id]
	if !ok {
		return IAMInvitation{}, errNotFound
	}
	invite.Status = status
	invite.UpdatedAt = time.Now().UTC()
	if status == "accepted" {
		invite.AcceptedBy = actor
	}
	s.invitations[id] = invite
	return invite, nil
}

func (s *memoryIAMStore) ListAuditEvents(_ context.Context, orgID string) ([]IAMAuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IAMAuditEvent, 0, len(s.audit))
	for i := len(s.audit) - 1; i >= 0; i-- {
		event := s.audit[i]
		if orgID == "" || event.OrgID == "" || event.OrgID == orgID {
			out = append(out, event)
		}
		if len(out) >= 100 {
			break
		}
	}
	return out, nil
}

func (s *memoryIAMStore) AppendAuditEvent(_ context.Context, event IAMAuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = firstNonEmpty(event.ID, "audit_"+randomHex(8))
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	s.audit = append(s.audit, event)
	return nil
}

func (s *memoryIAMStore) findUserLocked(actor string) (IAMUser, bool) {
	for _, user := range s.users {
		if user.ID == actor || user.Email == actor || user.ExternalID == actor || user.ProviderUserID == actor {
			return user, true
		}
	}
	return IAMUser{}, false
}

type sqlIAMStore struct {
	db *sql.DB
}

func (s *sqlIAMStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS axis_orgs (
	id text PRIMARY KEY,
	external_id text UNIQUE,
	provider text NOT NULL DEFAULT '',
	provider_org_id text UNIQUE,
	name text NOT NULL,
	slug text NOT NULL UNIQUE,
	status text NOT NULL DEFAULT 'active',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS axis_users (
	id text PRIMARY KEY,
	external_id text UNIQUE,
	provider text NOT NULL DEFAULT '',
	provider_user_id text UNIQUE,
	email text NOT NULL UNIQUE,
	name text NOT NULL DEFAULT '',
	axis_role text NOT NULL DEFAULT '',
	status text NOT NULL DEFAULT 'active',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS axis_products (
	id text PRIMARY KEY,
	tenant_id text NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
	product_surface text NOT NULL,
	name text NOT NULL,
	status text NOT NULL DEFAULT 'active',
	config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (tenant_id, product_surface)
);
CREATE TABLE IF NOT EXISTS axis_org_members (
	org_id text NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
	user_id text NOT NULL REFERENCES axis_users(id) ON DELETE CASCADE,
	role text NOT NULL DEFAULT 'member',
	status text NOT NULL DEFAULT 'active',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (org_id, user_id)
);
CREATE TABLE IF NOT EXISTS axis_org_invitations (
	id text PRIMARY KEY,
	org_id text NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
	email text NOT NULL,
	role text NOT NULL DEFAULT 'member',
	status text NOT NULL DEFAULT 'pending',
	provider text NOT NULL DEFAULT 'identity',
	provider_invitation_id text,
	invited_by text,
	accepted_by text,
	expires_at timestamptz NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS axis_iam_audit_events (
	id text PRIMARY KEY,
	org_id text,
	actor text,
	action text NOT NULL,
	target text NOT NULL,
	target_id text,
	payload_json jsonb NOT NULL DEFAULT '{}'::jsonb,
	created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_axis_org_members_user ON axis_org_members(user_id);
CREATE INDEX IF NOT EXISTS idx_axis_products_tenant ON axis_products(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_axis_org_invitations_org ON axis_org_invitations(org_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_axis_iam_audit_events_org ON axis_iam_audit_events(org_id, created_at DESC);
ALTER TABLE axis_orgs ADD COLUMN IF NOT EXISTS synced_at timestamptz;
ALTER TABLE axis_users ADD COLUMN IF NOT EXISTS synced_at timestamptz;
ALTER TABLE axis_users ADD COLUMN IF NOT EXISTS axis_role text NOT NULL DEFAULT '';
ALTER TABLE axis_org_members ADD COLUMN IF NOT EXISTS provider_membership_id text;
ALTER TABLE axis_org_members ADD COLUMN IF NOT EXISTS synced_at timestamptz;

-- Control Plane tenancy model: a tenant is (organization x product). It is the
-- first-class unit that scopes all application-plane data; the BFF resolves a
-- tenant_id to (org_id, product_surface) when minting the internal JWT.
CREATE TABLE IF NOT EXISTS axis_tenants (
	id text PRIMARY KEY,
	org_id text NOT NULL REFERENCES axis_orgs(id) ON DELETE CASCADE,
	product_surface text NOT NULL,
	name text NOT NULL DEFAULT '',
	status text NOT NULL DEFAULT 'active',
	plan text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (org_id, product_surface)
);
CREATE TABLE IF NOT EXISTS axis_tenant_members (
	tenant_id text NOT NULL REFERENCES axis_tenants(id) ON DELETE CASCADE,
	user_id text NOT NULL REFERENCES axis_users(id) ON DELETE CASCADE,
	role text NOT NULL DEFAULT 'member',
	status text NOT NULL DEFAULT 'active',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (tenant_id, user_id)
);
-- Platform roles are orthogonal to the active tenant: they grant Control Plane
-- access (super-admin), stored in Axis, never derived from Clerk metadata.
CREATE TABLE IF NOT EXISTS axis_platform_roles (
	user_id text NOT NULL REFERENCES axis_users(id) ON DELETE CASCADE,
	role text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (user_id, role)
);
CREATE INDEX IF NOT EXISTS idx_axis_tenant_members_user ON axis_tenant_members(user_id, status);
CREATE INDEX IF NOT EXISTS idx_axis_tenants_org ON axis_tenants(org_id, status);
`)
	if err != nil {
		return fmt.Errorf("migrate axis control plane: %w", err)
	}
	return nil
}

func (s *sqlIAMStore) ListOrgsForActor(ctx context.Context, actor string, cross bool) ([]IAMOrg, error) {
	query := `SELECT o.id, COALESCE(o.external_id, ''), o.provider, COALESCE(o.provider_org_id, ''), o.name, o.slug, o.status, o.created_at, o.updated_at FROM axis_orgs o`
	args := []any{}
	if !cross {
		query += ` JOIN axis_org_members m ON m.org_id = o.id JOIN axis_users u ON u.id = m.user_id WHERE m.status = 'active' AND (u.id = $1 OR u.email = $1 OR u.external_id = $1 OR u.provider_user_id = $1)`
		args = append(args, actor)
	}
	query += ` ORDER BY o.created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMOrg
	for rows.Next() {
		var org IAMOrg
		if err := rows.Scan(&org.ID, &org.ExternalID, &org.Provider, &org.ProviderOrgID, &org.Name, &org.Slug, &org.Status, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, org)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) ActorCanAccessOrg(ctx context.Context, actor string, orgID string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM axis_org_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.status = 'active'
		  AND (u.id = $2 OR u.email = $2 OR u.external_id = $2 OR u.provider_user_id = $2)
	)`, orgID, actor).Scan(&ok)
	return ok, err
}

func (s *sqlIAMStore) CreateOrg(ctx context.Context, org IAMOrg, actor string) (IAMOrg, error) {
	now := time.Now().UTC()
	org.ID = firstNonEmpty(org.ID, "org_"+randomHex(8))
	org.Name = firstNonEmpty(org.Name, org.ID)
	org.Slug = firstNonEmpty(org.Slug, slugify(org.Name))
	org.Status = firstNonEmpty(org.Status, "active")

	// Resolve the owner (read) before the tx; the org is created whether or not
	// the actor resolves to a known user.
	ownerID := ""
	if strings.TrimSpace(actor) != "" {
		user, ok, err := s.findUser(ctx, actor)
		if err != nil {
			return IAMOrg{}, err
		}
		if ok {
			ownerID = user.ID
		}
	}

	// Atomic: the org row and its owner membership must not half-apply (an org
	// without its owner, or vice versa). Wrap both writes in one transaction.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return IAMOrg{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO axis_orgs (id, external_id, provider, provider_org_id, name, slug, status, created_at, updated_at)
		VALUES ($1, nullif($2, ''), $3, nullif($4, ''), $5, $6, $7, $8, $8)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, slug = EXCLUDED.slug, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_org_id, ''), name, slug, status, created_at, updated_at
	`, org.ID, org.ExternalID, org.Provider, org.ProviderOrgID, org.Name, org.Slug, org.Status, now).Scan(&org.ID, &org.ExternalID, &org.Provider, &org.ProviderOrgID, &org.Name, &org.Slug, &org.Status, &org.CreatedAt, &org.UpdatedAt); err != nil {
		return IAMOrg{}, err
	}
	if ownerID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO axis_org_members (org_id, user_id, role, status, created_at, updated_at)
			VALUES ($1, $2, 'owner', 'active', $3, $3)
			ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		`, org.ID, ownerID, now); err != nil {
			return IAMOrg{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return IAMOrg{}, err
	}
	return org, nil
}

func (s *sqlIAMStore) UpdateOrg(ctx context.Context, orgID string, patch IAMOrg) (IAMOrg, error) {
	current := IAMOrg{}
	err := s.db.QueryRowContext(ctx, `
		UPDATE axis_orgs
		SET name = COALESCE(NULLIF($2, ''), name),
		    slug = COALESCE(NULLIF($3, ''), slug),
		    status = COALESCE(NULLIF($4, ''), status),
		    updated_at = now()
		WHERE id = $1
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_org_id, ''), name, slug, status, created_at, updated_at
	`, orgID, patch.Name, patch.Slug, patch.Status).Scan(&current.ID, &current.ExternalID, &current.Provider, &current.ProviderOrgID, &current.Name, &current.Slug, &current.Status, &current.CreatedAt, &current.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMOrg{}, errNotFound
	}
	return current, err
}

func (s *sqlIAMStore) DeleteOrg(ctx context.Context, orgID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM axis_orgs WHERE id = $1`, orgID)
	if err != nil {
		return err
	}
	return ensureDeleted(result)
}

func (s *sqlIAMStore) ListUsers(ctx context.Context) ([]IAMUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, axis_role, status, created_at, updated_at FROM axis_users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMUser
	for rows.Next() {
		var user IAMUser
		if err := rows.Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.AxisRole, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) CreateUser(ctx context.Context, user IAMUser) (IAMUser, error) {
	now := time.Now().UTC()
	user.ID = firstNonEmpty(user.ID, "user_"+randomHex(8))
	user.ExternalID = firstNonEmpty(user.ExternalID, user.ProviderUserID, user.Email, user.ID)
	user.Email = firstNonEmpty(user.Email, user.ExternalID)
	user.Name = firstNonEmpty(user.Name, user.Email, user.ID)
	user.AxisRole = normalizedRole(user.AxisRole)
	user.Status = firstNonEmpty(user.Status, "active")
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO axis_users (id, external_id, provider, provider_user_id, email, name, axis_role, status, created_at, updated_at)
		VALUES ($1, nullif($2, ''), $3, nullif($4, ''), $5, $6, $7, $8, $9, $9)
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			name = EXCLUDED.name,
			axis_role = COALESCE(NULLIF(EXCLUDED.axis_role, ''), axis_users.axis_role),
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, axis_role, status, created_at, updated_at
	`, user.ID, user.ExternalID, user.Provider, user.ProviderUserID, user.Email, user.Name, user.AxisRole, user.Status, now).Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.AxisRole, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}

func (s *sqlIAMStore) UpdateUser(ctx context.Context, userID string, patch IAMUser) (IAMUser, error) {
	var user IAMUser
	err := s.db.QueryRowContext(ctx, `
		UPDATE axis_users
		SET email = COALESCE(NULLIF($2, ''), email),
		    name = COALESCE(NULLIF($3, ''), name),
		    axis_role = COALESCE($4, axis_role),
		    status = COALESCE(NULLIF($5, ''), status),
		    updated_at = now()
		WHERE id = $1
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, axis_role, status, created_at, updated_at
	`, userID, patch.Email, patch.Name, nullableRole(patch.AxisRole), patch.Status).Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.AxisRole, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMUser{}, errNotFound
	}
	return user, err
}

func (s *sqlIAMStore) DeleteUser(ctx context.Context, userID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM axis_users WHERE id = $1`, userID)
	if err != nil {
		return err
	}
	return ensureDeleted(result)
}

func (s *sqlIAMStore) ListProducts(ctx context.Context, tenantID string, status string) ([]IAMProduct, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, product_surface, name, status, config_json, created_at, updated_at
		FROM axis_products
		WHERE ($1 = '' OR tenant_id = $1)
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
	`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMProduct
	for rows.Next() {
		var product IAMProduct
		var config []byte
		if err := rows.Scan(&product.ID, &product.TenantID, &product.ProductSurface, &product.Name, &product.Status, &config, &product.CreatedAt, &product.UpdatedAt); err != nil {
			return nil, err
		}
		if len(config) > 0 {
			_ = json.Unmarshal(config, &product.Config)
		}
		out = append(out, product)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) CreateProduct(ctx context.Context, product IAMProduct) (IAMProduct, error) {
	now := time.Now().UTC()
	product.ID = firstNonEmpty(product.ID, "product_"+randomHex(8))
	product.Name = firstNonEmpty(product.Name, product.ProductSurface, product.ID)
	product.ProductSurface = firstNonEmpty(product.ProductSurface, slugify(product.Name))
	product.Status = firstNonEmpty(product.Status, "active")
	config, err := jsonMap(product.Config)
	if err != nil {
		return IAMProduct{}, err
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO axis_products (id, tenant_id, product_surface, name, status, config_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (tenant_id, product_surface) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		RETURNING id, tenant_id, product_surface, name, status, config_json, created_at, updated_at
	`, product.ID, product.TenantID, product.ProductSurface, product.Name, product.Status, string(config), now).Scan(&product.ID, &product.TenantID, &product.ProductSurface, &product.Name, &product.Status, &config, &product.CreatedAt, &product.UpdatedAt)
	if len(config) > 0 {
		_ = json.Unmarshal(config, &product.Config)
	}
	return product, err
}

func (s *sqlIAMStore) UpdateProduct(ctx context.Context, productID string, patch IAMProduct) (IAMProduct, error) {
	var product IAMProduct
	var config []byte
	configArg, err := jsonMap(patch.Config)
	if err != nil {
		return IAMProduct{}, err
	}
	productSurface := ""
	if strings.TrimSpace(patch.ProductSurface) != "" {
		productSurface = slugify(patch.ProductSurface)
	}
	err = s.db.QueryRowContext(ctx, `
		UPDATE axis_products
		SET product_surface = COALESCE(NULLIF($2, ''), product_surface),
		    name = COALESCE(NULLIF($3, ''), name),
		    status = COALESCE(NULLIF($4, ''), status),
		    config_json = CASE WHEN $5::jsonb = '{}'::jsonb THEN config_json ELSE $5::jsonb END,
		    updated_at = now()
		WHERE id = $1
		RETURNING id, tenant_id, product_surface, name, status, config_json, created_at, updated_at
	`, productID, productSurface, patch.Name, patch.Status, string(configArg)).Scan(&product.ID, &product.TenantID, &product.ProductSurface, &product.Name, &product.Status, &config, &product.CreatedAt, &product.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMProduct{}, errNotFound
	}
	if len(config) > 0 {
		_ = json.Unmarshal(config, &product.Config)
	}
	return product, err
}

func (s *sqlIAMStore) DeleteProduct(ctx context.Context, productID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM axis_products WHERE id = $1`, productID)
	if err != nil {
		return err
	}
	return ensureDeleted(result)
}

func (s *sqlIAMStore) ListMembers(ctx context.Context, orgID string) ([]IAMMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.org_id, m.user_id, m.role, m.status, m.created_at, m.updated_at,
		       u.id, COALESCE(u.external_id, ''), u.provider, COALESCE(u.provider_user_id, ''), u.email, u.name, u.axis_role, u.status, u.created_at, u.updated_at
		FROM axis_org_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY m.created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMMember
	for rows.Next() {
		var member IAMMember
		var user IAMUser
		if err := rows.Scan(&member.OrgID, &member.UserID, &member.Role, &member.Status, &member.CreatedAt, &member.UpdatedAt, &user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.AxisRole, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		member.User = &user
		out = append(out, member)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) UpsertMember(ctx context.Context, member IAMMember) (IAMMember, error) {
	now := time.Now().UTC()
	member.Role = firstNonEmpty(member.Role, "member")
	member.Status = firstNonEmpty(member.Status, "active")
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO axis_org_members (org_id, user_id, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		RETURNING org_id, user_id, role, status, created_at, updated_at
	`, member.OrgID, member.UserID, member.Role, member.Status, now).Scan(&member.OrgID, &member.UserID, &member.Role, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	return member, err
}

func (s *sqlIAMStore) UpdateMember(ctx context.Context, orgID string, userID string, patch IAMMember) (IAMMember, error) {
	var member IAMMember
	err := s.db.QueryRowContext(ctx, `
		UPDATE axis_org_members
		SET role = COALESCE(NULLIF($3, ''), role),
		    status = COALESCE(NULLIF($4, ''), status),
		    updated_at = now()
		WHERE org_id = $1 AND user_id = $2
		RETURNING org_id, user_id, role, status, created_at, updated_at
	`, orgID, userID, patch.Role, patch.Status).Scan(&member.OrgID, &member.UserID, &member.Role, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMMember{}, errNotFound
	}
	return member, err
}

func (s *sqlIAMStore) DeleteMember(ctx context.Context, orgID string, userID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM axis_org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return err
	}
	return ensureDeleted(result)
}

func (s *sqlIAMStore) CreateInvitation(ctx context.Context, invite IAMInvitation) (IAMInvitation, error) {
	now := time.Now().UTC()
	invite.ID = firstNonEmpty(invite.ID, "inv_"+randomHex(8))
	invite.Role = firstNonEmpty(invite.Role, "member")
	invite.Status = firstNonEmpty(invite.Status, "pending")
	invite.Provider = firstNonEmpty(invite.Provider, "identity")
	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = now.Add(7 * 24 * time.Hour)
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO axis_org_invitations (id, org_id, email, role, status, provider, provider_invitation_id, invited_by, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), $8, $9, $10, $10)
		RETURNING id, org_id, email, role, status, provider, COALESCE(provider_invitation_id, ''), COALESCE(invited_by, ''), COALESCE(accepted_by, ''), expires_at, created_at, updated_at
	`, invite.ID, invite.OrgID, invite.Email, invite.Role, invite.Status, invite.Provider, invite.ProviderID, invite.InvitedBy, invite.ExpiresAt, now).Scan(&invite.ID, &invite.OrgID, &invite.Email, &invite.Role, &invite.Status, &invite.Provider, &invite.ProviderID, &invite.InvitedBy, &invite.AcceptedBy, &invite.ExpiresAt, &invite.CreatedAt, &invite.UpdatedAt)
	return invite, err
}

func (s *sqlIAMStore) ListInvitations(ctx context.Context, orgID string) ([]IAMInvitation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, org_id, email, role, status, provider, COALESCE(provider_invitation_id, ''), COALESCE(invited_by, ''), COALESCE(accepted_by, ''), expires_at, created_at, updated_at
		FROM axis_org_invitations
		WHERE org_id = $1
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMInvitation
	for rows.Next() {
		var invite IAMInvitation
		if err := rows.Scan(&invite.ID, &invite.OrgID, &invite.Email, &invite.Role, &invite.Status, &invite.Provider, &invite.ProviderID, &invite.InvitedBy, &invite.AcceptedBy, &invite.ExpiresAt, &invite.CreatedAt, &invite.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, invite)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) UpdateInvitationStatus(ctx context.Context, id string, status string, actor string) (IAMInvitation, error) {
	var invite IAMInvitation
	err := s.db.QueryRowContext(ctx, `
		UPDATE axis_org_invitations
		SET status = $2,
		    accepted_by = CASE WHEN $2 = 'accepted' THEN $3 ELSE accepted_by END,
		    updated_at = now()
		WHERE id = $1
		RETURNING id, org_id, email, role, status, provider, COALESCE(provider_invitation_id, ''), COALESCE(invited_by, ''), COALESCE(accepted_by, ''), expires_at, created_at, updated_at
	`, id, status, actor).Scan(&invite.ID, &invite.OrgID, &invite.Email, &invite.Role, &invite.Status, &invite.Provider, &invite.ProviderID, &invite.InvitedBy, &invite.AcceptedBy, &invite.ExpiresAt, &invite.CreatedAt, &invite.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMInvitation{}, errNotFound
	}
	return invite, err
}

func (s *sqlIAMStore) ListAuditEvents(ctx context.Context, orgID string) ([]IAMAuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(org_id, ''), COALESCE(actor, ''), action, target, COALESCE(target_id, ''), payload_json, created_at
		FROM axis_iam_audit_events
		WHERE ($1 = '' OR org_id IS NULL OR org_id = $1)
		ORDER BY created_at DESC
		LIMIT 100
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMAuditEvent
	for rows.Next() {
		var event IAMAuditEvent
		var payload []byte
		if err := rows.Scan(&event.ID, &event.OrgID, &event.Actor, &event.Action, &event.Target, &event.TargetID, &payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &event.Payload)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) AppendAuditEvent(ctx context.Context, event IAMAuditEvent) error {
	event.ID = firstNonEmpty(event.ID, "audit_"+randomHex(8))
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO axis_iam_audit_events (id, org_id, actor, action, target, target_id, payload_json, created_at)
		VALUES ($1, nullif($2, ''), nullif($3, ''), $4, $5, nullif($6, ''), $7, $8)
	`, event.ID, event.OrgID, event.Actor, event.Action, event.Target, event.TargetID, payload, event.CreatedAt)
	return err
}

func (s *sqlIAMStore) findUser(ctx context.Context, actor string) (IAMUser, bool, error) {
	var user IAMUser
	err := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, axis_role, status, created_at, updated_at
		FROM axis_users
		WHERE id = $1 OR email = $1 OR external_id = $1 OR provider_user_id = $1
		LIMIT 1
	`, actor).Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.AxisRole, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMUser{}, false, nil
	}
	if err != nil {
		return IAMUser{}, false, err
	}
	return user, true, nil
}

var errNotFound = errors.New("not found")

// errValidation marca una falla de validación del cliente; writeStoreError la
// mapea a HTTP 400 (en vez del 500 por defecto de errores sin clasificar).
var errValidation = errors.New("validation")

func ensureDeleted(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errNotFound
	}
	return nil
}

func nullableRole(role string) any {
	role = normalizedRole(role)
	if role == "" {
		return nil
	}
	return role
}

func jsonMap(value map[string]any) ([]byte, error) {
	if len(value) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(compactPayload(value))
}

func cleanStringList(values []string) []string {
	if values == nil {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeAutonomy(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A1", "A2", "A3":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return "A1"
	}
}

func memberKey(orgID, userID string) string {
	return strings.TrimSpace(orgID) + "\x00" + strings.TrimSpace(userID)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "org"
	}
	return slug
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func decodeJSONBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var out T
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return out, false
	}
	return out, true
}
