package governance

import (
	"context"
	"strings"
	"testing"

	actiondomain "github.com/devpablocristo/nexus-v2/internal/actiontypes/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/authorization"
	"github.com/devpablocristo/nexus-v2/internal/governance/usecases/domain"
	"github.com/devpablocristo/nexus-v2/internal/governancepolicies"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestCheckAllowsLowAndMediumRisk(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{
		"low.action": {
			ActionTypeKey: "low.action",
			RiskClass:     actiondomain.RiskClassLow,
			Enabled:       true,
		},
		"medium.action": {
			ActionTypeKey: "medium.action",
			RiskClass:     actiondomain.RiskClassMedium,
			Enabled:       true,
		},
	}, recorder)

	for _, actionType := range []string{"low.action", "medium.action"} {
		out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
			RequesterID: "virployee-1",
			ActionType:  actionType,
			BindingHash: "binding-" + actionType,
		})
		if err != nil {
			t.Fatalf("Check(%s) error = %v", actionType, err)
		}
		if out.Decision != domain.DecisionAllow || out.Status != domain.StatusAllowed {
			t.Fatalf("Check(%s) = %+v, want allow/allowed", actionType, out)
		}
		if out.WouldRequireApproval {
			t.Fatalf("Check(%s) should not require approval", actionType)
		}
		if out.BindingHash != "binding-"+actionType {
			t.Fatalf("Check(%s) binding_hash = %q", actionType, out.BindingHash)
		}
	}
	if len(recorder.rows) != 2 {
		t.Fatalf("expected 2 recorded checks, got %+v", recorder.rows)
	}
}

func TestCheckRequiresApprovalForHighRisk(t *testing.T) {
	recorder := &fakeCheckRecorder{approvalID: uuid.NewString()}
	uc := NewUseCases(fakeActionTypeReader{
		"high.action": {
			ActionTypeKey: "high.action",
			RiskClass:     actiondomain.RiskClassHigh,
			Enabled:       true,
		},
	}, recorder)

	out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "high.action",
		BindingHash: "binding-high",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if out.Decision != domain.DecisionRequireApproval || out.Status != domain.StatusPendingApproval {
		t.Fatalf("Check() = %+v, want require_approval/pending_approval", out)
	}
	if !out.WouldRequireApproval {
		t.Fatal("high risk should require approval")
	}
	if out.BindingHash != "binding-high" {
		t.Fatalf("unexpected binding hash: %+v", out)
	}
	if out.ApprovalID != recorder.approvalID || out.ApprovalStatus != "pending" {
		t.Fatalf("expected approval metadata, got %+v", out)
	}
}

func TestCheckPreservesProfessionalAuthorityBinding(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{
		"medium.action": {ActionTypeKey: "medium.action", RiskClass: actiondomain.RiskClassMedium, Enabled: true},
	}, recorder)
	_, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1", ActionType: "medium.action", BindingHash: "binding-1",
		AuthorityBindingHash: "authority-1", ScopeRevision: 2, PolicyRevisionHash: "policies-1",
		DelegationRequired: true, DelegationID: "delegation-1", DelegationRevision: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.rows) != 1 {
		t.Fatalf("expected recorded governance check, got %d", len(recorder.rows))
	}
	in := recorder.rows[0].input
	if in.AuthorityBindingHash != "authority-1" || in.ScopeRevision != 2 || in.PolicyRevisionHash != "policies-1" || in.DelegationRevision != 4 {
		t.Fatalf("authority binding metadata was lost: %+v", in)
	}
}

func TestCheckDerivesFunctionalRolesFromActiveGrants(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{
		"low.action": {ActionTypeKey: "low.action", RiskClass: actiondomain.RiskClassLow, Enabled: true},
	}, recorder)
	grantID := uuid.New()
	uc.SetFunctionalRoleResolver(fakeFunctionalRoleResolver{grants: []authorization.Grant{{
		ID: grantID, RoleKey: authorization.RoleAuditor, ActionTypePattern: "*", MaxRiskClass: "critical", Revision: 3,
	}}})

	out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterType: "human", RequesterID: "user-1", ActionType: "low.action",
		FunctionalRoles: []string{authorization.RolePolicyAdmin}, FunctionalScopes: []string{"forged"},
	})
	if err != nil {
		t.Fatal(err)
	}
	roles, _ := out.RoleSnapshot["functional_roles"].([]string)
	scopes, _ := out.RoleSnapshot["functional_scopes"].([]string)
	if len(roles) != 1 || roles[0] != authorization.RoleAuditor || len(scopes) != 1 || !strings.Contains(scopes[0], grantID.String()) {
		t.Fatalf("functional authority was not server-derived: %+v", out.RoleSnapshot)
	}
}

func TestCheckRejectsUnknownActionTypes(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{}, recorder)

	_, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "missing.action",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for unknown action type, got %v", err)
	}
	if len(recorder.rows) != 0 {
		t.Fatalf("invalid checks must not be recorded, got %+v", recorder.rows)
	}
}

func TestCheckDeniesDisabledActionTypes(t *testing.T) {
	recorder := &fakeCheckRecorder{}
	uc := NewUseCases(fakeActionTypeReader{
		"disabled.action": {
			ActionTypeKey: "disabled.action",
			RiskClass:     actiondomain.RiskClassMedium,
			Enabled:       false,
		},
	}, recorder)

	out, err := uc.Check(context.Background(), "tenant-1", domain.CheckInput{
		RequesterID: "virployee-1",
		ActionType:  "disabled.action",
		BindingHash: "binding-disabled",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if out.Decision != domain.DecisionDeny || out.Status != domain.StatusDenied {
		t.Fatalf("Check() = %+v, want deny/denied", out)
	}
	if out.WouldRequireApproval || out.ApprovalID != "" || out.ApprovalStatus != "" {
		t.Fatalf("deny should not require approval, got %+v", out)
	}
	if out.BindingHash != "binding-disabled" || out.RiskLevel != "medium" {
		t.Fatalf("unexpected deny metadata: %+v", out)
	}
	if len(recorder.rows) != 1 || recorder.rows[0].result.Decision != domain.DecisionDeny {
		t.Fatalf("deny check should be recorded, got %+v", recorder.rows)
	}
}

func TestRevalidateRejectsChangedActivePolicySnapshot(t *testing.T) {
	recorder := &fakeCheckRecorder{revalidation: domain.RevalidationRecord{
		Input: domain.NormalizedCheckInput{
			RequesterType: "agent", RequesterID: "virployee-1", ActionType: "high.action",
			BindingHash: "binding-1", AuthorityBindingHash: "authority-1", ScopeRevision: 2,
			PolicyRevisionHash: "professional-1", DelegationID: "delegation-1", DelegationRevision: 4,
		},
		Decision: domain.DecisionRequireApproval, RiskLevel: "high", PolicySnapshotHash: "snapshot-a",
	}}
	uc := NewUseCases(fakeActionTypeReader{
		"high.action": {ActionTypeKey: "high.action", RiskClass: actiondomain.RiskClassHigh, Enabled: true},
	}, recorder)
	uc.SetPolicyEvaluator(fakePolicyEvaluator{result: governancepolicies.EvaluationResult{
		Matched: true, Decision: governancepolicies.EffectRequireApproval, EffectiveRisk: "high",
		PolicySnapshotHash: "snapshot-b", InputHash: "input-b",
	}})

	out, err := uc.Revalidate(context.Background(), "tenant-1", uuid.NewString(), domain.RevalidationInput{
		BindingHash: "binding-1", PolicySnapshotHash: "snapshot-a", AuthorityBindingHash: "authority-1",
		ScopeRevision: 2, PolicyRevisionHash: "professional-1", DelegationID: "delegation-1", DelegationRevision: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Valid || out.Reason != "active policy snapshot changed" || out.PolicySnapshotHash != "snapshot-b" {
		t.Fatalf("expected stale policy snapshot rejection, got %+v", out)
	}
}

func TestRevalidateRejectsExpiredOrRevokedFunctionalGrantSnapshot(t *testing.T) {
	recorder := &fakeCheckRecorder{revalidation: domain.RevalidationRecord{
		Input: domain.NormalizedCheckInput{RequesterType: "human", RequesterID: "user-1", ActionType: "low.action",
			BindingHash: "binding-1", FunctionalRoles: []string{authorization.RoleAuditor}, FunctionalScopes: []string{"grant-snapshot"}},
		Decision: domain.DecisionAllow, RiskLevel: "low", PolicySnapshotHash: "snapshot-a",
	}}
	uc := NewUseCases(fakeActionTypeReader{
		"low.action": {ActionTypeKey: "low.action", RiskClass: actiondomain.RiskClassLow, Enabled: true},
	}, recorder)
	uc.SetFunctionalRoleResolver(fakeFunctionalRoleResolver{grants: nil})
	uc.SetPolicyEvaluator(fakePolicyEvaluator{result: governancepolicies.EvaluationResult{
		EffectiveRisk: "low", PolicySnapshotHash: "snapshot-a",
	}})

	out, err := uc.Revalidate(context.Background(), "tenant-1", uuid.NewString(), domain.RevalidationInput{BindingHash: "binding-1", PolicySnapshotHash: "snapshot-a"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Valid || out.Reason != "functional role authority changed" {
		t.Fatalf("expected revoked grant snapshot rejection, got %+v", out)
	}
}

type fakeActionTypeReader map[string]actiondomain.ActionType

func (r fakeActionTypeReader) GetByKey(_ context.Context, tenantID string, key string) (actiondomain.ActionType, error) {
	row, ok := r[key]
	if !ok {
		return actiondomain.ActionType{}, domainerr.NotFound("action type not found")
	}
	row.ID = uuid.New()
	row.TenantID = tenantID
	return row, nil
}

type recordedCheck struct {
	tenantID string
	input    domain.NormalizedCheckInput
	result   domain.CheckResult
}

type fakeCheckRecorder struct {
	rows         []recordedCheck
	approvalID   string
	revalidation domain.RevalidationRecord
}

func (r *fakeCheckRecorder) GetCheckForRevalidation(_ context.Context, _, _ string) (domain.RevalidationRecord, error) {
	return r.revalidation, nil
}

type fakePolicyEvaluator struct {
	result governancepolicies.EvaluationResult
	err    error
}

func (f fakePolicyEvaluator) Evaluate(context.Context, string, governancepolicies.SafeInput) (governancepolicies.EvaluationResult, error) {
	return f.result, f.err
}

type fakeFunctionalRoleResolver struct {
	grants []authorization.Grant
	err    error
}

func (f fakeFunctionalRoleResolver) EffectiveGrants(context.Context, string, string) ([]authorization.Grant, error) {
	return f.grants, f.err
}

func (r *fakeCheckRecorder) RecordCheck(_ context.Context, tenantID string, input domain.NormalizedCheckInput, result domain.CheckResult) (domain.RecordedCheck, error) {
	r.rows = append(r.rows, recordedCheck{tenantID: tenantID, input: input, result: result})
	if result.Decision == domain.DecisionRequireApproval && r.approvalID != "" {
		return domain.RecordedCheck{ApprovalID: r.approvalID, ApprovalStatus: "pending"}, nil
	}
	return domain.RecordedCheck{}, nil
}
