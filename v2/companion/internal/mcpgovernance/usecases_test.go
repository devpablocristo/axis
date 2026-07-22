package mcpgovernance

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type fakeRepository struct {
	policy     Policy
	resolved   InvocationContext
	resolveErr error
	audits     []InvocationAudit
	completed  []string
	reserveErr error
}

func (f *fakeRepository) GetPolicy(context.Context, string) (Policy, error) { return f.policy, nil }
func (f *fakeRepository) PutPolicy(context.Context, string, string, PutPolicyInput) (Policy, error) {
	return f.policy, nil
}
func (f *fakeRepository) ListPolicyAudit(context.Context, string, int) ([]PolicyAudit, error) {
	return nil, nil
}
func (f *fakeRepository) ResolveContext(context.Context, ContextRequest) (InvocationContext, error) {
	return f.resolved, f.resolveErr
}
func (f *fakeRepository) ReserveInvocation(_ context.Context, audit InvocationAudit, _, _ int) error {
	f.audits = append(f.audits, audit)
	return f.reserveErr
}
func (f *fakeRepository) CompleteInvocation(_ context.Context, _ string, _ uuid.UUID, status, _, _, _ string, _ int64) error {
	f.completed = append(f.completed, status)
	return nil
}
func (f *fakeRepository) ListInvocations(context.Context, string, uuid.UUID, int) ([]InvocationAudit, error) {
	return f.audits, nil
}

type fakeCatalog struct{ items []capabilitydomain.Capability }

func (f fakeCatalog) ListActive(context.Context, string) ([]capabilitydomain.Capability, error) {
	return f.items, nil
}

type fakeVirployees struct{ item virployeedomain.Virployee }

func (f fakeVirployees) Get(context.Context, string, uuid.UUID) (virployeedomain.Virployee, error) {
	return f.item, nil
}

type fakeAuthority struct {
	allowed bool
	hash    string
}

func (f fakeAuthority) EvaluateAuthority(context.Context, executiongate.AuthorityCheckInput) (executiongate.AuthorityCheckResult, error) {
	return executiongate.AuthorityCheckResult{Allowed: f.allowed, SnapshotHash: f.hash}, nil
}

type fakeWriteGate struct {
	last WriteGateInput
	out  WriteGateResult
}

func (f *fakeWriteGate) SupportsMCPAction(string) bool { return true }

func (f *fakeWriteGate) PrepareMCPAction(_ context.Context, input WriteGateInput) (WriteGateResult, error) {
	f.last = input
	return f.out, nil
}

type fakeReader struct{ result map[string]any }

func (f fakeReader) Execute(context.Context, InvocationContext, capabilitydomain.Capability, map[string]any) (map[string]any, error) {
	return f.result, nil
}

func TestListToolsUsesCapabilitiesAndEffectivePolicy(t *testing.T) {
	uc, repo, capability, _, request := testUseCases(t, "read")
	uc.RegisterReadExecutor(capability.CapabilityKey, fakeReader{result: map[string]any{"items": []any{}}})
	tools, err := uc.ListTools(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != capability.CapabilityKey || !tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if len(repo.audits) != 1 || len(repo.completed) != 1 || repo.completed[0] != "succeeded" {
		t.Fatalf("list must be audited: audits=%+v completed=%+v", repo.audits, repo.completed)
	}

	repo.policy.DeniedCapabilities = []string{"calendar.*"}
	tools, err = uc.ListTools(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("denylist must win, got %+v", tools)
	}
}

func TestListToolsHidesCapabilityWithoutRegisteredExecutor(t *testing.T) {
	uc, _, _, _, request := testUseCases(t, "read")
	tools, err := uc.ListTools(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("capability without an executor must not be advertised: %+v", tools)
	}
}

func TestListToolsFailsClosedOnWrongSubjectAssignment(t *testing.T) {
	uc, repo, _, _, request := testUseCases(t, "read")
	repo.resolveErr = domainerr.Forbidden("subject belongs to another Virployee")
	if _, err := uc.ListTools(context.Background(), request); !domainerr.IsForbidden(err) {
		t.Fatalf("expected assignment denial, got %v", err)
	}
}

func TestWriteRequiresIdempotencyAndReturnsPendingApproval(t *testing.T) {
	uc, repo, capability, _, request := testUseCases(t, "write")
	writeGate := &fakeWriteGate{out: WriteGateResult{Status: "pending_approval", ApprovalID: uuid.NewString(), BindingHash: "binding"}}
	uc.writeGate = writeGate
	ctx, err := uc.ResolveContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	arguments := map[string]any{"title": "Consulta"}
	if _, err := uc.CallTool(context.Background(), Invocation{Context: ctx, ToolName: capability.CapabilityKey, Arguments: arguments}); !domainerr.IsValidation(err) {
		t.Fatalf("write without idempotency must fail, got %v", err)
	}
	out, err := uc.CallTool(context.Background(), Invocation{Context: ctx, ToolName: capability.CapabilityKey, Arguments: arguments, IdempotencyKey: "stable-key"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "pending_approval" || writeGate.last.ContextHash == "" || writeGate.last.PayloadHash == "" || writeGate.last.AuthorityHash != "authority-hash" {
		t.Fatalf("unexpected governed write: out=%+v input=%+v", out, writeGate.last)
	}
	if repo.completed[len(repo.completed)-1] != "pending_approval" {
		t.Fatalf("pending approval must be audited: %+v", repo.completed)
	}
}

func TestWriteIdempotentReplayReturnsSameApprovalWithoutCallingGate(t *testing.T) {
	uc, repo, capability, _, request := testUseCases(t, "write")
	approvalID := uuid.NewString()
	repo.reserveErr = &IdempotentReplayError{Prior: InvocationAudit{Status: "pending_approval", ApprovalID: approvalID, BindingHash: "binding"}}
	gate := &fakeWriteGate{out: WriteGateResult{Status: "pending_approval", ApprovalID: "must-not-run"}}
	uc.writeGate = gate
	ctx, err := uc.ResolveContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	out, err := uc.CallTool(context.Background(), Invocation{Context: ctx, ToolName: capability.CapabilityKey, Arguments: map[string]any{"title": "Consulta"}, IdempotencyKey: "stable-key"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ApprovalID != approvalID || gate.last.Capability.ID != uuid.Nil {
		t.Fatalf("replay must reuse the prior result without invoking the gate: out=%+v gate=%+v", out, gate.last)
	}
}

func TestReadRejectsInvalidExecutorOutput(t *testing.T) {
	uc, _, capability, _, request := testUseCases(t, "read")
	uc.RegisterReadExecutor(capability.CapabilityKey, fakeReader{result: map[string]any{}})
	ctx, err := uc.ResolveContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.CallTool(context.Background(), Invocation{Context: ctx, ToolName: capability.CapabilityKey, Arguments: map[string]any{"query": "today"}})
	if !domainerr.IsConflict(err) {
		t.Fatalf("invalid output must fail closed, got %v", err)
	}
}

func TestValidateJSONSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object", "required": []any{"name"}, "additionalProperties": false,
		"properties": map[string]any{"name": map[string]any{"type": "string", "minLength": float64(1)}},
	}
	if err := ValidateJSONSchema(schema, map[string]any{"name": "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSONSchema(schema, map[string]any{"name": "", "secret": true}); err == nil {
		t.Fatal("invalid input must be rejected")
	}
}

func testUseCases(t *testing.T, sideEffect string) (*UseCases, *fakeRepository, capabilitydomain.Capability, virployeedomain.Virployee, ContextRequest) {
	t.Helper()
	tenantID := "tenant-1"
	virployeeID, subjectID, assignmentID, roleID, capabilityID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	capability := capabilitydomain.Capability{
		ID: capabilityID, TenantID: tenantID, CapabilityKey: "calendar.events." + map[string]string{"read": "read", "write": "create"}[sideEffect],
		Name: "Calendar", Description: "Calendar tool", RequiredAutonomy: virployeedomain.AutonomyA3,
		RiskClass: "medium", SideEffectClass: sideEffect, RequiresNexusApproval: sideEffect == "write",
		PromotionState: capabilitydomain.PromotionActive, ManifestHash: "manifest-hash", ConformedHash: "manifest-hash",
		Manifest: capabilitydomain.Manifest{
			Version: "1.0.0", ProductSurface: "axis",
			InputSchema:  map[string]any{"type": "object", "required": []any{map[string]string{"read": "query", "write": "title"}[sideEffect]}},
			OutputSchema: map[string]any{"type": "object", "required": []any{"items"}},
			Idempotency:  capabilitydomain.IdempotencyContract{Mode: "required", KeyFields: []string{"subject_id"}},
		},
	}
	virployee := virployeedomain.Virployee{ID: virployeeID, JobRoleID: roleID, CapabilityIDs: []uuid.UUID{capabilityID}, Autonomy: virployeedomain.AutonomyA3, CreatedAt: time.Now()}
	resolved := InvocationContext{
		TenantID: tenantID, ActorID: "actor-1", ActorRole: "owner", VirployeeID: virployeeID,
		SubjectID: subjectID, AssignmentID: assignmentID, AssignmentVersion: 3,
		PrincipalType: "person", PrincipalID: subjectID.String(),
	}
	repo := &fakeRepository{policy: DefaultPolicy(tenantID), resolved: resolved}
	repo.policy.Enabled, repo.policy.Version = true, 1
	uc := NewUseCases(repo, fakeCatalog{items: []capabilitydomain.Capability{capability}}, fakeVirployees{item: virployee}, fakeAuthority{allowed: true, hash: "authority-hash"}, nil)
	uc.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return uc, repo, capability, virployee, ContextRequest{TenantID: tenantID, ActorID: "actor-1", ActorRole: "owner", VirployeeID: virployeeID, SubjectID: subjectID}
}
