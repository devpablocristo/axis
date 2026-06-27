package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// IAMTenant is the first-class Control Plane tenant: an (organization x product)
// instance. It is what scopes all application-plane data. The BFF resolves a
// tenant_id to (org_id=OrgID, product_surface=ProductSurface) when minting the
// internal JWT, so companion/nexus keep scoping by (org_id, product_surface).
type IAMTenant struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	ProductSurface string    `json:"product_surface"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	Plan           string    `json:"plan,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// IAMTenantMember binds a user to a tenant with a tenant-scoped role
// (owner/admin/member). This replaces deriving authz from Clerk org roles.
type IAMTenantMember struct {
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      *IAMUser  `json:"user,omitempty"`
}

func tenantMemberKey(tenantID, userID string) string { return tenantID + "|" + userID }

// --- memoryIAMStore ---

func (s *memoryIAMStore) ListTenants(_ context.Context, orgID string) ([]IAMTenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IAMTenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		if orgID == "" || t.OrgID == orgID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *memoryIAMStore) TenantByID(_ context.Context, tenantID string) (IAMTenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantID]
	if !ok {
		return IAMTenant{}, errNotFound
	}
	return t, nil
}

func (s *memoryIAMStore) CreateTenant(_ context.Context, tenant IAMTenant) (IAMTenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tenants {
		if t.OrgID == tenant.OrgID && t.ProductSurface == tenant.ProductSurface {
			return t, nil // idempotent: tenant for this (org, product) already exists
		}
	}
	now := time.Now().UTC()
	tenant.ID = firstNonEmpty(tenant.ID, "tenant_"+randomHex(8))
	tenant.Status = firstNonEmpty(tenant.Status, "active")
	tenant.CreatedAt, tenant.UpdatedAt = now, now
	s.tenants[tenant.ID] = tenant
	return tenant, nil
}

func (s *memoryIAMStore) ResolveTenantsForUser(_ context.Context, userID string) ([]IAMTenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []IAMTenant
	for _, m := range s.tenantMembers {
		if m.UserID != userID || m.Status != "active" {
			continue
		}
		if t, ok := s.tenants[m.TenantID]; ok && t.Status == "active" {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *memoryIAMStore) UserInTenant(_ context.Context, userID string, tenantID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.tenantMembers[tenantMemberKey(tenantID, userID)]
	return ok && m.Status == "active", nil
}

func (s *memoryIAMStore) TenantMembership(_ context.Context, tenantID string, userID string) (IAMTenantMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.tenantMembers[tenantMemberKey(tenantID, userID)]
	if !ok {
		return IAMTenantMember{}, errNotFound
	}
	return m, nil
}

func (s *memoryIAMStore) ListTenantMembers(_ context.Context, tenantID string) ([]IAMTenantMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []IAMTenantMember
	for _, m := range s.tenantMembers {
		if m.TenantID != tenantID {
			continue
		}
		if u, ok := s.users[m.UserID]; ok {
			uc := u
			m.User = &uc
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *memoryIAMStore) UpsertTenantMember(_ context.Context, member IAMTenantMember) (IAMTenantMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	member.Role = firstNonEmpty(member.Role, "member")
	member.Status = firstNonEmpty(member.Status, "active")
	key := tenantMemberKey(member.TenantID, member.UserID)
	if existing, ok := s.tenantMembers[key]; ok {
		member.CreatedAt = existing.CreatedAt
	} else {
		member.CreatedAt = now
	}
	member.UpdatedAt = now
	s.tenantMembers[key] = member
	return member, nil
}

func (s *memoryIAMStore) RemoveTenantMember(_ context.Context, tenantID string, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tenantMembers, tenantMemberKey(tenantID, userID))
	return nil
}

func (s *memoryIAMStore) PlatformRolesForUser(_ context.Context, userID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.platformRoles[userID]...), nil
}

func (s *memoryIAMStore) SetPlatformRole(_ context.Context, userID string, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.platformRoles[userID] {
		if r == role {
			return nil
		}
	}
	s.platformRoles[userID] = append(s.platformRoles[userID], role)
	return nil
}

func (s *memoryIAMStore) RemovePlatformRole(_ context.Context, userID string, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]string, 0, len(s.platformRoles[userID]))
	for _, r := range s.platformRoles[userID] {
		if r != role {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		delete(s.platformRoles, userID)
	} else {
		s.platformRoles[userID] = kept
	}
	return nil
}

// --- sqlIAMStore ---

const tenantColumns = `id, org_id, product_surface, name, status, plan, created_at, updated_at`

func scanTenant(row interface{ Scan(...any) error }) (IAMTenant, error) {
	var t IAMTenant
	err := row.Scan(&t.ID, &t.OrgID, &t.ProductSurface, &t.Name, &t.Status, &t.Plan, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (s *sqlIAMStore) ListTenants(ctx context.Context, orgID string) ([]IAMTenant, error) {
	query := `SELECT ` + tenantColumns + ` FROM axis_tenants`
	args := []any{}
	if orgID != "" {
		query += ` WHERE org_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY org_id, product_surface`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMTenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) TenantByID(ctx context.Context, tenantID string) (IAMTenant, error) {
	t, err := scanTenant(s.db.QueryRowContext(ctx, `SELECT `+tenantColumns+` FROM axis_tenants WHERE id = $1`, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return IAMTenant{}, errNotFound
	}
	return t, err
}

func (s *sqlIAMStore) CreateTenant(ctx context.Context, tenant IAMTenant) (IAMTenant, error) {
	now := time.Now().UTC()
	tenant.ID = firstNonEmpty(tenant.ID, "tenant_"+randomHex(8))
	tenant.Status = firstNonEmpty(tenant.Status, "active")
	out, err := scanTenant(s.db.QueryRowContext(ctx, `
		INSERT INTO axis_tenants (id, org_id, product_surface, name, status, plan, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (org_id, product_surface) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), axis_tenants.name),
			status = EXCLUDED.status,
			plan = COALESCE(NULLIF(EXCLUDED.plan, ''), axis_tenants.plan),
			updated_at = EXCLUDED.updated_at
		RETURNING `+tenantColumns,
		tenant.ID, tenant.OrgID, tenant.ProductSurface, tenant.Name, tenant.Status, tenant.Plan, now))
	return out, err
}

func (s *sqlIAMStore) ResolveTenantsForUser(ctx context.Context, userID string) ([]IAMTenant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.org_id, t.product_surface, t.name, t.status, t.plan, t.created_at, t.updated_at
		FROM axis_tenants t
		JOIN axis_tenant_members m ON m.tenant_id = t.id
		WHERE m.user_id = $1 AND m.status = 'active' AND t.status = 'active'
		ORDER BY t.org_id, t.product_surface
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMTenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) UserInTenant(ctx context.Context, userID string, tenantID string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM axis_tenant_members WHERE tenant_id = $1 AND user_id = $2 AND status = 'active')
	`, tenantID, userID).Scan(&ok)
	return ok, err
}

func (s *sqlIAMStore) TenantMembership(ctx context.Context, tenantID string, userID string) (IAMTenantMember, error) {
	var m IAMTenantMember
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, user_id, role, status, created_at, updated_at
		FROM axis_tenant_members WHERE tenant_id = $1 AND user_id = $2
	`, tenantID, userID).Scan(&m.TenantID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMTenantMember{}, errNotFound
	}
	return m, err
}

func (s *sqlIAMStore) ListTenantMembers(ctx context.Context, tenantID string) ([]IAMTenantMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.tenant_id, m.user_id, m.role, m.status, m.created_at, m.updated_at,
		       u.id, COALESCE(u.external_id, ''), u.provider, COALESCE(u.provider_user_id, ''), u.email, u.name, u.axis_role, u.status, u.created_at, u.updated_at
		FROM axis_tenant_members m
		JOIN axis_users u ON u.id = m.user_id
		WHERE m.tenant_id = $1
		ORDER BY m.created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMTenantMember
	for rows.Next() {
		var m IAMTenantMember
		var u IAMUser
		if err := rows.Scan(&m.TenantID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt,
			&u.ID, &u.ExternalID, &u.Provider, &u.ProviderUserID, &u.Email, &u.Name, &u.AxisRole, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		m.User = &u
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) UpsertTenantMember(ctx context.Context, member IAMTenantMember) (IAMTenantMember, error) {
	now := time.Now().UTC()
	member.Role = firstNonEmpty(member.Role, "member")
	member.Status = firstNonEmpty(member.Status, "active")
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO axis_tenant_members (tenant_id, user_id, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		RETURNING tenant_id, user_id, role, status, created_at, updated_at
	`, member.TenantID, member.UserID, member.Role, member.Status, now).Scan(
		&member.TenantID, &member.UserID, &member.Role, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	return member, err
}

func (s *sqlIAMStore) RemoveTenantMember(ctx context.Context, tenantID string, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM axis_tenant_members WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID)
	return err
}

func (s *sqlIAMStore) PlatformRolesForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role FROM axis_platform_roles WHERE user_id = $1 ORDER BY role`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (s *sqlIAMStore) SetPlatformRole(ctx context.Context, userID string, role string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO axis_platform_roles (user_id, role) VALUES ($1, $2)
		ON CONFLICT (user_id, role) DO NOTHING
	`, userID, role)
	return err
}

func (s *sqlIAMStore) RemovePlatformRole(ctx context.Context, userID string, role string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM axis_platform_roles WHERE user_id = $1 AND role = $2`, userID, role)
	return err
}
