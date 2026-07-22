package governancepolicies

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StateDraft   = "draft"
	StateShadow  = "shadow"
	StateActive  = "active"
	StateRetired = "retired"

	EffectAllow           = "allow"
	EffectDeny            = "deny"
	EffectRequireApproval = "require_approval"
)

type Artifact struct {
	ID          uuid.UUID `json:"id"`
	TenantID    string    `json:"tenant_id"`
	PolicyKey   string    `json:"policy_key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Versions    []Version `json:"versions,omitempty"`
}

type Version struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          string     `json:"tenant_id"`
	PolicyID          uuid.UUID  `json:"policy_id"`
	Version           int        `json:"version"`
	State             string     `json:"state"`
	ProductSurface    string     `json:"product_surface,omitempty"`
	ActionTypePattern string     `json:"action_type_pattern"`
	TargetSystem      string     `json:"target_system,omitempty"`
	RequesterType     string     `json:"requester_type,omitempty"`
	Expression        string     `json:"expression"`
	Effect            string     `json:"effect"`
	RiskOverride      string     `json:"risk_override,omitempty"`
	Priority          int        `json:"priority"`
	ContentHash       string     `json:"content_hash"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	RetiredAt         *time.Time `json:"retired_at,omitempty"`
}

type CreateArtifactInput struct {
	PolicyKey   string `json:"policy_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreateVersionInput struct {
	ProductSurface    string `json:"product_surface"`
	ActionTypePattern string `json:"action_type_pattern"`
	TargetSystem      string `json:"target_system"`
	RequesterType     string `json:"requester_type"`
	Expression        string `json:"expression"`
	Effect            string `json:"effect"`
	RiskOverride      string `json:"risk_override"`
	Priority          int    `json:"priority"`
}

type Simulation struct {
	ID                   uuid.UUID `json:"id"`
	TenantID             string    `json:"tenant_id"`
	PolicyVersionID      uuid.UUID `json:"policy_version_id"`
	RequestedBy          string    `json:"requested_by"`
	TotalEvaluated       int       `json:"total_evaluated"`
	WouldMatch           int       `json:"would_match"`
	WouldAllow           int       `json:"would_allow"`
	WouldDeny            int       `json:"would_deny"`
	WouldRequireApproval int       `json:"would_require_approval"`
	ReportHash           string    `json:"report_hash"`
	CreatedAt            time.Time `json:"created_at"`
}

type Promotion struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        string     `json:"tenant_id"`
	PolicyVersionID uuid.UUID  `json:"policy_version_id"`
	SimulationID    uuid.UUID  `json:"simulation_id"`
	TargetState     string     `json:"target_state"`
	Status          string     `json:"status"`
	RequestedBy     string     `json:"requested_by"`
	DecidedBy       string     `json:"decided_by,omitempty"`
	DecisionReason  string     `json:"decision_reason,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
}

type PromotionInput struct {
	TargetState  string    `json:"target_state"`
	SimulationID uuid.UUID `json:"simulation_id"`
}

type PromotionDecisionInput struct {
	Reason string `json:"reason"`
}

type SafeInput struct {
	ProductSurface    string            `json:"product_surface"`
	ActionType        string            `json:"action_type"`
	TargetSystem      string            `json:"target_system"`
	ResourceType      string            `json:"resource_type"`
	ResourceReference string            `json:"resource_reference"`
	RiskClass         string            `json:"risk_class"`
	RequesterType     string            `json:"requester_type"`
	RequesterID       string            `json:"requester_id"`
	MembershipRole    string            `json:"membership_role"`
	FunctionalRoles   []string          `json:"functional_roles"`
	FunctionalScopes  []string          `json:"functional_scopes"`
	AuthorityHashes   map[string]string `json:"authority_hashes"`
	Now               time.Time         `json:"-"`
}

type PolicyMatch struct {
	PolicyID       uuid.UUID `json:"policy_id"`
	VersionID      uuid.UUID `json:"version_id"`
	Version        int       `json:"version"`
	Effect         string    `json:"effect"`
	Mode           string    `json:"mode"`
	Priority       int       `json:"priority"`
	RiskOverride   string    `json:"risk_override,omitempty"`
	ContentHash    string    `json:"content_hash"`
	ExpressionTrue bool      `json:"expression_true"`
}

type EvaluationResult struct {
	Matched            bool          `json:"matched"`
	Decision           string        `json:"decision,omitempty"`
	EffectiveRisk      string        `json:"effective_risk"`
	Reason             string        `json:"reason"`
	PolicySnapshotHash string        `json:"policy_snapshot_hash"`
	InputHash          string        `json:"input_hash"`
	Matches            []PolicyMatch `json:"matches"`
}

type Evaluation struct {
	ID              uuid.UUID `json:"id"`
	TenantID        string    `json:"tenant_id"`
	PolicyVersionID uuid.UUID `json:"policy_version_id"`
	Mode            string    `json:"mode"`
	Matched         bool      `json:"matched"`
	Effect          string    `json:"effect"`
	Decision        string    `json:"decision,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
	InputHash       string    `json:"input_hash"`
	CreatedAt       time.Time `json:"created_at"`
}

type Change struct {
	ID              uuid.UUID      `json:"id"`
	TenantID        string         `json:"tenant_id"`
	PolicyID        uuid.UUID      `json:"policy_id"`
	PolicyVersionID *uuid.UUID     `json:"policy_version_id,omitempty"`
	ActorID         string         `json:"actor_id"`
	Action          string         `json:"action"`
	Summary         string         `json:"summary"`
	Data            map[string]any `json:"data"`
	CreatedAt       time.Time      `json:"created_at"`
}

func NormalizeArtifact(in CreateArtifactInput) (CreateArtifactInput, error) {
	in.PolicyKey = strings.ToLower(strings.TrimSpace(in.PolicyKey))
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if !keyPattern.MatchString(in.PolicyKey) || in.Name == "" {
		return CreateArtifactInput{}, domainerr.Validation("policy_key and name are required")
	}
	return in, nil
}

func NormalizeVersion(in CreateVersionInput) (CreateVersionInput, error) {
	in.ProductSurface = strings.ToLower(strings.TrimSpace(in.ProductSurface))
	in.ActionTypePattern = strings.ToLower(strings.TrimSpace(in.ActionTypePattern))
	in.TargetSystem = strings.ToLower(strings.TrimSpace(in.TargetSystem))
	in.RequesterType = strings.ToLower(strings.TrimSpace(in.RequesterType))
	in.Expression = strings.TrimSpace(in.Expression)
	in.Effect = strings.ToLower(strings.TrimSpace(in.Effect))
	in.RiskOverride = strings.ToLower(strings.TrimSpace(in.RiskOverride))
	if in.ActionTypePattern == "" {
		in.ActionTypePattern = "*"
	}
	if !actionPattern.MatchString(in.ActionTypePattern) {
		return CreateVersionInput{}, domainerr.Validation("action_type_pattern is invalid")
	}
	if in.Expression == "" {
		in.Expression = "true"
	}
	if len(in.Expression) > 4096 {
		return CreateVersionInput{}, domainerr.Validation("expression is too large")
	}
	if in.Effect != EffectAllow && in.Effect != EffectDeny && in.Effect != EffectRequireApproval {
		return CreateVersionInput{}, domainerr.Validation("effect is invalid")
	}
	if in.RiskOverride != "" && riskRank[in.RiskOverride] == 0 {
		return CreateVersionInput{}, domainerr.Validation("risk_override is invalid")
	}
	if in.Priority == 0 {
		in.Priority = 100
	}
	return in, nil
}

func ContentHash(in CreateVersionInput) string {
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func Hash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func RaiseRisk(current, candidate string) string {
	current = normalizeRisk(current)
	candidate = normalizeRisk(candidate)
	if riskRank[candidate] > riskRank[current] {
		return candidate
	}
	return current
}

func RiskRequiresApproval(risk string) bool { return riskRank[normalizeRisk(risk)] >= riskRank["high"] }

func normalizeRisk(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if riskRank[value] == 0 {
		return "critical"
	}
	return value
}

func MatchesPattern(pattern, value string) bool {
	pattern, value = strings.ToLower(strings.TrimSpace(pattern)), strings.ToLower(strings.TrimSpace(value))
	return pattern == "*" || pattern == value || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(value, strings.TrimSuffix(pattern, "*")))
}

func (v Version) AppliesTo(in SafeInput) bool {
	return (v.ProductSurface == "" || v.ProductSurface == strings.ToLower(strings.TrimSpace(in.ProductSurface))) &&
		MatchesPattern(v.ActionTypePattern, in.ActionType) &&
		(v.TargetSystem == "" || v.TargetSystem == strings.ToLower(strings.TrimSpace(in.TargetSystem))) &&
		(v.RequesterType == "" || v.RequesterType == strings.ToLower(strings.TrimSpace(in.RequesterType)))
}

var (
	keyPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,126}[a-z0-9]$`)
	actionPattern = regexp.MustCompile(`^(\*|[a-z0-9_-]+(?:\.[a-z0-9_-]+)*(?:\.\*)?)$`)
	riskRank      = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
)
