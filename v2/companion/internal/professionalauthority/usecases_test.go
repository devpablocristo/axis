package professionalauthority

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestEvaluateAuthorityRequiresCurrentMatchingDelegation(t *testing.T) {
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	virployeeID, jobRoleID, packID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeRepository{resolved: map[string]ResolvedAuthority{
		"tenant-a": {
			TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
			Scope: ScopePolicy{Revision: 3}, BindingRevision: 4,
			PolicyPacks: []PolicyPack{{
				ID: packID, Version: 2, Revision: 1,
				Rules: PolicyRules{AllowedCapabilities: []string{"calendar.events.*"}, DelegationRequired: true},
			}},
			Delegations: []Delegation{{
				ID: uuid.New(), Revision: 2, CapabilityScopes: []string{"calendar.events.*"},
				PrincipalType: "person", PrincipalID: "patient-a",
				ValidFrom: now.Add(-2 * time.Hour), ValidUntil: now,
			}},
		},
	}}
	uc := NewUseCases(repo)
	uc.SetNow(func() time.Time { return now })

	input := executiongate.AuthorityCheckInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		CapabilityKey: "calendar.events.create", PrincipalType: "person", PrincipalID: "patient-a", At: now,
	}
	denied, err := uc.EvaluateAuthority(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if denied.Allowed || !denied.DelegationRequired || denied.DelegationID != "" {
		t.Fatalf("expired delegation must fail closed, got %+v", denied)
	}

	valid := Delegation{
		ID: uuid.New(), Revision: 5, CapabilityScopes: []string{"calendar.events.create"},
		PrincipalType: "person", PrincipalID: "patient-a",
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
	}
	resolved := repo.resolved["tenant-a"]
	resolved.Delegations = append(resolved.Delegations, valid)
	repo.resolved["tenant-a"] = resolved
	allowed, err := uc.EvaluateAuthority(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Allowed || allowed.DelegationID != valid.ID.String() || allowed.DelegationRevision != 5 {
		t.Fatalf("current delegation should allow, got %+v", allowed)
	}
	if allowed.ScopeRevision != 3 || allowed.PolicyRevisionHash == "" || allowed.SnapshotHash == "" {
		t.Fatalf("authority revisions must be bound, got %+v", allowed)
	}

	resolved.Scope.Revision++
	repo.resolved["tenant-a"] = resolved
	changed, err := uc.EvaluateAuthority(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.SnapshotHash == allowed.SnapshotHash {
		t.Fatal("scope revision change must invalidate the authority snapshot")
	}
}

func TestEvaluateAuthoritySelectsDelegationForExactPrincipal(t *testing.T) {
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	virployeeID, jobRoleID := uuid.New(), uuid.New()
	delegationA := Delegation{
		ID: uuid.New(), PrincipalType: "person", PrincipalID: "patient-a", Revision: 2,
		CapabilityScopes: []string{"records.read"}, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
	}
	delegationB := Delegation{
		ID: uuid.New(), PrincipalType: "person", PrincipalID: "patient-b", Revision: 4,
		CapabilityScopes: []string{"records.read"}, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour),
	}
	repo := &fakeRepository{resolved: map[string]ResolvedAuthority{
		"tenant-a": {
			TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
			PolicyPacks: []PolicyPack{{ID: uuid.New(), Version: 1, Revision: 1,
				Rules: PolicyRules{DelegationRequired: true, AllowedCapabilities: []string{"records.read"}}}},
			Delegations: []Delegation{delegationA, delegationB},
		},
	}}
	uc := NewUseCases(repo)
	uc.SetNow(func() time.Time { return now })

	evaluate := func(principalID string) executiongate.AuthorityCheckResult {
		t.Helper()
		result, err := uc.EvaluateAuthority(context.Background(), executiongate.AuthorityCheckInput{
			TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
			CapabilityKey: "records.read", PrincipalType: "person", PrincipalID: principalID, At: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	selectedA, selectedB := evaluate("patient-a"), evaluate("patient-b")
	if !selectedA.Allowed || selectedA.DelegationID != delegationA.ID.String() || selectedA.DelegationRevision != 2 {
		t.Fatalf("patient A must select only delegation A: %+v", selectedA)
	}
	if !selectedB.Allowed || selectedB.DelegationID != delegationB.ID.String() || selectedB.DelegationRevision != 4 {
		t.Fatalf("patient B must select only delegation B: %+v", selectedB)
	}
	if selectedA.SnapshotHash == selectedB.SnapshotHash {
		t.Fatal("different principals must produce different authority snapshots")
	}

	missing, err := uc.EvaluateAuthority(context.Background(), executiongate.AuthorityCheckInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		CapabilityKey: "records.read", At: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Allowed || missing.DelegationID != "" {
		t.Fatalf("delegation-required action without principal must block: %+v", missing)
	}
	wrong := evaluate("patient-c")
	if wrong.Allowed || wrong.DelegationID != "" {
		t.Fatalf("an unrelated principal must not borrow another delegation: %+v", wrong)
	}
}

func TestEvaluateAuthorityKeepsLegacyActionWhenDelegationNotRequired(t *testing.T) {
	virployeeID, jobRoleID := uuid.New(), uuid.New()
	uc := NewUseCases(&fakeRepository{resolved: map[string]ResolvedAuthority{
		"tenant-a": {TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID},
	}})
	result, err := uc.EvaluateAuthority(context.Background(), executiongate.AuthorityCheckInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID, CapabilityKey: "calendar.events.read",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Allowed || result.DelegationRequired {
		t.Fatalf("legacy action without delegation requirement must remain allowed: %+v", result)
	}
}

func TestEvaluateAuthorityRequiresProductResourceAndRiskScopes(t *testing.T) {
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	virployeeID, jobRoleID := uuid.New(), uuid.New()
	delegation := Delegation{ID: uuid.New(), Revision: 1, PrincipalType: "person", PrincipalID: "patient-a",
		CapabilityScopes: []string{"records.read"}, ProductScopes: []string{"clinical"},
		ResourceScopes: []ResourceScope{{ResourceType: "case", ResourceID: "case-a"}}, MaxRiskClass: "medium",
		Purpose: "review case records", ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	resolved := ResolvedAuthority{TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		PolicyPacks: []PolicyPack{{ID: uuid.New(), Version: 1, Revision: 1,
			Rules: PolicyRules{DelegationRequired: true, AllowedCapabilities: []string{"records.read"}}}},
		Delegations: []Delegation{delegation}}
	uc := NewUseCases(&fakeRepository{resolved: map[string]ResolvedAuthority{"tenant-a": resolved}})
	uc.SetNow(func() time.Time { return now })
	base := executiongate.AuthorityCheckInput{TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		CapabilityKey: "records.read", PrincipalType: "person", PrincipalID: "patient-a", ProductSurface: "clinical",
		ResourceType: "case", ResourceID: "case-a", RiskClass: "medium", At: now}
	allowed, err := uc.EvaluateAuthority(context.Background(), base)
	if err != nil || !allowed.Allowed {
		t.Fatalf("fully scoped delegation should allow: %+v err=%v", allowed, err)
	}
	for name, mutate := range map[string]func(*executiongate.AuthorityCheckInput){
		"product":  func(in *executiongate.AuthorityCheckInput) { in.ProductSurface = "finance" },
		"resource": func(in *executiongate.AuthorityCheckInput) { in.ResourceID = "case-b" },
		"risk":     func(in *executiongate.AuthorityCheckInput) { in.RiskClass = "high" },
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			result, evalErr := uc.EvaluateAuthority(context.Background(), input)
			if evalErr != nil || result.Allowed {
				t.Fatalf("scope mismatch must deny: %+v err=%v", result, evalErr)
			}
		})
	}
}

func TestEvaluateAuthorityEnforcesTenantAndPolicyCapability(t *testing.T) {
	virployeeID, jobRoleID := uuid.New(), uuid.New()
	repo := &fakeRepository{resolved: map[string]ResolvedAuthority{
		"tenant-a": {
			TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
			PolicyPacks: []PolicyPack{{ID: uuid.New(), Version: 1, Revision: 1,
				Rules: PolicyRules{ProhibitedCapabilities: []string{"records.*"}}}},
		},
	}}
	uc := NewUseCases(repo)
	result, err := uc.EvaluateAuthority(context.Background(), executiongate.AuthorityCheckInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID, CapabilityKey: "records.delete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Allowed {
		t.Fatalf("prohibited capability must be denied: %+v", result)
	}
	if repo.lastTenant != "tenant-a" {
		t.Fatalf("authority resolution lost tenant scope: %q", repo.lastTenant)
	}
	_, err = uc.EvaluateAuthority(context.Background(), executiongate.AuthorityCheckInput{
		TenantID: "tenant-b", VirployeeID: virployeeID, JobRoleID: jobRoleID, CapabilityKey: "records.delete",
	})
	if !domainerr.IsNotFound(err) {
		t.Fatalf("cross-tenant resolution must not reuse authority, got %v", err)
	}
}

func TestEvaluateConversationScopeAppliesProhibitionsAndOutOfScopeDecision(t *testing.T) {
	virployeeID, jobRoleID := uuid.New(), uuid.New()
	repo := &fakeRepository{resolved: map[string]ResolvedAuthority{
		"tenant-a": {
			TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
			Scope: ScopePolicy{
				AllowedTopics:    []string{"turnos"},
				ProhibitedTopics: []string{"diagnostico"},
				OutOfScope:       OutOfScopeEscalate,
				Revision:         7,
			},
			BindingRevision: 3,
			PolicyPacks: []PolicyPack{{
				ID: uuid.New(), Version: 2, Revision: 4,
				Rules: PolicyRules{AllowedTopics: []string{"turnos", "agenda"}},
			}},
		},
	}}
	uc := NewUseCases(repo)

	allowed, err := uc.EvaluateConversationScope(context.Background(), executiongate.ConversationScopeInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		Query: "Necesito cambiar mis turnos para mañana",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Allowed || allowed.Decision != "allow" || allowed.Reason != "within_professional_scope" {
		t.Fatalf("expected query in both allowlists to pass, got %+v", allowed)
	}

	prohibited, err := uc.EvaluateConversationScope(context.Background(), executiongate.ConversationScopeInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		Query: "Decime mi diagnostico ahora",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prohibited.Allowed || prohibited.Decision != "escalate" || prohibited.Reason != "prohibited_topic" {
		t.Fatalf("prohibition must win and use the strictest decision, got %+v", prohibited)
	}

	outside, err := uc.EvaluateConversationScope(context.Background(), executiongate.ConversationScopeInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		Query: "Contame el pronostico del tiempo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outside.Allowed || outside.Decision != "escalate" || outside.Reason != "outside_allowed_topics" {
		t.Fatalf("outside query must fail closed, got %+v", outside)
	}
	if outside.ScopeRevision != 7 || outside.PolicyRevisionHash == "" || outside.SnapshotHash == "" {
		t.Fatalf("scope decisions must bind policy revisions, got %+v", outside)
	}
}

func TestEvaluateConversationScopeSnapshotHashesButDoesNotExposeQuery(t *testing.T) {
	virployeeID, jobRoleID := uuid.New(), uuid.New()
	resolved := ResolvedAuthority{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID,
		Scope: ScopePolicy{AllowedTopics: []string{"agenda"}, OutOfScope: OutOfScopeAbstain, Revision: 1},
	}
	repo := &fakeRepository{resolved: map[string]ResolvedAuthority{"tenant-a": resolved}}
	uc := NewUseCases(repo)
	query := "agenda confidencial paciente 123"

	first, err := uc.EvaluateConversationScope(context.Background(), executiongate.ConversationScopeInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID, Query: query,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved.Scope.Revision = 2
	repo.resolved["tenant-a"] = resolved
	second, err := uc.EvaluateConversationScope(context.Background(), executiongate.ConversationScopeInput{
		TenantID: "tenant-a", VirployeeID: virployeeID, JobRoleID: jobRoleID, Query: query,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotHash == second.SnapshotHash {
		t.Fatal("scope revision change must invalidate the conversation snapshot")
	}
	encoded, _ := json.Marshal(first)
	if strings.Contains(string(encoded), query) {
		t.Fatalf("conversation result must not expose the query: %s", encoded)
	}
}

func TestAuthorityMutationsRequireOwnerOrAdmin(t *testing.T) {
	repo := &fakeRepository{}
	uc := NewUseCases(repo)
	_, err := uc.PutScopePolicy(context.Background(), "tenant-a", uuid.New(), PutScopePolicyInput{
		OutOfScope: OutOfScopeAbstain,
	}, Actor{ID: "user-a", Role: "member"})
	if !domainerr.IsForbidden(err) {
		t.Fatalf("member mutation must be forbidden, got %v", err)
	}
	if repo.putScopeCalls != 0 {
		t.Fatal("forbidden mutation must not reach repository")
	}
}

func TestNormalizeDelegationRejectsExpiredWindow(t *testing.T) {
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	_, err := NormalizeDelegationInput(CreateDelegationInput{
		PrincipalType: "person", PrincipalID: "patient-a", CapabilityScopes: []string{"records.read"},
		ValidUntil: now,
	}, now)
	if !domainerr.IsValidation(err) {
		t.Fatalf("expired delegation must be rejected, got %v", err)
	}
}

func TestDelegationAdminRevokeIsAuthorizedAgainstTargetScope(t *testing.T) {
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	virployeeID, delegationID := uuid.New(), uuid.New()
	delegation := Delegation{ID: delegationID, VirployeeID: virployeeID, PrincipalType: "person", PrincipalID: "patient-a",
		CapabilityScopes: []string{"records.read"}, ProductScopes: []string{"clinical"},
		ResourceScopes: []ResourceScope{{ResourceType: "case", ResourceID: "case-a"}}, MaxRiskClass: "medium",
		Purpose: "case review", ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), Revision: 2}
	repo := &fakeRepository{delegations: []Delegation{delegation}}
	authorizer := &fakeDelegationAuthorizer{result: DelegationAuthorizationResult{Allowed: true}}
	uc := NewUseCases(repo)
	uc.SetDelegationAuthorizer(authorizer)
	uc.SetNow(func() time.Time { return now })

	_, err := uc.RevokeDelegation(context.Background(), "tenant-a", virployeeID, delegationID,
		RevokeDelegationInput{ExpectedRevision: 2, Reason: "authority ended"}, Actor{ID: "delegate-admin", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.revokeCalls != 1 || authorizer.last.ProductSurface != "clinical" || authorizer.last.ActionType != "records.read" ||
		authorizer.last.ResourceType != "case" || authorizer.last.ResourceID != "case-a" || authorizer.last.RiskClass != "medium" {
		t.Fatalf("target delegation scope was not enforced: check=%+v revoke_calls=%d", authorizer.last, repo.revokeCalls)
	}

	authorizer.result = DelegationAuthorizationResult{Allowed: false, Reason: "outside grant scope"}
	_, err = uc.RevokeDelegation(context.Background(), "tenant-a", virployeeID, delegationID,
		RevokeDelegationInput{ExpectedRevision: 2, Reason: "retry"}, Actor{ID: "delegate-admin", Role: "member"})
	if !domainerr.IsForbidden(err) || repo.revokeCalls != 1 {
		t.Fatalf("out-of-scope revoke reached repository: err=%v calls=%d", err, repo.revokeCalls)
	}
}

type fakeRepository struct {
	resolved      map[string]ResolvedAuthority
	lastTenant    string
	putScopeCalls int
	delegations   []Delegation
	revokeCalls   int
}

func (r *fakeRepository) EnsureVirployee(context.Context, string, uuid.UUID) error { return nil }
func (r *fakeRepository) GetScopePolicy(context.Context, string, uuid.UUID) (ScopePolicy, error) {
	return ScopePolicy{}, domainerr.NotFound("not found")
}
func (r *fakeRepository) PutScopePolicy(_ context.Context, tenant string, id uuid.UUID, in PutScopePolicyInput, _ string, now time.Time) (ScopePolicy, error) {
	r.putScopeCalls++
	return ScopePolicy{TenantID: tenant, VirployeeID: id, AllowedTopics: in.AllowedTopics, ProhibitedTopics: in.ProhibitedTopics, OutOfScope: in.OutOfScope, Revision: 1, CreatedAt: now, UpdatedAt: now}, nil
}
func (r *fakeRepository) CreatePolicyPack(context.Context, string, CreatePolicyPackInput, *uuid.UUID, string, time.Time) (PolicyPack, error) {
	return PolicyPack{}, nil
}
func (r *fakeRepository) ListPolicyPacks(context.Context, string) ([]PolicyPack, error) {
	return nil, nil
}
func (r *fakeRepository) GetPolicyPack(context.Context, string, uuid.UUID) (PolicyPack, error) {
	return PolicyPack{}, nil
}
func (r *fakeRepository) GetPolicyBinding(context.Context, string, uuid.UUID) (PolicyBinding, error) {
	return PolicyBinding{}, domainerr.NotFound("not found")
}
func (r *fakeRepository) PutPolicyBinding(context.Context, string, uuid.UUID, []uuid.UUID, int64, string, time.Time) (PolicyBinding, error) {
	return PolicyBinding{}, nil
}
func (r *fakeRepository) CreateDelegation(context.Context, string, uuid.UUID, CreateDelegationInput, string, time.Time) (Delegation, error) {
	return Delegation{}, nil
}
func (r *fakeRepository) ListDelegations(context.Context, string, uuid.UUID) ([]Delegation, error) {
	return r.delegations, nil
}
func (r *fakeRepository) RevokeDelegation(context.Context, string, uuid.UUID, uuid.UUID, RevokeDelegationInput, string, time.Time) (Delegation, error) {
	r.revokeCalls++
	return r.delegations[0], nil
}

type fakeDelegationAuthorizer struct {
	result DelegationAuthorizationResult
	err    error
	last   DelegationAuthorizationCheck
}

func (f *fakeDelegationAuthorizer) CheckDelegationAuthorization(_ context.Context, in DelegationAuthorizationCheck) (DelegationAuthorizationResult, error) {
	f.last = in
	return f.result, f.err
}
func (r *fakeRepository) ReviewDelegation(context.Context, string, uuid.UUID, uuid.UUID, ReviewDelegationInput, string, time.Time) (Delegation, error) {
	return Delegation{}, nil
}
func (r *fakeRepository) ResolveAuthority(_ context.Context, tenant string, _ uuid.UUID) (ResolvedAuthority, error) {
	r.lastTenant = tenant
	resolved, ok := r.resolved[tenant]
	if !ok {
		return ResolvedAuthority{}, domainerr.NotFound("not found")
	}
	return resolved, nil
}
