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

type IAMStore interface {
	ListOrgsForActor(context.Context, string, bool) ([]IAMOrg, error)
	ActorCanAccessOrg(context.Context, string, string) (bool, error)
	CreateOrg(context.Context, IAMOrg, string) (IAMOrg, error)
	UpdateOrg(context.Context, string, IAMOrg) (IAMOrg, error)
	ListUsers(context.Context) ([]IAMUser, error)
	CreateUser(context.Context, IAMUser) (IAMUser, error)
	UpdateUser(context.Context, string, IAMUser) (IAMUser, error)
	ListMembers(context.Context, string) ([]IAMMember, error)
	UpsertMember(context.Context, IAMMember) (IAMMember, error)
	UpdateMember(context.Context, string, string, IAMMember) (IAMMember, error)
	CreateInvitation(context.Context, IAMInvitation) (IAMInvitation, error)
	UpdateInvitationStatus(context.Context, string, string, string) (IAMInvitation, error)
}

func newIAMStore(ctx context.Context, cfg config) (IAMStore, error) {
	if strings.TrimSpace(cfg.ControlDatabaseURL) == "" {
		return newMemoryIAMStore(cfg), nil
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
	if err := store.seed(ctx, cfg); err != nil {
		return nil, err
	}
	return store, nil
}

type memoryIAMStore struct {
	mu          sync.RWMutex
	orgs        map[string]IAMOrg
	users       map[string]IAMUser
	members     map[string]IAMMember
	invitations map[string]IAMInvitation
}

func newMemoryIAMStore(cfg config) *memoryIAMStore {
	now := time.Now().UTC()
	orgID := firstNonEmpty(cfg.DevOrgID, defaultDevOrgID)
	userID := firstNonEmpty(cfg.DevUserID, defaultDevUserID)
	org := IAMOrg{ID: orgID, Name: "Local Dev Org", Slug: slugify(orgID), Status: "active", CreatedAt: now, UpdatedAt: now}
	user := IAMUser{ID: userID, ExternalID: userID, Provider: "dev", ProviderUserID: userID, Email: userID, Name: userID, Status: "active", CreatedAt: now, UpdatedAt: now}
	member := IAMMember{OrgID: org.ID, UserID: user.ID, Role: "owner", Status: "active", CreatedAt: now, UpdatedAt: now}
	return &memoryIAMStore{
		orgs:        map[string]IAMOrg{org.ID: org},
		users:       map[string]IAMUser{user.ID: user},
		members:     map[string]IAMMember{memberKey(member.OrgID, member.UserID): member},
		invitations: map[string]IAMInvitation{},
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
	if strings.TrimSpace(patch.Status) != "" {
		user.Status = strings.TrimSpace(patch.Status)
	}
	user.UpdatedAt = time.Now().UTC()
	s.users[user.ID] = user
	return user, nil
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

func (s *memoryIAMStore) CreateInvitation(_ context.Context, invite IAMInvitation) (IAMInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	invite.ID = firstNonEmpty(invite.ID, "inv_"+randomHex(8))
	invite.Role = firstNonEmpty(invite.Role, "member")
	invite.Status = firstNonEmpty(invite.Status, "pending")
	invite.Provider = firstNonEmpty(invite.Provider, "clerk")
	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = now.Add(7 * 24 * time.Hour)
	}
	invite.CreatedAt = now
	invite.UpdatedAt = now
	s.invitations[invite.ID] = invite
	return invite, nil
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
	status text NOT NULL DEFAULT 'active',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
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
	provider text NOT NULL DEFAULT 'clerk',
	provider_invitation_id text,
	invited_by text,
	accepted_by text,
	expires_at timestamptz NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_axis_org_members_user ON axis_org_members(user_id);
CREATE INDEX IF NOT EXISTS idx_axis_org_invitations_org ON axis_org_invitations(org_id, status, created_at DESC);
`)
	if err != nil {
		return fmt.Errorf("migrate axis control plane: %w", err)
	}
	return nil
}

func (s *sqlIAMStore) seed(ctx context.Context, cfg config) error {
	now := time.Now().UTC()
	org := IAMOrg{ID: firstNonEmpty(cfg.DevOrgID, defaultDevOrgID), Name: "Local Dev Org", Slug: slugify(firstNonEmpty(cfg.DevOrgID, defaultDevOrgID)), Status: "active", CreatedAt: now, UpdatedAt: now}
	user := IAMUser{ID: firstNonEmpty(cfg.DevUserID, defaultDevUserID), ExternalID: firstNonEmpty(cfg.DevUserID, defaultDevUserID), Provider: "dev", ProviderUserID: firstNonEmpty(cfg.DevUserID, defaultDevUserID), Email: firstNonEmpty(cfg.DevUserID, defaultDevUserID), Name: firstNonEmpty(cfg.DevUserID, defaultDevUserID), Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, err := s.CreateOrg(ctx, org, ""); err != nil {
		return err
	}
	if _, err := s.CreateUser(ctx, user); err != nil {
		return err
	}
	_, err := s.UpsertMember(ctx, IAMMember{OrgID: org.ID, UserID: user.ID, Role: "owner", Status: "active"})
	return err
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
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO axis_orgs (id, external_id, provider, provider_org_id, name, slug, status, created_at, updated_at)
		VALUES ($1, nullif($2, ''), $3, nullif($4, ''), $5, $6, $7, $8, $8)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, slug = EXCLUDED.slug, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_org_id, ''), name, slug, status, created_at, updated_at
	`, org.ID, org.ExternalID, org.Provider, org.ProviderOrgID, org.Name, org.Slug, org.Status, now).Scan(&org.ID, &org.ExternalID, &org.Provider, &org.ProviderOrgID, &org.Name, &org.Slug, &org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		return IAMOrg{}, err
	}
	if strings.TrimSpace(actor) != "" {
		if user, ok, err := s.findUser(ctx, actor); err != nil {
			return IAMOrg{}, err
		} else if ok {
			if _, err := s.UpsertMember(ctx, IAMMember{OrgID: org.ID, UserID: user.ID, Role: "owner", Status: "active"}); err != nil {
				return IAMOrg{}, err
			}
		}
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

func (s *sqlIAMStore) ListUsers(ctx context.Context) ([]IAMUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, status, created_at, updated_at FROM axis_users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IAMUser
	for rows.Next() {
		var user IAMUser
		if err := rows.Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
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
	user.Status = firstNonEmpty(user.Status, "active")
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO axis_users (id, external_id, provider, provider_user_id, email, name, status, created_at, updated_at)
		VALUES ($1, nullif($2, ''), $3, nullif($4, ''), $5, $6, $7, $8, $8)
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, status, created_at, updated_at
	`, user.ID, user.ExternalID, user.Provider, user.ProviderUserID, user.Email, user.Name, user.Status, now).Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}

func (s *sqlIAMStore) UpdateUser(ctx context.Context, userID string, patch IAMUser) (IAMUser, error) {
	var user IAMUser
	err := s.db.QueryRowContext(ctx, `
		UPDATE axis_users
		SET email = COALESCE(NULLIF($2, ''), email),
		    name = COALESCE(NULLIF($3, ''), name),
		    status = COALESCE(NULLIF($4, ''), status),
		    updated_at = now()
		WHERE id = $1
		RETURNING id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, status, created_at, updated_at
	`, userID, patch.Email, patch.Name, patch.Status).Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMUser{}, errNotFound
	}
	return user, err
}

func (s *sqlIAMStore) ListMembers(ctx context.Context, orgID string) ([]IAMMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.org_id, m.user_id, m.role, m.status, m.created_at, m.updated_at,
		       u.id, COALESCE(u.external_id, ''), u.provider, COALESCE(u.provider_user_id, ''), u.email, u.name, u.status, u.created_at, u.updated_at
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
		if err := rows.Scan(&member.OrgID, &member.UserID, &member.Role, &member.Status, &member.CreatedAt, &member.UpdatedAt, &user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
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

func (s *sqlIAMStore) CreateInvitation(ctx context.Context, invite IAMInvitation) (IAMInvitation, error) {
	now := time.Now().UTC()
	invite.ID = firstNonEmpty(invite.ID, "inv_"+randomHex(8))
	invite.Role = firstNonEmpty(invite.Role, "member")
	invite.Status = firstNonEmpty(invite.Status, "pending")
	invite.Provider = firstNonEmpty(invite.Provider, "clerk")
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

func (s *sqlIAMStore) findUser(ctx context.Context, actor string) (IAMUser, bool, error) {
	var user IAMUser
	err := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(external_id, ''), provider, COALESCE(provider_user_id, ''), email, name, status, created_at, updated_at
		FROM axis_users
		WHERE id = $1 OR email = $1 OR external_id = $1 OR provider_user_id = $1
		LIMIT 1
	`, actor).Scan(&user.ID, &user.ExternalID, &user.Provider, &user.ProviderUserID, &user.Email, &user.Name, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IAMUser{}, false, nil
	}
	if err != nil {
		return IAMUser{}, false, err
	}
	return user, true, nil
}

var errNotFound = errors.New("not found")

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
