package authorization

import (
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	RolePolicyAdmin     = "policy_admin"
	RoleApprover        = "approver"
	RoleAuditor         = "auditor"
	RoleDelegationAdmin = "delegation_admin"
)

type RoleDefinition struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

var roleDefinitions = []RoleDefinition{
	{Key: RolePolicyAdmin, Description: "Create, simulate and promote governance policies", Permissions: []string{"policies.read", "policies.write", "policies.simulate", "policies.promote"}},
	{Key: RoleApprover, Description: "Read and decide scoped approvals", Permissions: []string{"approvals.read", "approvals.decide"}},
	{Key: RoleAuditor, Description: "Read governance and authority audit data", Permissions: []string{"audit.read", "policies.read", "approvals.read", "delegations.read", "rbac.read"}},
	{Key: RoleDelegationAdmin, Description: "Read and manage professional delegations", Permissions: []string{"delegations.read", "delegations.write", "delegations.revoke"}},
}

type Grant struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          string     `json:"tenant_id"`
	UserID            string     `json:"user_id"`
	RoleKey           string     `json:"role_key"`
	ProductSurface    string     `json:"product_surface,omitempty"`
	ActionTypePattern string     `json:"action_type_pattern"`
	ResourceType      string     `json:"resource_type,omitempty"`
	ResourceID        string     `json:"resource_id,omitempty"`
	MaxRiskClass      string     `json:"max_risk_class"`
	ValidFrom         time.Time  `json:"valid_from"`
	ValidUntil        time.Time  `json:"valid_until"`
	Revision          int64      `json:"revision"`
	GrantedBy         string     `json:"granted_by"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	RevokedBy         string     `json:"revoked_by,omitempty"`
	RevocationReason  string     `json:"revocation_reason,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type CreateGrantInput struct {
	UserID            string     `json:"user_id"`
	RoleKey           string     `json:"role_key"`
	ProductSurface    string     `json:"product_surface"`
	ActionTypePattern string     `json:"action_type_pattern"`
	ResourceType      string     `json:"resource_type"`
	ResourceID        string     `json:"resource_id"`
	MaxRiskClass      string     `json:"max_risk_class"`
	ValidFrom         *time.Time `json:"valid_from"`
	ValidUntil        time.Time  `json:"valid_until"`
}

type RevokeInput struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}

type CheckInput struct {
	TenantID       string `json:"-"`
	ActorID        string `json:"actor_id"`
	ActorRole      string `json:"actor_role"`
	Permission     string `json:"permission"`
	ProductSurface string `json:"product_surface"`
	ActionType     string `json:"action_type"`
	ResourceType   string `json:"resource_type"`
	ResourceID     string `json:"resource_id"`
	RiskClass      string `json:"risk_class"`
}

type CheckResult struct {
	Allowed       bool       `json:"allowed"`
	Reason        string     `json:"reason"`
	GrantID       *uuid.UUID `json:"grant_id,omitempty"`
	GrantRevision int64      `json:"grant_revision,omitempty"`
	SnapshotHash  string     `json:"snapshot_hash"`
}

func Definitions() []RoleDefinition {
	out := make([]RoleDefinition, len(roleDefinitions))
	copy(out, roleDefinitions)
	return out
}

func NormalizeCreate(in CreateGrantInput, now time.Time) (CreateGrantInput, error) {
	in.UserID = strings.TrimSpace(in.UserID)
	in.RoleKey = strings.ToLower(strings.TrimSpace(in.RoleKey))
	in.ProductSurface = strings.ToLower(strings.TrimSpace(in.ProductSurface))
	in.ActionTypePattern = strings.ToLower(strings.TrimSpace(in.ActionTypePattern))
	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	in.MaxRiskClass = strings.ToLower(strings.TrimSpace(in.MaxRiskClass))
	if in.UserID == "" || !validRole(in.RoleKey) {
		return CreateGrantInput{}, domainerr.Validation("user_id and a valid functional role are required")
	}
	if in.ActionTypePattern == "" {
		in.ActionTypePattern = "*"
	}
	if !capabilityPattern.MatchString(in.ActionTypePattern) {
		return CreateGrantInput{}, domainerr.Validation("action_type_pattern is invalid")
	}
	if _, ok := riskRank[in.MaxRiskClass]; !ok {
		in.MaxRiskClass = "critical"
	}
	if in.ValidFrom == nil {
		value := now.UTC()
		in.ValidFrom = &value
	}
	if in.ValidUntil.IsZero() || !in.ValidUntil.After(in.ValidFrom.UTC()) {
		return CreateGrantInput{}, domainerr.Validation("valid_until must be after valid_from")
	}
	return in, nil
}

func RoleHasPermission(role, permission string) bool {
	for _, definition := range roleDefinitions {
		if definition.Key != role {
			continue
		}
		for _, candidate := range definition.Permissions {
			if candidate == permission {
				return true
			}
		}
	}
	return false
}

func (g Grant) Matches(input CheckInput, now time.Time) bool {
	if g.RevokedAt != nil || now.Before(g.ValidFrom) || !now.Before(g.ValidUntil) || !RoleHasPermission(g.RoleKey, input.Permission) {
		return false
	}
	if g.ProductSurface != "" && g.ProductSurface != strings.ToLower(strings.TrimSpace(input.ProductSurface)) {
		return false
	}
	if !MatchesPattern(g.ActionTypePattern, input.ActionType) {
		return false
	}
	if g.ResourceType != "" && g.ResourceType != strings.ToLower(strings.TrimSpace(input.ResourceType)) {
		return false
	}
	if g.ResourceID != "" && g.ResourceID != "*" && g.ResourceID != strings.TrimSpace(input.ResourceID) {
		return false
	}
	return riskRank[normalizeRisk(input.RiskClass)] <= riskRank[g.MaxRiskClass]
}

func MatchesPattern(pattern, value string) bool {
	pattern, value = strings.ToLower(strings.TrimSpace(pattern)), strings.ToLower(strings.TrimSpace(value))
	return pattern == "*" || pattern == value || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(value, strings.TrimSuffix(pattern, "*")))
}

func validRole(role string) bool {
	for _, definition := range roleDefinitions {
		if definition.Key == role {
			return true
		}
	}
	return false
}

func normalizeRisk(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := riskRank[value]; !ok {
		return "critical"
	}
	return value
}

var (
	capabilityPattern = regexp.MustCompile(`^(\*|[a-z0-9_-]+(?:\.[a-z0-9_-]+)*(?:\.\*)?)$`)
	riskRank          = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
)
