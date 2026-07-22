package virployees

import (
	"context"
	"testing"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

type recordingAuthorityEvaluator struct {
	input  executiongate.AuthorityCheckInput
	result executiongate.AuthorityCheckResult
}

func (f *recordingAuthorityEvaluator) EvaluateAuthority(_ context.Context, input executiongate.AuthorityCheckInput) (executiongate.AuthorityCheckResult, error) {
	f.input = input
	return f.result, nil
}

func TestAuthorityRevisionChangesActionBinding(t *testing.T) {
	virployeeID := uuid.New()
	result := dryrun.Result{
		Input: "create event",
		Intent: dryrun.Intent{
			Matched: true, CapabilityKey: "calendar.events.create", Action: "create",
			Domain: "calendar", Resource: "events",
		},
		RuntimeContext: runtimecontext.Context{Virployee: domain.Virployee{ID: virployeeID}},
	}
	first, err := bindingHashForAuthority("organization-a", result, nil, &executiongate.AuthorityCheckResult{
		Allowed: true, SnapshotHash: "snapshot-a", ScopeRevision: 1, PolicyRevisionHash: "policy-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := bindingHashForAuthority("organization-a", result, nil, &executiongate.AuthorityCheckResult{
		Allowed: true, SnapshotHash: "snapshot-b", ScopeRevision: 2, PolicyRevisionHash: "policy-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("authority revision must invalidate action binding: first=%q second=%q", first, second)
	}
}

func TestAuthorityRevalidationUsesPrincipalPersistedWithAction(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	evaluator := &recordingAuthorityEvaluator{result: executiongate.AuthorityCheckResult{Allowed: true, SnapshotHash: "snapshot-a"}}
	uc.SetAuthorityEvaluator(evaluator)
	capability := capabilitydomain.Capability{CapabilityKey: "calendar.events.create", RiskClass: "high", Manifest: capabilitydomain.Manifest{ProductSurface: "calendar"}}
	action := preparedactions.Action{PrincipalType: "person", PrincipalID: "patient-a"}
	if _, err := uc.verifyCurrentAuthority(context.Background(), "organization-1", created.ID, capability, action, "snapshot-a"); err != nil {
		t.Fatalf("verify authority: %v", err)
	}
	if evaluator.input.PrincipalType != "person" || evaluator.input.PrincipalID != "patient-a" {
		t.Fatalf("execution did not revalidate the approved principal: %+v", evaluator.input)
	}
}
