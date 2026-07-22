package professionalauthority

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type OutOfScope string

const (
	OutOfScopeAbstain  OutOfScope = "abstain"
	OutOfScopeEscalate OutOfScope = "escalate"
)

type Actor struct {
	ID   string
	Role string
}

type ScopePolicy struct {
	OrgID            string
	VirployeeID      uuid.UUID
	AllowedTopics    []string
	ProhibitedTopics []string
	OutOfScope       OutOfScope
	Revision         int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PutScopePolicyInput struct {
	AllowedTopics    []string
	ProhibitedTopics []string
	OutOfScope       OutOfScope
	ExpectedRevision int64
}

type PolicyRules struct {
	AllowedTopics          []string   `json:"allowed_topics"`
	ProhibitedTopics       []string   `json:"prohibited_topics"`
	OutOfScope             OutOfScope `json:"out_of_scope"`
	AllowedCapabilities    []string   `json:"allowed_capabilities"`
	ProhibitedCapabilities []string   `json:"prohibited_capabilities"`
	DelegationRequired     bool       `json:"delegation_required"`
}

type PolicyPack struct {
	ID        uuid.UUID
	OrgID     string
	PolicyKey string
	Name      string
	Version   int
	JobRoleID *uuid.UUID
	Rules     PolicyRules
	Revision  int64
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreatePolicyPackInput struct {
	PolicyKey string
	Name      string
	Version   int
	JobRoleID string
	Rules     PolicyRules
}

type PolicyBinding struct {
	OrgID         string
	VirployeeID   uuid.UUID
	PolicyPackIDs []uuid.UUID
	Revision      int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PutPolicyBindingInput struct {
	PolicyPackIDs    []string
	ExpectedRevision int64
}

type Delegation struct {
	ID               uuid.UUID
	OrgID            string
	VirployeeID      uuid.UUID
	PrincipalType    string
	PrincipalID      string
	CapabilityScopes []string
	ProductScopes    []string
	ResourceScopes   []ResourceScope
	MaxRiskClass     string
	Purpose          string
	GrantedBy        string
	ValidFrom        time.Time
	ValidUntil       time.Time
	Revision         int64
	RevokedAt        *time.Time
	RevokedBy        string
	RevocationReason string
	ReviewedAt       *time.Time
	ReviewedBy       string
	ReviewNote       string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ResourceScope struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

func (d Delegation) ActiveAt(at time.Time) bool {
	return d.RevokedAt == nil && !at.Before(d.ValidFrom) && at.Before(d.ValidUntil)
}

type CreateDelegationInput struct {
	PrincipalType    string
	PrincipalID      string
	CapabilityScopes []string
	ProductScopes    []string
	ResourceScopes   []ResourceScope
	MaxRiskClass     string
	Purpose          string
	ValidFrom        *time.Time
	ValidUntil       time.Time
}

type ReviewDelegationInput struct {
	ExpectedRevision int64
	Note             string
}

type RevokeDelegationInput struct {
	ExpectedRevision int64
	Reason           string
}

type ResolvedAuthority struct {
	OrgID           string
	VirployeeID     uuid.UUID
	JobRoleID       uuid.UUID
	Scope           ScopePolicy
	BindingRevision int64
	PolicyPacks     []PolicyPack
	Delegations     []Delegation
}

type Snapshot struct {
	OrgID                    string            `json:"org_id"`
	VirployeeID              string            `json:"virployee_id"`
	JobRoleID                string            `json:"job_role_id"`
	CapabilityKey            string            `json:"capability_key"`
	ProductSurface           string            `json:"product_surface,omitempty"`
	ResourceType             string            `json:"resource_type,omitempty"`
	ResourceID               string            `json:"resource_id,omitempty"`
	RiskClass                string            `json:"risk_class,omitempty"`
	ScopeRevision            int64             `json:"scope_revision"`
	BindingRevision          int64             `json:"binding_revision"`
	PolicyPacks              []PolicyReference `json:"policy_packs"`
	DelegationID             string            `json:"delegation_id"`
	DelegationRevision       int64             `json:"delegation_revision"`
	PrincipalType            string            `json:"principal_type,omitempty"`
	PrincipalID              string            `json:"principal_id,omitempty"`
	InputHash                string            `json:"input_hash,omitempty"`
	DelegationConditionsHash string            `json:"delegation_conditions_hash,omitempty"`
}

type PolicyReference struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Revision int64  `json:"revision"`
}

func (s Snapshot) Hash() (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func NormalizeActor(actor Actor) (Actor, error) {
	actor.ID = strings.TrimSpace(actor.ID)
	actor.Role = strings.ToLower(strings.TrimSpace(actor.Role))
	if actor.ID == "" {
		return Actor{}, domainerr.Validation("actor is required")
	}
	if actor.Role != "owner" && actor.Role != "admin" {
		return Actor{}, domainerr.Forbidden("professional authority changes require an owner or admin")
	}
	return actor, nil
}

func NormalizeScopePolicyInput(in PutScopePolicyInput) (PutScopePolicyInput, error) {
	var err error
	in.AllowedTopics, err = normalizeStrings(in.AllowedTopics, 100, 160)
	if err != nil {
		return PutScopePolicyInput{}, domainerr.Validation("allowed_topics contains an invalid topic")
	}
	in.ProhibitedTopics, err = normalizeStrings(in.ProhibitedTopics, 100, 160)
	if err != nil {
		return PutScopePolicyInput{}, domainerr.Validation("prohibited_topics contains an invalid topic")
	}
	if overlap(in.AllowedTopics, in.ProhibitedTopics) {
		return PutScopePolicyInput{}, domainerr.Validation("a topic cannot be both allowed and prohibited")
	}
	if in.OutOfScope == "" {
		in.OutOfScope = OutOfScopeAbstain
	}
	if in.OutOfScope != OutOfScopeAbstain && in.OutOfScope != OutOfScopeEscalate {
		return PutScopePolicyInput{}, domainerr.Validation("out_of_scope must be abstain or escalate")
	}
	if in.ExpectedRevision < 0 {
		return PutScopePolicyInput{}, domainerr.Validation("expected_revision cannot be negative")
	}
	return in, nil
}

func NormalizePolicyPackInput(in CreatePolicyPackInput) (CreatePolicyPackInput, *uuid.UUID, error) {
	in.PolicyKey = strings.ToLower(strings.TrimSpace(in.PolicyKey))
	in.Name = strings.TrimSpace(in.Name)
	if !policyKeyPattern.MatchString(in.PolicyKey) {
		return CreatePolicyPackInput{}, nil, domainerr.Validation("policy_key has an invalid format")
	}
	if in.Name == "" || len([]rune(in.Name)) > 160 {
		return CreatePolicyPackInput{}, nil, domainerr.Validation("name is required and must be at most 160 characters")
	}
	if in.Version <= 0 {
		return CreatePolicyPackInput{}, nil, domainerr.Validation("version must be greater than zero")
	}
	rules, err := normalizePolicyRules(in.Rules)
	if err != nil {
		return CreatePolicyPackInput{}, nil, err
	}
	in.Rules = rules
	var jobRoleID *uuid.UUID
	if raw := strings.TrimSpace(in.JobRoleID); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return CreatePolicyPackInput{}, nil, domainerr.Validation("job_role_id must be a UUID")
		}
		jobRoleID = &parsed
	}
	return in, jobRoleID, nil
}

func NormalizePolicyBindingInput(in PutPolicyBindingInput) ([]uuid.UUID, int64, error) {
	if in.ExpectedRevision < 0 {
		return nil, 0, domainerr.Validation("expected_revision cannot be negative")
	}
	seen := map[uuid.UUID]struct{}{}
	ids := make([]uuid.UUID, 0, len(in.PolicyPackIDs))
	for _, raw := range in.PolicyPackIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, 0, domainerr.Validation("policy_pack_ids must contain UUIDs")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) > 50 {
		return nil, 0, domainerr.Validation("at most 50 policy packs may be assigned")
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids, in.ExpectedRevision, nil
}

func NormalizeDelegationInput(in CreateDelegationInput, now time.Time) (CreateDelegationInput, error) {
	in.PrincipalType = strings.ToLower(strings.TrimSpace(in.PrincipalType))
	in.PrincipalID = strings.TrimSpace(in.PrincipalID)
	if !validPrincipalTypes[in.PrincipalType] {
		return CreateDelegationInput{}, domainerr.Validation("principal_type is invalid")
	}
	if in.PrincipalID == "" || len([]rune(in.PrincipalID)) > 256 {
		return CreateDelegationInput{}, domainerr.Validation("principal_id is required and must be at most 256 characters")
	}
	scopes, err := normalizeCapabilityPatterns(in.CapabilityScopes)
	if err != nil || len(scopes) == 0 {
		return CreateDelegationInput{}, domainerr.Validation("capability_scopes must contain at least one valid capability pattern")
	}
	in.CapabilityScopes = scopes
	in.ProductScopes, err = normalizeCapabilityPatterns(in.ProductScopes)
	if err != nil || len(in.ProductScopes) == 0 {
		return CreateDelegationInput{}, domainerr.Validation("product_scopes must contain at least one valid product pattern")
	}
	in.ResourceScopes, err = normalizeResourceScopes(in.ResourceScopes)
	if err != nil || len(in.ResourceScopes) == 0 {
		return CreateDelegationInput{}, domainerr.Validation("resource_scopes must contain at least one valid resource")
	}
	in.MaxRiskClass = strings.ToLower(strings.TrimSpace(in.MaxRiskClass))
	if riskRank[in.MaxRiskClass] == 0 {
		return CreateDelegationInput{}, domainerr.Validation("max_risk_class is invalid")
	}
	in.Purpose = strings.TrimSpace(in.Purpose)
	if in.Purpose == "" || len([]rune(in.Purpose)) > 500 {
		return CreateDelegationInput{}, domainerr.Validation("purpose is required and must be at most 500 characters")
	}
	if in.ValidFrom == nil {
		value := now.UTC()
		in.ValidFrom = &value
	} else {
		value := in.ValidFrom.UTC()
		in.ValidFrom = &value
	}
	in.ValidUntil = in.ValidUntil.UTC()
	if !in.ValidUntil.After(*in.ValidFrom) {
		return CreateDelegationInput{}, domainerr.Validation("valid_until must be after valid_from")
	}
	if !in.ValidUntil.After(now.UTC()) {
		return CreateDelegationInput{}, domainerr.Validation("valid_until must be in the future")
	}
	return in, nil
}

func NormalizeReviewInput(in ReviewDelegationInput) (ReviewDelegationInput, error) {
	in.Note = strings.TrimSpace(in.Note)
	if in.ExpectedRevision <= 0 {
		return ReviewDelegationInput{}, domainerr.Validation("expected_revision must be greater than zero")
	}
	if in.Note == "" || len([]rune(in.Note)) > 500 {
		return ReviewDelegationInput{}, domainerr.Validation("review note is required and must be at most 500 characters")
	}
	return in, nil
}

func NormalizeRevocationInput(in RevokeDelegationInput) (RevokeDelegationInput, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedRevision <= 0 {
		return RevokeDelegationInput{}, domainerr.Validation("expected_revision must be greater than zero")
	}
	if in.Reason == "" || len([]rune(in.Reason)) > 500 {
		return RevokeDelegationInput{}, domainerr.Validation("reason is required and must be at most 500 characters")
	}
	return in, nil
}

func MatchesCapability(pattern, capability string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	capability = strings.ToLower(strings.TrimSpace(capability))
	if pattern == "*" || pattern == capability {
		return true
	}
	return strings.HasSuffix(pattern, ".*") && strings.HasPrefix(capability, strings.TrimSuffix(pattern, "*"))
}

func (d Delegation) MatchesProduct(product string) bool {
	if len(d.ProductScopes) == 0 {
		return true
	}
	return matchesAny(d.ProductScopes, strings.ToLower(strings.TrimSpace(product)))
}

func (d Delegation) MatchesResource(resourceType, resourceID string) bool {
	resourceType, resourceID = strings.ToLower(strings.TrimSpace(resourceType)), strings.TrimSpace(resourceID)
	if len(d.ResourceScopes) == 0 {
		return resourceID == d.PrincipalID
	}
	for _, scope := range d.ResourceScopes {
		if (scope.ResourceType == "*" || scope.ResourceType == resourceType) && (scope.ResourceID == "*" || scope.ResourceID == resourceID) {
			return true
		}
	}
	return false
}

func (d Delegation) AllowsRisk(risk string) bool {
	risk = strings.ToLower(strings.TrimSpace(risk))
	if riskRank[risk] == 0 {
		risk = "critical"
	}
	maxRisk := strings.ToLower(strings.TrimSpace(d.MaxRiskClass))
	if riskRank[maxRisk] == 0 {
		maxRisk = "critical"
	}
	return riskRank[risk] <= riskRank[maxRisk]
}

func (d Delegation) ConditionsHash() string {
	return hashValue(map[string]any{"capability_scopes": d.CapabilityScopes, "product_scopes": d.ProductScopes,
		"resource_scopes": d.ResourceScopes, "max_risk_class": d.MaxRiskClass, "purpose": d.Purpose,
		"principal_type": d.PrincipalType, "principal_id": d.PrincipalID, "valid_from": d.ValidFrom, "valid_until": d.ValidUntil})
}

func normalizePolicyRules(rules PolicyRules) (PolicyRules, error) {
	var err error
	rules.AllowedTopics, err = normalizeStrings(rules.AllowedTopics, 100, 160)
	if err != nil {
		return PolicyRules{}, domainerr.Validation("rules.allowed_topics contains an invalid topic")
	}
	rules.ProhibitedTopics, err = normalizeStrings(rules.ProhibitedTopics, 100, 160)
	if err != nil {
		return PolicyRules{}, domainerr.Validation("rules.prohibited_topics contains an invalid topic")
	}
	if overlap(rules.AllowedTopics, rules.ProhibitedTopics) {
		return PolicyRules{}, domainerr.Validation("a policy topic cannot be both allowed and prohibited")
	}
	if rules.OutOfScope == "" {
		rules.OutOfScope = OutOfScopeAbstain
	}
	if rules.OutOfScope != OutOfScopeAbstain && rules.OutOfScope != OutOfScopeEscalate {
		return PolicyRules{}, domainerr.Validation("rules.out_of_scope must be abstain or escalate")
	}
	rules.AllowedCapabilities, err = normalizeCapabilityPatterns(rules.AllowedCapabilities)
	if err != nil {
		return PolicyRules{}, domainerr.Validation("rules.allowed_capabilities contains an invalid pattern")
	}
	rules.ProhibitedCapabilities, err = normalizeCapabilityPatterns(rules.ProhibitedCapabilities)
	if err != nil {
		return PolicyRules{}, domainerr.Validation("rules.prohibited_capabilities contains an invalid pattern")
	}
	return rules, nil
}

func normalizeStrings(in []string, maxItems, maxRunes int) ([]string, error) {
	if len(in) > maxItems {
		return nil, domainerr.Validation("too many values")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > maxRunes {
			return nil, domainerr.Validation("invalid value")
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

func normalizeCapabilityPatterns(in []string) ([]string, error) {
	if len(in) > 100 {
		return nil, domainerr.Validation("too many capability patterns")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.ToLower(strings.TrimSpace(value))
		if !capabilityPattern.MatchString(value) {
			return nil, domainerr.Validation("invalid capability pattern")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeResourceScopes(in []ResourceScope) ([]ResourceScope, error) {
	if len(in) > 50 {
		return nil, domainerr.Validation("too many resource scopes")
	}
	seen := map[string]struct{}{}
	out := make([]ResourceScope, 0, len(in))
	for _, scope := range in {
		scope.ResourceType = strings.ToLower(strings.TrimSpace(scope.ResourceType))
		scope.ResourceID = strings.TrimSpace(scope.ResourceID)
		if scope.ResourceType == "" || scope.ResourceID == "" || len(scope.ResourceType) > 100 || len(scope.ResourceID) > 256 {
			return nil, domainerr.Validation("invalid resource scope")
		}
		key := scope.ResourceType + "\x00" + scope.ResourceID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourceType == out[j].ResourceType {
			return out[i].ResourceID < out[j].ResourceID
		}
		return out[i].ResourceType < out[j].ResourceType
	})
	return out, nil
}

func hashValue(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func overlap(left, right []string) bool {
	seen := map[string]struct{}{}
	for _, value := range left {
		seen[strings.ToLower(value)] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[strings.ToLower(value)]; ok {
			return true
		}
	}
	return false
}

var (
	policyKeyPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	capabilityPattern   = regexp.MustCompile(`^(\*|[a-z0-9_-]+(?:\.[a-z0-9_-]+)*(?:\.\*)?)$`)
	validPrincipalTypes = map[string]bool{
		"person": true, "organization": true, "team": true,
		"case": true, "project": true, "service": true,
	}
	riskRank = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
)
