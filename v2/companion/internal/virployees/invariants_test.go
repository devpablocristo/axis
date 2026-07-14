package virployees

// Guardian suite for the governance "foso" (Fase 0, PR 1).
//
// These tests do not exercise new behavior; they pin the safety invariants of
// the existing dry-run → execution-gate → governance → execution loop so that a
// future change that quietly weakens the moat fails CI. Each test names the
// invariant it protects. If one of these breaks, the fix is almost never the
// test — it is the change that removed a safety property.

import (
	"context"
	"errors"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const executableInput = "Agendá una reunión mañana a las 15 con ana@example.com"

// Invariant 1 — Fail-closed at the execution gate.
// The gate passes if and ONLY if governance explicitly allows. Missing
// governance, a governance error, deny, and require_approval must all block,
// and the gate must never mark WillExecute (it is a simulation).
func TestInvariantExecutionGatePassesOnlyOnGovernanceAllow(t *testing.T) {
	cases := []struct {
		name      string
		configure func(uc *UseCases)
		wantPass  bool
	}{
		{
			name:      "no governance configured blocks",
			configure: func(uc *UseCases) { uc.SetGovernanceChecker(nil) },
			wantPass:  false,
		},
		{
			name:      "governance error blocks",
			configure: func(uc *UseCases) { uc.SetGovernanceChecker(&fakeGovernanceChecker{err: errors.New("nexus down")}) },
			wantPass:  false,
		},
		{
			name: "deny blocks",
			configure: func(uc *UseCases) {
				uc.SetGovernanceChecker(&fakeGovernanceChecker{result: executiongate.GovernanceCheckResult{Decision: "deny", Status: "denied"}})
			},
			wantPass: false,
		},
		{
			name: "require_approval blocks",
			configure: func(uc *UseCases) {
				uc.SetGovernanceChecker(&fakeGovernanceChecker{result: executiongate.GovernanceCheckResult{Decision: "require_approval", Status: "pending_approval", ApprovalID: "approval-1", ApprovalStatus: "pending"}})
			},
			wantPass: false,
		},
		{
			name: "allow passes",
			configure: func(uc *UseCases) {
				uc.SetGovernanceChecker(&fakeGovernanceChecker{result: executiongate.GovernanceCheckResult{Decision: "allow", Status: "allowed"}})
			},
			wantPass: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
			tc.configure(uc)

			result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, executableInput, nil)
			if err != nil {
				t.Fatalf("ExecutionGate: %v", err)
			}
			// Guard the premise: the local gate must reach governance, otherwise
			// this test would pass vacuously and stop protecting the invariant.
			if result.DryRun.Draft.Status != dryrun.DraftStatusReady {
				t.Fatalf("premise broken: expected a ready draft that reaches governance, got %+v", result.DryRun.Draft)
			}
			if result.Gate.WillExecute {
				t.Fatalf("execution gate must never set WillExecute (simulation only), got %+v", result.Gate)
			}
			gotPass := result.Gate.Decision == executiongate.DecisionPass
			if gotPass != tc.wantPass {
				t.Fatalf("gate decision = %q, wantPass=%v", result.Gate.Decision, tc.wantPass)
			}
			if !tc.wantPass {
				assertExecutionGateCheck(t, result.Gate.Checks, "governance_check", executiongate.CheckStatusBlocked)
			}
		})
	}
}

// Invariant 2 — The governance check defaults to Blocked.
// Any decision other than the literal "allow" (including empty/unknown values)
// must append a blocked governance check and flip a passing gate to blocked.
func TestInvariantGovernanceCheckDefaultsToBlocked(t *testing.T) {
	blocking := []string{"", "unknown", "maybe", "deny", "require_approval", "ALLOW", "Allow"}
	for _, decision := range blocking {
		base := executiongate.Result{Gate: executiongate.Gate{Decision: executiongate.DecisionPass, Checks: []executiongate.Check{}}}
		got := executiongate.ApplyGovernance(base, executiongate.GovernanceCheckResult{Decision: decision})
		if got.Gate.Decision != executiongate.DecisionBlocked {
			t.Fatalf("decision %q: expected gate blocked, got %q", decision, got.Gate.Decision)
		}
		last := got.Gate.Checks[len(got.Gate.Checks)-1]
		if last.Key != "governance_check" || last.Status != executiongate.CheckStatusBlocked {
			t.Fatalf("decision %q: expected blocked governance_check, got %+v", decision, last)
		}
	}

	base := executiongate.Result{Gate: executiongate.Gate{Decision: executiongate.DecisionPass, Checks: []executiongate.Check{}}}
	got := executiongate.ApplyGovernance(base, executiongate.GovernanceCheckResult{Decision: "allow"})
	if got.Gate.Decision != executiongate.DecisionPass {
		t.Fatalf("allow must keep the gate passing, got %q", got.Gate.Decision)
	}
	last := got.Gate.Checks[len(got.Gate.Checks)-1]
	if last.Status != executiongate.CheckStatusPass {
		t.Fatalf("allow must append a passing governance_check, got %+v", last)
	}
}

// Invariant 3 — The action binding carries every load-bearing field.
// The binding hash seals exactly what was evaluated. Dropping any of these keys
// silently narrows what an approval is pinned to, so the set is frozen here.
func TestInvariantActionBindingCarriesAllFields(t *testing.T) {
	result := dryrun.Result{
		Input: "schedule a meeting with ana@example.com",
		Intent: dryrun.Intent{
			Matched:       true,
			CapabilityKey: "calendar.events.create",
			Domain:        "calendar",
			Resource:      "events",
			Action:        "create",
		},
		RuntimeContext: runtimecontext.Context{
			Virployee:         domain.Virployee{ID: uuid.New()},
			MemoryContextHash: "mem-hash",
		},
	}

	binding := actionBinding("tenant-1", result, nil)
	if binding == nil {
		t.Fatal("expected a binding for a matched intent")
	}
	requiredKeys := []string{
		"schema_version", "tenant_id", "virployee_id", "operation",
		"capability_key", "action", "target_system", "target_resource",
		"input_hash", "memory_context_hash",
	}
	for _, key := range requiredKeys {
		value, ok := binding[key]
		if !ok {
			t.Fatalf("binding is missing required field %q: %+v", key, binding)
		}
		if s, isStr := value.(string); isStr && s == "" {
			t.Fatalf("binding field %q must not be empty", key)
		}
	}

	// A prepared action must add its schema + payload hash so the approval is
	// pinned to the concrete payload, not just the intent.
	draft := dryrun.Draft{
		Status: dryrun.DraftStatusReady,
		Action: "calendar.events.create",
		Kind:   "calendar_event",
		Fields: []dryrun.DraftField{
			{Key: "title", Value: "Reunión"},
			{Key: "date", Value: "2026-07-15"},
			{Key: "time", Value: "15:00"},
			{Key: "timezone", Value: "America/Argentina/Buenos_Aires"},
			{Key: "duration_minutes", Value: "60"},
			{Key: "attendees", Value: "ana@example.com"},
		},
	}
	prepared, err := preparedactions.FromDraft(draft)
	if err != nil {
		t.Fatalf("FromDraft: %v", err)
	}
	withPrepared := actionBinding("tenant-1", result, &prepared)
	for _, key := range []string{"prepared_action_schema", "prepared_action_hash"} {
		if _, ok := withPrepared[key]; !ok {
			t.Fatalf("binding with prepared action is missing %q: %+v", key, withPrepared)
		}
	}

	// No matched intent means no binding at all (nothing to pin).
	unmatched := actionBinding("tenant-1", dryrun.Result{Intent: dryrun.Intent{Matched: false}}, nil)
	if unmatched != nil {
		t.Fatalf("expected nil binding for unmatched intent, got %+v", unmatched)
	}
}

// Invariant 4 — Approved execution re-verifies the binding.
// ExecuteApprovedAction must refuse (and never invoke the executor) when the
// prepared action's binding hash OR governance check id does not match the
// approval. Only a full match may reach the executor.
func TestInvariantExecuteApprovedActionRebindsBeforeExecuting(t *testing.T) {
	checkID := uuid.New()
	const bindingHash = "binding-hash-abc"

	newCase := func(prepared PreparedActionRecord) (*UseCases, *fakeActionExecutor, uuid.UUID, uuid.UUID) {
		repo := &fakeExecRepo{fakeRepo: newFakeRepo(), prepared: prepared, beginCreated: true}
		uc, err := NewUseCases(repo)
		if err != nil {
			t.Fatal(err)
		}
		created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
			Name:              "Sofia",
			JobRoleID:         uuid.NewString(),
			ProfileTemplateID: uuid.NewString(),
			SupervisorUserID:  "dev-user",
			Autonomy:          "A3",
		})
		if err != nil {
			t.Fatal(err)
		}
		approvalID := uuid.New()
		uc.SetApprovalReader(&fakeApprovalReader{approval: executiongate.GovernanceApproval{
			ID:                approvalID.String(),
			GovernanceCheckID: checkID.String(),
			RequesterID:       created.ID.String(),
			BindingHash:       bindingHash,
			Status:            "approved",
		}})
		executor := &fakeActionExecutor{resourceID: "res-1"}
		uc.RegisterExecutor("calendar.events.create", executor)
		return uc, executor, created.ID, approvalID
	}

	preparedTemplate := func() PreparedActionRecord {
		return PreparedActionRecord{
			ID:                uuid.New(),
			TenantID:          "tenant-1",
			GovernanceCheckID: checkID,
			BindingHash:       bindingHash,
			Action:            preparedactions.Action{SchemaVersion: preparedactions.SchemaVersion, Action: "calendar.events.create"},
		}
	}

	t.Run("binding hash mismatch is refused without executing", func(t *testing.T) {
		prepared := preparedTemplate()
		prepared.BindingHash = "tampered-binding"
		uc, executor, id, approvalID := newCase(prepared)

		_, err := uc.ExecuteApprovedAction(context.Background(), "tenant-1", id, approvalID)
		if !domainerr.IsConflict(err) {
			t.Fatalf("expected conflict on binding mismatch, got %v", err)
		}
		if executor.called {
			t.Fatal("executor must not run when the binding hash does not match")
		}
	})

	t.Run("governance check id mismatch is refused without executing", func(t *testing.T) {
		prepared := preparedTemplate()
		prepared.GovernanceCheckID = uuid.New() // different from the approval's check id
		uc, executor, id, approvalID := newCase(prepared)

		_, err := uc.ExecuteApprovedAction(context.Background(), "tenant-1", id, approvalID)
		if !domainerr.IsConflict(err) {
			t.Fatalf("expected conflict on governance check id mismatch, got %v", err)
		}
		if executor.called {
			t.Fatal("executor must not run when the governance check id does not match")
		}
	})

	t.Run("full match reaches the executor", func(t *testing.T) {
		prepared := preparedTemplate()
		uc, executor, id, approvalID := newCase(prepared)
		repo := uc.executionRepo.(*fakeExecRepo)
		// Short-circuit the post-execution trace lookup so the test focuses on
		// the binding gate, not trace assembly.
		repo.existingExecTrace = &runtraces.Trace{
			ID:              uuid.New(),
			ExecutionResult: &runtraces.ExecutionResult{Status: "succeeded"},
		}

		if _, err := uc.ExecuteApprovedAction(context.Background(), "tenant-1", id, approvalID); err != nil {
			t.Fatalf("ExecuteApprovedAction with matching binding: %v", err)
		}
		if !executor.called {
			t.Fatal("executor must run when the binding fully matches the approval")
		}
	})
}

// --- fakes used only by invariant 4 (execution path) ---

type fakeActionExecutor struct {
	called     bool
	resourceID string
}

func (e *fakeActionExecutor) Execute(context.Context, string, uuid.UUID, ExecutionAttempt, preparedactions.Action) (string, map[string]any, error) {
	e.called = true
	return e.resourceID, map[string]any{"mode": "local"}, nil
}

// fakeExecRepo embeds fakeRepo (RepositoryPort) and adds ExecutionRepositoryPort
// so NewUseCases wires it as the execution repository.
type fakeExecRepo struct {
	*fakeRepo
	prepared          PreparedActionRecord
	preparedErr       error
	beginCreated      bool
	existingExecTrace *runtraces.Trace
}

func (r *fakeExecRepo) FindExecutionTraceByApproval(_ context.Context, _ string, _ uuid.UUID, _ string) (runtraces.Trace, error) {
	if r.existingExecTrace != nil {
		return *r.existingExecTrace, nil
	}
	return runtraces.Trace{}, domainerr.NotFound("execution trace not found")
}

func (r *fakeExecRepo) SavePreparedAction(_ context.Context, _ string, _ uuid.UUID, _, _ string, _, _, _ string, _ preparedactions.Action) (PreparedActionRecord, error) {
	return r.prepared, nil
}

func (r *fakeExecRepo) GetPreparedActionByApproval(_ context.Context, _ string, _, _ uuid.UUID) (PreparedActionRecord, error) {
	if r.preparedErr != nil {
		return PreparedActionRecord{}, r.preparedErr
	}
	return r.prepared, nil
}

func (r *fakeExecRepo) BeginExecution(_ context.Context, _ string, _ uuid.UUID, preparedActionID uuid.UUID, idempotencyKey string) (ExecutionAttempt, bool, error) {
	return ExecutionAttempt{ID: uuid.New(), PreparedActionID: preparedActionID, IdempotencyKey: idempotencyKey}, r.beginCreated, nil
}

func (r *fakeExecRepo) GetExecutionByPreparedAction(_ context.Context, _ string, _ uuid.UUID) (ExecutionAttempt, error) {
	return ExecutionAttempt{}, domainerr.NotFound("execution not found")
}

func (r *fakeExecRepo) CompleteExecution(_ context.Context, _ string, id uuid.UUID, status, resourceID string, result map[string]any, executionError string, durationMS int64) (ExecutionAttempt, error) {
	return ExecutionAttempt{ID: id, Status: status, ResourceID: resourceID, Result: result, Error: executionError, DurationMS: durationMS}, nil
}

func (r *fakeExecRepo) CreateLocalCalendarEvent(_ context.Context, _ string, _ uuid.UUID, _ ExecutionAttempt, _ preparedactions.Action) (string, error) {
	return "res-1", nil
}

func (r *fakeExecRepo) SetNexusReportStatus(_ context.Context, _ string, _ uuid.UUID, _ string) error {
	return nil
}
