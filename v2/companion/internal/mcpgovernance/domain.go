package mcpgovernance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const ProtocolVersion = "2025-03-26"

type Rule struct {
	Disabled            bool     `json:"disabled"`
	AllowedCapabilities []string `json:"allowed_capabilities"`
	DeniedCapabilities  []string `json:"denied_capabilities"`
}

type Policy struct {
	TenantID               string          `json:"tenant_id"`
	Enabled                bool            `json:"enabled"`
	KillSwitch             bool            `json:"kill_switch"`
	AllowedCapabilities    []string        `json:"allowed_capabilities"`
	DeniedCapabilities     []string        `json:"denied_capabilities"`
	CapabilityKillSwitches map[string]bool `json:"capability_kill_switches"`
	MaxRiskClass           string          `json:"max_risk_class"`
	MaxCallsPerMinute      int             `json:"max_calls_per_minute"`
	MaxConcurrency         int             `json:"max_concurrency"`
	ProductRules           map[string]Rule `json:"product_rules"`
	JobRoleRules           map[string]Rule `json:"job_role_rules"`
	Version                int64           `json:"version"`
	ChangedBy              string          `json:"changed_by"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type PutPolicyInput struct {
	Enabled                bool            `json:"enabled"`
	KillSwitch             bool            `json:"kill_switch"`
	AllowedCapabilities    []string        `json:"allowed_capabilities"`
	DeniedCapabilities     []string        `json:"denied_capabilities"`
	CapabilityKillSwitches map[string]bool `json:"capability_kill_switches"`
	MaxRiskClass           string          `json:"max_risk_class"`
	MaxCallsPerMinute      int             `json:"max_calls_per_minute"`
	MaxConcurrency         int             `json:"max_concurrency"`
	ProductRules           map[string]Rule `json:"product_rules"`
	JobRoleRules           map[string]Rule `json:"job_role_rules"`
	ExpectedVersion        int64           `json:"expected_version"`
}

type PolicyAudit struct {
	ID              uuid.UUID `json:"id"`
	TenantID        string    `json:"tenant_id"`
	ActorID         string    `json:"actor_id"`
	PreviousVersion int64     `json:"previous_version"`
	NewVersion      int64     `json:"new_version"`
	PreviousPolicy  Policy    `json:"previous_policy"`
	NewPolicy       Policy    `json:"new_policy"`
	CreatedAt       time.Time `json:"created_at"`
}

type InvocationContext struct {
	TenantID             string    `json:"tenant_id"`
	ActorID              string    `json:"actor_id"`
	ActorRole            string    `json:"actor_role"`
	VirployeeID          uuid.UUID `json:"virployee_id"`
	SubjectID            uuid.UUID `json:"subject_id"`
	CaseID               uuid.UUID `json:"case_id,omitempty"`
	AssignmentID         uuid.UUID `json:"assignment_id"`
	AssignmentVersion    int64     `json:"assignment_version"`
	ProductSurface       string    `json:"product_surface,omitempty"`
	RepositoryGeneration string    `json:"repository_generation,omitempty"`
	PrincipalType        string    `json:"principal_type"`
	PrincipalID          string    `json:"principal_id"`
}

type ContextRequest struct {
	TenantID             string
	ActorID              string
	ActorRole            string
	VirployeeID          uuid.UUID
	SubjectID            uuid.UUID
	CaseID               uuid.UUID
	ProductSurface       string
	RepositoryGeneration string
}

type Tool struct {
	Name          string                      `json:"name"`
	Description   string                      `json:"description,omitempty"`
	InputSchema   map[string]any              `json:"inputSchema"`
	OutputSchema  map[string]any              `json:"outputSchema,omitempty"`
	Annotations   ToolAnnotations             `json:"annotations"`
	Meta          ToolMeta                    `json:"_meta"`
	Capability    capabilitydomain.Capability `json:"-"`
	AuthorityHash string                      `json:"-"`
}

type ToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type ToolMeta struct {
	CapabilityVersion string `json:"axis/capabilityVersion"`
	ManifestHash      string `json:"axis/manifestHash"`
	RiskClass         string `json:"axis/riskClass"`
	RequiresApproval  bool   `json:"axis/requiresApproval"`
	RollbackMode      string `json:"axis/rollbackMode"`
}

type Invocation struct {
	Context              InvocationContext
	ToolName             string
	Arguments            map[string]any
	IdempotencyKey       string
	ExpectedManifestHash string
}

type InvocationResult struct {
	Status         string         `json:"status"`
	Result         map[string]any `json:"result,omitempty"`
	ApprovalID     string         `json:"approval_id,omitempty"`
	BindingHash    string         `json:"binding_hash,omitempty"`
	DecisionReason string         `json:"decision_reason,omitempty"`
}

type InvocationAudit struct {
	ID                uuid.UUID         `json:"id"`
	Context           InvocationContext `json:"context"`
	Method            string            `json:"method"`
	CapabilityKey     string            `json:"capability_key"`
	CapabilityVersion string            `json:"capability_version"`
	ManifestHash      string            `json:"manifest_hash"`
	PolicyVersion     int64             `json:"policy_version"`
	ContextHash       string            `json:"context_hash"`
	PayloadHash       string            `json:"payload_hash"`
	IdempotencyHash   string            `json:"idempotency_hash"`
	ResultHash        string            `json:"result_hash"`
	Status            string            `json:"status"`
	BlockedBy         string            `json:"blocked_by"`
	ErrorCode         string            `json:"error_code"`
	ApprovalID        string            `json:"approval_id,omitempty"`
	BindingHash       string            `json:"binding_hash,omitempty"`
	DecisionReason    string            `json:"decision_reason,omitempty"`
	DurationMS        int64             `json:"duration_ms"`
	CreatedAt         time.Time         `json:"created_at"`
	CompletedAt       *time.Time        `json:"completed_at,omitempty"`
}

// IdempotentReplayError is returned after the database uniqueness barrier has
// proved that a write with the same stable key was already reserved.
type IdempotentReplayError struct{ Prior InvocationAudit }

func (e *IdempotentReplayError) Error() string { return "MCP write is an idempotent replay" }

func DefaultPolicy(tenantID string) Policy {
	return Policy{
		TenantID: tenantID, Enabled: false, MaxRiskClass: "high",
		MaxCallsPerMinute: 120, MaxConcurrency: 10,
		AllowedCapabilities: []string{}, DeniedCapabilities: []string{},
		CapabilityKillSwitches: map[string]bool{}, ProductRules: map[string]Rule{}, JobRoleRules: map[string]Rule{},
	}
}

func NormalizePolicyInput(in PutPolicyInput) (PutPolicyInput, error) {
	if in.ExpectedVersion < 0 {
		return PutPolicyInput{}, domainerr.Validation("expected_version cannot be negative")
	}
	var err error
	if in.AllowedCapabilities, err = normalizePatterns(in.AllowedCapabilities); err != nil {
		return PutPolicyInput{}, domainerr.Validation("allowed_capabilities contains an invalid pattern")
	}
	if in.DeniedCapabilities, err = normalizePatterns(in.DeniedCapabilities); err != nil {
		return PutPolicyInput{}, domainerr.Validation("denied_capabilities contains an invalid pattern")
	}
	if in.MaxRiskClass == "" {
		in.MaxRiskClass = "high"
	}
	in.MaxRiskClass = strings.ToLower(strings.TrimSpace(in.MaxRiskClass))
	if _, ok := riskRank[in.MaxRiskClass]; !ok {
		return PutPolicyInput{}, domainerr.Validation("max_risk_class must be low, medium, high or critical")
	}
	if in.MaxCallsPerMinute == 0 {
		in.MaxCallsPerMinute = 120
	}
	if in.MaxConcurrency == 0 {
		in.MaxConcurrency = 10
	}
	if in.MaxCallsPerMinute < 1 || in.MaxCallsPerMinute > 100000 || in.MaxConcurrency < 1 || in.MaxConcurrency > 1000 {
		return PutPolicyInput{}, domainerr.Validation("MCP limits are invalid")
	}
	if in.CapabilityKillSwitches == nil {
		in.CapabilityKillSwitches = map[string]bool{}
	}
	normalizedSwitches := make(map[string]bool, len(in.CapabilityKillSwitches))
	for key, enabled := range in.CapabilityKillSwitches {
		key = strings.ToLower(strings.TrimSpace(key))
		if !capabilityPattern.MatchString(key) || strings.HasSuffix(key, ".*") || key == "*" {
			return PutPolicyInput{}, domainerr.Validation("capability_kill_switches contains an invalid capability key")
		}
		normalizedSwitches[key] = enabled
	}
	in.CapabilityKillSwitches = normalizedSwitches
	if in.ProductRules, err = normalizeRules(in.ProductRules, false); err != nil {
		return PutPolicyInput{}, err
	}
	if in.JobRoleRules, err = normalizeRules(in.JobRoleRules, true); err != nil {
		return PutPolicyInput{}, err
	}
	return in, nil
}

func normalizeRules(rules map[string]Rule, uuidKeys bool) (map[string]Rule, error) {
	out := make(map[string]Rule, len(rules))
	for key, rule := range rules {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return nil, domainerr.Validation("MCP rule key is required")
		}
		if uuidKeys {
			if _, err := uuid.Parse(key); err != nil {
				return nil, domainerr.Validation("job_role_rules keys must be UUIDs")
			}
		}
		var err error
		if rule.AllowedCapabilities, err = normalizePatterns(rule.AllowedCapabilities); err != nil {
			return nil, domainerr.Validation("MCP rule contains an invalid allow pattern")
		}
		if rule.DeniedCapabilities, err = normalizePatterns(rule.DeniedCapabilities); err != nil {
			return nil, domainerr.Validation("MCP rule contains an invalid deny pattern")
		}
		out[key] = rule
	}
	return out, nil
}

func normalizePatterns(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !capabilityPattern.MatchString(value) {
			return nil, domainerr.Validation("invalid capability pattern")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func Matches(pattern, capability string) bool {
	pattern, capability = strings.ToLower(strings.TrimSpace(pattern)), strings.ToLower(strings.TrimSpace(capability))
	return pattern == "*" || pattern == capability || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(capability, strings.TrimSuffix(pattern, "*")))
}

func AllowsPolicy(policy Policy, capability capabilitydomain.Capability, jobRoleID uuid.UUID) (bool, string) {
	key := capability.CapabilityKey
	if !policy.Enabled {
		return false, "mcp_disabled"
	}
	if policy.KillSwitch {
		return false, "global_kill_switch"
	}
	if policy.CapabilityKillSwitches[key] {
		return false, "capability_kill_switch"
	}
	if matchesAny(policy.DeniedCapabilities, key) {
		return false, "tenant_denylist"
	}
	if len(policy.AllowedCapabilities) > 0 && !matchesAny(policy.AllowedCapabilities, key) {
		return false, "tenant_allowlist"
	}
	if riskRank[capability.RiskClass] > riskRank[policy.MaxRiskClass] {
		return false, "risk_limit"
	}
	for _, rule := range []Rule{policy.ProductRules[capability.Manifest.ProductSurface], policy.JobRoleRules[jobRoleID.String()]} {
		if rule.Disabled || matchesAny(rule.DeniedCapabilities, key) || (len(rule.AllowedCapabilities) > 0 && !matchesAny(rule.AllowedCapabilities, key)) {
			return false, "scoped_policy"
		}
	}
	return true, ""
}

func matchesAny(patterns []string, capability string) bool {
	for _, pattern := range patterns {
		if Matches(pattern, capability) {
			return true
		}
	}
	return false
}

func Hash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func HashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var (
	capabilityPattern = regexp.MustCompile(`^(\*|[a-z0-9_-]+(?:\.[a-z0-9_-]+)*(?:\.\*)?)$`)
	riskRank          = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
)
