package professionalauthority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type RepositoryPort interface {
	EnsureVirployee(context.Context, string, uuid.UUID) error
	GetScopePolicy(context.Context, string, uuid.UUID) (ScopePolicy, error)
	PutScopePolicy(context.Context, string, uuid.UUID, PutScopePolicyInput, string, time.Time) (ScopePolicy, error)
	CreatePolicyPack(context.Context, string, CreatePolicyPackInput, *uuid.UUID, string, time.Time) (PolicyPack, error)
	ListPolicyPacks(context.Context, string) ([]PolicyPack, error)
	GetPolicyPack(context.Context, string, uuid.UUID) (PolicyPack, error)
	GetPolicyBinding(context.Context, string, uuid.UUID) (PolicyBinding, error)
	PutPolicyBinding(context.Context, string, uuid.UUID, []uuid.UUID, int64, string, time.Time) (PolicyBinding, error)
	CreateDelegation(context.Context, string, uuid.UUID, CreateDelegationInput, string, time.Time) (Delegation, error)
	ListDelegations(context.Context, string, uuid.UUID) ([]Delegation, error)
	RevokeDelegation(context.Context, string, uuid.UUID, uuid.UUID, RevokeDelegationInput, string, time.Time) (Delegation, error)
	ReviewDelegation(context.Context, string, uuid.UUID, uuid.UUID, ReviewDelegationInput, string, time.Time) (Delegation, error)
	ResolveAuthority(context.Context, string, uuid.UUID) (ResolvedAuthority, error)
}

type DelegationAuthorizationCheck struct {
	TenantID       string
	ActorID        string
	ActorRole      string
	Permission     string
	ProductSurface string
	ActionType     string
	ResourceType   string
	ResourceID     string
	RiskClass      string
}

type DelegationAuthorizationResult struct {
	Allowed bool
	Reason  string
}

type DelegationAuthorizerPort interface {
	CheckDelegationAuthorization(context.Context, DelegationAuthorizationCheck) (DelegationAuthorizationResult, error)
}

type UseCases struct {
	repo       RepositoryPort
	now        func() time.Time
	authorizer DelegationAuthorizerPort
}

func (u *UseCases) SetDelegationAuthorizer(authorizer DelegationAuthorizerPort) {
	u.authorizer = authorizer
}

func NewUseCases(repo RepositoryPort) *UseCases {
	return &UseCases{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (u *UseCases) SetNow(now func() time.Time) {
	if now != nil {
		u.now = now
	}
}

func (u *UseCases) GetScopePolicy(ctx context.Context, tenantID string, virployeeID uuid.UUID) (ScopePolicy, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return ScopePolicy{}, err
	}
	if err := u.repo.EnsureVirployee(ctx, tenantID, virployeeID); err != nil {
		return ScopePolicy{}, err
	}
	policy, err := u.repo.GetScopePolicy(ctx, tenantID, virployeeID)
	if domainerr.IsNotFound(err) {
		return ScopePolicy{TenantID: tenantID, VirployeeID: virployeeID, OutOfScope: OutOfScopeAbstain}, nil
	}
	return policy, err
}

func (u *UseCases) PutScopePolicy(ctx context.Context, tenantID string, virployeeID uuid.UUID, input PutScopePolicyInput, actor Actor) (ScopePolicy, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return ScopePolicy{}, err
	}
	actor, err = NormalizeActor(actor)
	if err != nil {
		return ScopePolicy{}, err
	}
	input, err = NormalizeScopePolicyInput(input)
	if err != nil {
		return ScopePolicy{}, err
	}
	return u.repo.PutScopePolicy(ctx, tenantID, virployeeID, input, actor.ID, u.now())
}

func (u *UseCases) CreatePolicyPack(ctx context.Context, tenantID string, input CreatePolicyPackInput, actor Actor) (PolicyPack, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return PolicyPack{}, err
	}
	actor, err = NormalizeActor(actor)
	if err != nil {
		return PolicyPack{}, err
	}
	input, jobRoleID, err := NormalizePolicyPackInput(input)
	if err != nil {
		return PolicyPack{}, err
	}
	return u.repo.CreatePolicyPack(ctx, tenantID, input, jobRoleID, actor.ID, u.now())
}

func (u *UseCases) ListPolicyPacks(ctx context.Context, tenantID string) ([]PolicyPack, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	return u.repo.ListPolicyPacks(ctx, tenantID)
}

func (u *UseCases) GetPolicyPack(ctx context.Context, tenantID string, id uuid.UUID) (PolicyPack, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return PolicyPack{}, err
	}
	return u.repo.GetPolicyPack(ctx, tenantID, id)
}

func (u *UseCases) GetPolicyBinding(ctx context.Context, tenantID string, virployeeID uuid.UUID) (PolicyBinding, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return PolicyBinding{}, err
	}
	if err := u.repo.EnsureVirployee(ctx, tenantID, virployeeID); err != nil {
		return PolicyBinding{}, err
	}
	binding, err := u.repo.GetPolicyBinding(ctx, tenantID, virployeeID)
	if domainerr.IsNotFound(err) {
		return PolicyBinding{TenantID: tenantID, VirployeeID: virployeeID, PolicyPackIDs: []uuid.UUID{}}, nil
	}
	return binding, err
}

func (u *UseCases) PutPolicyBinding(ctx context.Context, tenantID string, virployeeID uuid.UUID, input PutPolicyBindingInput, actor Actor) (PolicyBinding, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return PolicyBinding{}, err
	}
	actor, err = NormalizeActor(actor)
	if err != nil {
		return PolicyBinding{}, err
	}
	ids, expectedRevision, err := NormalizePolicyBindingInput(input)
	if err != nil {
		return PolicyBinding{}, err
	}
	return u.repo.PutPolicyBinding(ctx, tenantID, virployeeID, ids, expectedRevision, actor.ID, u.now())
}

func (u *UseCases) CreateDelegation(ctx context.Context, tenantID string, virployeeID uuid.UUID, input CreateDelegationInput, actor Actor) (Delegation, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return Delegation{}, err
	}
	input, err = NormalizeDelegationInput(input, u.now())
	if err != nil {
		return Delegation{}, err
	}
	actor, err = u.authorizeDelegation(ctx, tenantID, actor, "delegations.write", input)
	if err != nil {
		return Delegation{}, err
	}
	return u.repo.CreateDelegation(ctx, tenantID, virployeeID, input, actor.ID, u.now())
}

func (u *UseCases) ListDelegations(ctx context.Context, tenantID string, virployeeID uuid.UUID, actors ...Actor) ([]Delegation, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	if err := u.repo.EnsureVirployee(ctx, tenantID, virployeeID); err != nil {
		return nil, err
	}
	items, err := u.repo.ListDelegations(ctx, tenantID, virployeeID)
	if err != nil || len(actors) == 0 {
		return items, err
	}
	actor := actors[0]
	if strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin") {
		return items, nil
	}
	visible := make([]Delegation, 0, len(items))
	for _, item := range items {
		if _, err := u.authorizeDelegation(ctx, tenantID, actor, "delegations.read", delegationAsCreateInput(item)); err == nil {
			visible = append(visible, item)
		} else if !domainerr.IsForbidden(err) {
			return nil, err
		}
	}
	return visible, nil
}

func (u *UseCases) RevokeDelegation(ctx context.Context, tenantID string, virployeeID, delegationID uuid.UUID, input RevokeDelegationInput, actor Actor) (Delegation, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return Delegation{}, err
	}
	target, err := u.findDelegation(ctx, tenantID, virployeeID, delegationID)
	if err != nil {
		return Delegation{}, err
	}
	actor, err = u.authorizeDelegation(ctx, tenantID, actor, "delegations.revoke", delegationAsCreateInput(target))
	if err != nil {
		return Delegation{}, err
	}
	input, err = NormalizeRevocationInput(input)
	if err != nil {
		return Delegation{}, err
	}
	return u.repo.RevokeDelegation(ctx, tenantID, virployeeID, delegationID, input, actor.ID, u.now())
}

func (u *UseCases) ReviewDelegation(ctx context.Context, tenantID string, virployeeID, delegationID uuid.UUID, input ReviewDelegationInput, actor Actor) (Delegation, error) {
	tenantID, err := normalizeTenantID(tenantID)
	if err != nil {
		return Delegation{}, err
	}
	target, err := u.findDelegation(ctx, tenantID, virployeeID, delegationID)
	if err != nil {
		return Delegation{}, err
	}
	actor, err = u.authorizeDelegation(ctx, tenantID, actor, "delegations.write", delegationAsCreateInput(target))
	if err != nil {
		return Delegation{}, err
	}
	input, err = NormalizeReviewInput(input)
	if err != nil {
		return Delegation{}, err
	}
	return u.repo.ReviewDelegation(ctx, tenantID, virployeeID, delegationID, input, actor.ID, u.now())
}

func (u *UseCases) authorizeDelegation(ctx context.Context, tenantID string, actor Actor, permission string, input CreateDelegationInput) (Actor, error) {
	actor.ID = strings.TrimSpace(actor.ID)
	actor.Role = strings.ToLower(strings.TrimSpace(actor.Role))
	if actor.ID == "" {
		return Actor{}, domainerr.Validation("actor is required")
	}
	if actor.Role == "owner" || actor.Role == "admin" {
		return actor, nil
	}
	if u.authorizer == nil {
		return Actor{}, domainerr.Forbidden("delegation authorization is unavailable")
	}
	check := DelegationAuthorizationCheck{TenantID: tenantID, ActorID: actor.ID, ActorRole: actor.Role, Permission: permission, RiskClass: input.MaxRiskClass}
	if check.RiskClass == "" {
		check.RiskClass = "low"
	}
	check.ProductSurface = singleScope(input.ProductScopes)
	check.ActionType = singleScope(input.CapabilityScopes)
	if len(input.ResourceScopes) == 1 {
		check.ResourceType, check.ResourceID = input.ResourceScopes[0].ResourceType, input.ResourceScopes[0].ResourceID
	} else if len(input.ResourceScopes) > 1 {
		check.ResourceType, check.ResourceID = "*", "*"
	}
	result, err := u.authorizer.CheckDelegationAuthorization(ctx, check)
	if err != nil {
		return Actor{}, err
	}
	if !result.Allowed {
		return Actor{}, domainerr.Forbidden(result.Reason)
	}
	return actor, nil
}

func singleScope(values []string) string {
	if len(values) == 1 {
		return values[0]
	}
	if len(values) > 1 {
		return "*"
	}
	return ""
}

func (u *UseCases) findDelegation(ctx context.Context, tenantID string, virployeeID, delegationID uuid.UUID) (Delegation, error) {
	items, err := u.repo.ListDelegations(ctx, tenantID, virployeeID)
	if err != nil {
		return Delegation{}, err
	}
	for _, item := range items {
		if item.ID == delegationID {
			return item, nil
		}
	}
	return Delegation{}, domainerr.NotFound("delegation not found")
}

func delegationAsCreateInput(item Delegation) CreateDelegationInput {
	return CreateDelegationInput{PrincipalType: item.PrincipalType, PrincipalID: item.PrincipalID,
		CapabilityScopes: item.CapabilityScopes, ProductScopes: item.ProductScopes, ResourceScopes: item.ResourceScopes,
		MaxRiskClass: item.MaxRiskClass, Purpose: item.Purpose, ValidFrom: &item.ValidFrom, ValidUntil: item.ValidUntil}
}

// EvaluateAuthority implements virployees.AuthorityEvaluatorPort. Every result
// includes a deterministic metadata-only snapshot hash, including denied
// results, so traces can explain exactly which revisions were evaluated.
func (u *UseCases) EvaluateAuthority(ctx context.Context, input executiongate.AuthorityCheckInput) (executiongate.AuthorityCheckResult, error) {
	tenantID, err := normalizeTenantID(input.TenantID)
	if err != nil {
		return executiongate.AuthorityCheckResult{}, err
	}
	capability := strings.ToLower(strings.TrimSpace(input.CapabilityKey))
	if input.VirployeeID == uuid.Nil || capability == "" {
		return executiongate.AuthorityCheckResult{}, domainerr.Validation("virployee_id and capability_key are required for authority evaluation")
	}
	resolved, err := u.repo.ResolveAuthority(ctx, tenantID, input.VirployeeID)
	if err != nil {
		return executiongate.AuthorityCheckResult{}, err
	}
	if input.JobRoleID != uuid.Nil && input.JobRoleID != resolved.JobRoleID {
		return executiongate.AuthorityCheckResult{}, domainerr.Conflict("authority job role does not match virployee")
	}
	principal, err := executiongate.NormalizePrincipalContext(executiongate.PrincipalContext{
		Type: input.PrincipalType, ID: input.PrincipalID,
	})
	if err != nil {
		return executiongate.AuthorityCheckResult{}, domainerr.Validation(err.Error())
	}
	at := input.At.UTC()
	if at.IsZero() {
		at = u.now()
	}

	refs := make([]PolicyReference, 0, len(resolved.PolicyPacks))
	delegationRequired := false
	allowed, reason := true, "professional authority permits this capability"
	for _, pack := range resolved.PolicyPacks {
		refs = append(refs, PolicyReference{ID: pack.ID.String(), Version: pack.Version, Revision: pack.Revision})
		for _, pattern := range pack.Rules.ProhibitedCapabilities {
			if MatchesCapability(pattern, capability) {
				allowed, reason = false, "professional policy prohibits this capability"
			}
		}
		if allowed && len(pack.Rules.AllowedCapabilities) > 0 && !matchesAny(pack.Rules.AllowedCapabilities, capability) {
			allowed, reason = false, "professional policy does not allow this capability"
		}
		delegationRequired = delegationRequired || pack.Rules.DelegationRequired
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })

	var selected *Delegation
	if allowed && delegationRequired {
		if principal.Type == "" {
			allowed, reason = false, "principal context is required for delegated authority"
		} else {
			resourceType, resourceID := strings.ToLower(strings.TrimSpace(input.ResourceType)), strings.TrimSpace(input.ResourceID)
			if resourceType == "" && resourceID == "" {
				resourceType, resourceID = principal.Type, principal.ID
			}
			candidates := make([]Delegation, 0)
			for _, delegation := range resolved.Delegations {
				if delegation.ActiveAt(at) && delegation.PrincipalType == principal.Type &&
					delegation.PrincipalID == principal.ID && matchesAny(delegation.CapabilityScopes, capability) &&
					delegation.MatchesProduct(input.ProductSurface) && delegation.MatchesResource(resourceType, resourceID) &&
					delegation.AllowsRisk(input.RiskClass) {
					candidates = append(candidates, delegation)
				}
			}
			sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID.String() < candidates[j].ID.String() })
			if len(candidates) == 0 {
				allowed, reason = false, "a current delegation for the requested principal is required for this capability"
			} else {
				selected = &candidates[0]
			}
		}
	}

	policyRevisionHash, err := revisionHash(resolved.BindingRevision, refs)
	if err != nil {
		return executiongate.AuthorityCheckResult{}, err
	}
	snapshot := Snapshot{
		TenantID: tenantID, VirployeeID: input.VirployeeID.String(), JobRoleID: resolved.JobRoleID.String(),
		CapabilityKey: capability, ScopeRevision: resolved.Scope.Revision,
		BindingRevision: resolved.BindingRevision, PolicyPacks: refs,
		PrincipalType: principal.Type, PrincipalID: principal.ID,
		ProductSurface: strings.ToLower(strings.TrimSpace(input.ProductSurface)), ResourceType: strings.ToLower(strings.TrimSpace(input.ResourceType)),
		ResourceID: strings.TrimSpace(input.ResourceID), RiskClass: strings.ToLower(strings.TrimSpace(input.RiskClass)),
	}
	if selected != nil {
		snapshot.DelegationID = selected.ID.String()
		snapshot.DelegationRevision = selected.Revision
		snapshot.DelegationConditionsHash = selected.ConditionsHash()
	}
	snapshotHash, err := snapshot.Hash()
	if err != nil {
		return executiongate.AuthorityCheckResult{}, err
	}
	return executiongate.AuthorityCheckResult{
		Allowed: allowed, Reason: reason, SnapshotHash: snapshotHash,
		ScopeRevision: resolved.Scope.Revision, PolicyRevisionHash: policyRevisionHash,
		DelegationRequired: delegationRequired, DelegationID: snapshot.DelegationID,
		DelegationRevision: snapshot.DelegationRevision,
	}, nil
}

// EvaluateConversationScope applies the intersection of the Virployee scope
// and every effective professional policy pack. Prohibitions always win;
// non-empty allowlists must each match. The query itself is never returned or
// persisted, only its hash participates in the decision snapshot.
func (u *UseCases) EvaluateConversationScope(ctx context.Context, input executiongate.ConversationScopeInput) (executiongate.ConversationScopeResult, error) {
	tenantID, err := normalizeTenantID(input.TenantID)
	if err != nil {
		return executiongate.ConversationScopeResult{}, err
	}
	query := strings.TrimSpace(input.Query)
	if input.VirployeeID == uuid.Nil || query == "" {
		return executiongate.ConversationScopeResult{}, domainerr.Validation("virployee_id and query are required for conversation scope evaluation")
	}
	resolved, err := u.repo.ResolveAuthority(ctx, tenantID, input.VirployeeID)
	if err != nil {
		return executiongate.ConversationScopeResult{}, err
	}
	if input.JobRoleID != uuid.Nil && input.JobRoleID != resolved.JobRoleID {
		return executiongate.ConversationScopeResult{}, domainerr.Conflict("conversation scope job role does not match virployee")
	}

	refs := make([]PolicyReference, 0, len(resolved.PolicyPacks))
	outOfScope := resolved.Scope.OutOfScope
	if outOfScope == "" {
		outOfScope = OutOfScopeAbstain
	}
	prohibitedSets := [][]string{resolved.Scope.ProhibitedTopics}
	allowedSets := make([][]string, 0, len(resolved.PolicyPacks)+1)
	if len(resolved.Scope.AllowedTopics) > 0 {
		allowedSets = append(allowedSets, resolved.Scope.AllowedTopics)
	}
	for _, pack := range resolved.PolicyPacks {
		refs = append(refs, PolicyReference{ID: pack.ID.String(), Version: pack.Version, Revision: pack.Revision})
		prohibitedSets = append(prohibitedSets, pack.Rules.ProhibitedTopics)
		if len(pack.Rules.AllowedTopics) > 0 {
			allowedSets = append(allowedSets, pack.Rules.AllowedTopics)
		}
		if pack.Rules.OutOfScope == OutOfScopeEscalate {
			outOfScope = OutOfScopeEscalate
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })

	allowed, reason := true, "within_professional_scope"
	for _, topics := range prohibitedSets {
		if matchesTopicSet(query, topics) {
			allowed, reason = false, "prohibited_topic"
			break
		}
	}
	if allowed {
		if len(allowedSets) == 0 {
			allowed, reason = false, "outside_allowed_topics"
		} else {
			for _, topics := range allowedSets {
				if !matchesTopicSet(query, topics) {
					allowed, reason = false, "outside_allowed_topics"
					break
				}
			}
		}
	}
	decision := "allow"
	if !allowed {
		decision = string(outOfScope)
	}
	policyRevisionHash, err := revisionHash(resolved.BindingRevision, refs)
	if err != nil {
		return executiongate.ConversationScopeResult{}, err
	}
	snapshot := Snapshot{
		TenantID: tenantID, VirployeeID: input.VirployeeID.String(), JobRoleID: resolved.JobRoleID.String(),
		CapabilityKey: "conversation.scope", ScopeRevision: resolved.Scope.Revision,
		BindingRevision: resolved.BindingRevision, PolicyPacks: refs, InputHash: hashString(query),
	}
	snapshotHash, err := snapshot.Hash()
	if err != nil {
		return executiongate.ConversationScopeResult{}, err
	}
	return executiongate.ConversationScopeResult{
		Allowed: allowed, Decision: decision, Reason: reason, SnapshotHash: snapshotHash,
		ScopeRevision: resolved.Scope.Revision, PolicyRevisionHash: policyRevisionHash,
	}, nil
}

func normalizeTenantID(tenantID string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", domainerr.Validation("tenant_id is required")
	}
	return tenantID, nil
}

func matchesAny(patterns []string, capability string) bool {
	for _, pattern := range patterns {
		if MatchesCapability(pattern, capability) {
			return true
		}
	}
	return false
}

func matchesTopicSet(query string, topics []string) bool {
	normalizedQuery := " " + normalizeTopicText(query) + " "
	for _, topic := range topics {
		if strings.TrimSpace(topic) == "*" {
			return true
		}
		normalizedTopic := normalizeTopicText(topic)
		if normalizedTopic != "" && strings.Contains(normalizedQuery, " "+normalizedTopic+" ") {
			return true
		}
	}
	return false
}

func normalizeTopicText(value string) string {
	var builder strings.Builder
	space := true
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			builder.WriteRune(r)
			space = false
			continue
		}
		if !space {
			builder.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func revisionHash(bindingRevision int64, refs []PolicyReference) (string, error) {
	raw, err := json.Marshal(struct {
		BindingRevision int64             `json:"binding_revision"`
		PolicyPacks     []PolicyReference `json:"policy_packs"`
	}{bindingRevision, refs})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
