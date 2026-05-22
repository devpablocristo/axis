package tasks

import (
	"testing"

	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
)

func TestEventFromSubmitResponse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   string
	}{
		{"allowed", evGovernanceResolvedAllow},
		{" executed ", evGovernanceResolvedAllow},
		{"ALLOWED", evGovernanceResolvedAllow},
		{"denied", evGovernanceResolvedDeny},
		{"pending_approval", evGovernancePendingApproval},
	}
	for _, tc := range cases {
		ev, err := eventFromSubmitResponse(governanceclient.SubmitResponse{Status: tc.status})
		if err != nil || ev != tc.want {
			t.Fatalf("status %q: got %q %v want %q", tc.status, ev, err, tc.want)
		}
	}
	_, err := eventFromSubmitResponse(governanceclient.SubmitResponse{Status: "weird"})
	if err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestEventFromGovernanceRequestStatus(t *testing.T) {
	t.Parallel()
	ev, ok := eventFromGovernanceRequestStatus("pending_approval")
	if ok || ev != "" {
		t.Fatalf("pending: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("evaluated")
	if ok || ev != "" {
		t.Fatalf("evaluated: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("approved")
	if !ok || ev != evGovernanceResolvedAllow {
		t.Fatalf("approved: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("rejected")
	if !ok || ev != evGovernanceResolvedDeny {
		t.Fatalf("rejected: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("expired")
	if !ok || ev != evGovernanceResolvedDeny {
		t.Fatalf("expired: got %q %v", ev, ok)
	}
}

func TestEventFromGovernanceRequestStatusWithExecutionPlan(t *testing.T) {
	t.Parallel()
	ev, ok := eventFromGovernanceRequestStatusWithExecutionPlan("approved", true)
	if !ok || ev != evGovernanceResolvedAllowAwaitInput {
		t.Fatalf("approved with execution plan: got %q %v", ev, ok)
	}
}

// TestFSMContract_HandlesAllNexusStatuses garantiza que el FSM de
// Companion mapea TODOS los statuses canónicos publicados por Nexus en
// governanceclient.KnownStatuses. Si Nexus agrega un status nuevo, la
// slice crece y el test falla hasta que un dev:
//  1. Agrega el case correspondiente en task_fsm.go.
//  2. Agrega la fila en este test (expected map).
//
// Esta es la red de seguridad del contract Companion ↔ Nexus (V8 plan).
func TestFSMContract_HandlesAllNexusStatuses(t *testing.T) {
	t.Parallel()
	type expectation struct {
		event string
		apply bool
	}
	expected := map[string]expectation{
		governanceclient.StatusPending:         {"", false},
		governanceclient.StatusEvaluated:       {"", false},
		governanceclient.StatusPendingApproval: {"", false},
		governanceclient.StatusAllowed:         {evGovernanceResolvedAllow, true},
		governanceclient.StatusApproved:        {evGovernanceResolvedAllow, true},
		governanceclient.StatusExecuted:        {evGovernanceResolvedAllow, true},
		governanceclient.StatusDenied:          {evGovernanceResolvedDeny, true},
		governanceclient.StatusRejected:        {evGovernanceResolvedDeny, true},
		governanceclient.StatusExpired:         {evGovernanceResolvedDeny, true},
		governanceclient.StatusFailed:          {evGovernanceResolvedDeny, true},
		governanceclient.StatusCancelled:       {evGovernanceResolvedDeny, true},
	}

	if len(expected) != len(governanceclient.KnownStatuses) {
		t.Fatalf("contract drift: governanceclient.KnownStatuses has %d entries but this test expects %d. "+
			"Nexus may have added/removed a status — update both task_fsm.go and the expected map below.",
			len(governanceclient.KnownStatuses), len(expected))
	}

	for _, status := range governanceclient.KnownStatuses {
		exp, found := expected[status]
		if !found {
			t.Errorf("contract: governanceclient.KnownStatuses includes %q but the FSM contract test has no row for it. "+
				"Update task_fsm.go to handle it, then add the expected mapping in this test.", status)
			continue
		}
		ev, apply := eventFromGovernanceRequestStatus(status)
		if ev != exp.event || apply != exp.apply {
			t.Errorf("status %q: got (event=%q, apply=%v) want (event=%q, apply=%v)",
				status, ev, apply, exp.event, exp.apply)
		}
	}
}

// TestFSMContract_SubmitResponseCoversImmediate verifica que
// eventFromSubmitResponse maneja sin error los statuses que Nexus puede
// devolver sincrónicamente en POST /v1/requests. Si Nexus agrega un
// nuevo status inmediato, esto falla — fuerza ajuste consciente.
func TestFSMContract_SubmitResponseCoversImmediate(t *testing.T) {
	t.Parallel()
	immediate := []string{
		governanceclient.StatusAllowed,
		governanceclient.StatusApproved,
		governanceclient.StatusExecuted,
		governanceclient.StatusDenied,
		governanceclient.StatusRejected,
		governanceclient.StatusPendingApproval,
	}
	for _, s := range immediate {
		ev, err := eventFromSubmitResponse(governanceclient.SubmitResponse{Status: s})
		if err != nil || ev == "" {
			t.Errorf("submit-response status %q: got (event=%q, err=%v) — expected a defined event", s, ev, err)
		}
	}
}

func TestCompanionTaskFSM_investigateAndGovernance(t *testing.T) {
	t.Parallel()
	m := companionTaskMachine()
	to, err := m.Transition(domain.TaskStatusNew, evInvestigate)
	if err != nil || to != domain.TaskStatusInvestigating {
		t.Fatalf("investigate: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evInvestigate)
	if err != nil || to != domain.TaskStatusInvestigating {
		t.Fatalf("investigate idempotent: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evGovernancePendingApproval)
	if err != nil || to != domain.TaskStatusWaitingForApproval {
		t.Fatalf("pending: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evGovernanceResolvedAllow)
	if err != nil || to != domain.TaskStatusDone {
		t.Fatalf("allow from investigating: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusWaitingForApproval, evGovernanceResolvedAllowAwaitInput)
	if err != nil || to != domain.TaskStatusWaitingForInput {
		t.Fatalf("allow awaiting input: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusWaitingForInput, evStartExecution)
	if err != nil || to != domain.TaskStatusExecuting {
		t.Fatalf("start execution: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusExecuting, evExecutionSucceeded)
	if err != nil || to != domain.TaskStatusVerifying {
		t.Fatalf("execution succeeded: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusVerifying, evExecutionVerified)
	if err != nil || to != domain.TaskStatusDone {
		t.Fatalf("execution verified: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusFailed, evRetryExecution)
	if err != nil || to != domain.TaskStatusExecuting {
		t.Fatalf("retry execution: %q %v", to, err)
	}
}
