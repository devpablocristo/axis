package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	Create(context.Context, Grant) (Grant, error)
	List(context.Context, string, string) ([]Grant, error)
	ActiveForUser(context.Context, string, string, time.Time) ([]Grant, error)
	Revoke(context.Context, string, uuid.UUID, string, string, int64) (Grant, error)
}

type UseCases struct {
	repo RepositoryPort
	now  func() time.Time
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}
func (u *UseCases) Definitions() []RoleDefinition { return Definitions() }

// EffectiveGrants returns only currently usable grants for a tenant user. It is
// used by governance to derive CEL role context server-side; callers cannot
// inject functional roles into a governance request.
func (u *UseCases) EffectiveGrants(ctx context.Context, tenantID, userID string) ([]Grant, error) {
	return u.repo.ActiveForUser(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(userID), u.now())
}

func (u *UseCases) Create(ctx context.Context, tenantID, actorID, actorRole string, in CreateGrantInput) (Grant, error) {
	if !ownerOrAdmin(actorRole) {
		return Grant{}, domainerr.Forbidden("role grants require an owner or admin")
	}
	normalized, err := NormalizeCreate(in, u.now())
	if err != nil {
		return Grant{}, err
	}
	now := u.now()
	grant := Grant{ID: uuid.New(), TenantID: strings.TrimSpace(tenantID), UserID: normalized.UserID, RoleKey: normalized.RoleKey,
		ProductSurface: normalized.ProductSurface, ActionTypePattern: normalized.ActionTypePattern, ResourceType: normalized.ResourceType,
		ResourceID: normalized.ResourceID, MaxRiskClass: normalized.MaxRiskClass, ValidFrom: normalized.ValidFrom.UTC(), ValidUntil: normalized.ValidUntil.UTC(),
		Revision: 1, GrantedBy: strings.TrimSpace(actorID), CreatedAt: now, UpdatedAt: now}
	return u.repo.Create(ctx, grant)
}

func (u *UseCases) List(ctx context.Context, tenantID, actorID, actorRole, userID string) ([]Grant, error) {
	check, err := u.Check(ctx, CheckInput{TenantID: tenantID, ActorID: actorID, ActorRole: actorRole, Permission: "rbac.read"})
	if err != nil {
		return nil, err
	}
	if !check.Allowed {
		return nil, domainerr.Forbidden(check.Reason)
	}
	return u.repo.List(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(userID))
}

func (u *UseCases) Revoke(ctx context.Context, tenantID, actorID, actorRole string, id uuid.UUID, in RevokeInput) (Grant, error) {
	if !ownerOrAdmin(actorRole) {
		return Grant{}, domainerr.Forbidden("role grants require an owner or admin")
	}
	if in.ExpectedRevision < 1 {
		return Grant{}, domainerr.Validation("expected_revision is required")
	}
	return u.repo.Revoke(ctx, strings.TrimSpace(tenantID), id, strings.TrimSpace(actorID), strings.TrimSpace(in.Reason), in.ExpectedRevision)
}

func (u *UseCases) Check(ctx context.Context, in CheckInput) (CheckResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.ActorRole = strings.ToLower(strings.TrimSpace(in.ActorRole))
	in.Permission = strings.TrimSpace(in.Permission)
	if in.TenantID == "" || in.ActorID == "" || in.Permission == "" {
		return CheckResult{}, domainerr.Validation("tenant, actor and permission are required")
	}
	if ownerOrAdmin(in.ActorRole) {
		return checkedResult(in, nil, "membership role grants permission"), nil
	}
	grants, err := u.repo.ActiveForUser(ctx, in.TenantID, in.ActorID, u.now())
	if err != nil {
		return CheckResult{}, err
	}
	for _, grant := range grants {
		if grant.Matches(in, u.now()) {
			return checkedResult(in, &grant, "functional role grant permits operation"), nil
		}
	}
	return checkedResult(in, nil, "no active functional role grant permits operation"), nil
}

func checkedResult(in CheckInput, grant *Grant, reason string) CheckResult {
	payload := map[string]any{"tenant_id": in.TenantID, "actor_id": in.ActorID, "actor_role": in.ActorRole, "permission": in.Permission, "allowed": grant != nil || ownerOrAdmin(in.ActorRole)}
	out := CheckResult{Allowed: payload["allowed"].(bool), Reason: reason}
	if grant != nil {
		out.GrantID = &grant.ID
		out.GrantRevision = grant.Revision
		payload["grant_id"] = grant.ID.String()
		payload["grant_revision"] = grant.Revision
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	out.SnapshotHash = hex.EncodeToString(sum[:])
	return out
}
func ownerOrAdmin(role string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	return role == "owner" || role == "admin"
}
