package tasks

import (
	"testing"

	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

func TestEventFromSubmitResponse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   string
	}{
		{"allowed", evNexusResolvedAllow},
		{" executed ", evNexusResolvedAllow},
		{"ALLOWED", evNexusResolvedAllow},
		{"denied", evNexusResolvedDeny},
		{"pending_approval", evNexusPendingApproval},
	}
	for _, tc := range cases {
		ev, err := eventFromSubmitResponse(nexusclient.SubmitResponse{Status: tc.status})
		if err != nil || ev != tc.want {
			t.Fatalf("status %q: got %q %v want %q", tc.status, ev, err, tc.want)
		}
	}
	_, err := eventFromSubmitResponse(nexusclient.SubmitResponse{Status: "weird"})
	if err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestEventFromNexusRequestStatus(t *testing.T) {
	t.Parallel()
	ev, ok := eventFromNexusRequestStatus("pending_approval")
	if ok || ev != "" {
		t.Fatalf("pending: got %q %v", ev, ok)
	}
	ev, ok = eventFromNexusRequestStatus("evaluated")
	if ok || ev != "" {
		t.Fatalf("evaluated: got %q %v", ev, ok)
	}
	ev, ok = eventFromNexusRequestStatus("approved")
	if !ok || ev != evNexusResolvedAllow {
		t.Fatalf("approved: got %q %v", ev, ok)
	}
	ev, ok = eventFromNexusRequestStatus("rejected")
	if !ok || ev != evNexusResolvedDeny {
		t.Fatalf("rejected: got %q %v", ev, ok)
	}
	ev, ok = eventFromNexusRequestStatus("expired")
	if !ok || ev != evNexusResolvedDeny {
		t.Fatalf("expired: got %q %v", ev, ok)
	}
}

// TestFSMContract_HandlesAllNexusStatuses garantiza que el FSM de
// Companion mapea TODOS los statuses canónicos publicados por Nexus en
// nexusclient.KnownStatuses. Si Nexus agrega un status nuevo, la
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
		nexusclient.StatusPending:         {"", false},
		nexusclient.StatusEvaluated:       {"", false},
		nexusclient.StatusPendingApproval: {"", false},
		nexusclient.StatusAllowed:         {evNexusResolvedAllow, true},
		nexusclient.StatusApproved:        {evNexusResolvedAllow, true},
		nexusclient.StatusExecuted:        {evNexusResolvedAllow, true},
		nexusclient.StatusDenied:          {evNexusResolvedDeny, true},
		nexusclient.StatusRejected:        {evNexusResolvedDeny, true},
		nexusclient.StatusExpired:         {evNexusResolvedDeny, true},
		nexusclient.StatusFailed:          {evNexusResolvedDeny, true},
		nexusclient.StatusCancelled:       {evNexusResolvedDeny, true},
	}

	if len(expected) != len(nexusclient.KnownStatuses) {
		t.Fatalf("contract drift: nexusclient.KnownStatuses has %d entries but this test expects %d. "+
			"Nexus may have added/removed a status — update both task_fsm.go and the expected map below.",
			len(nexusclient.KnownStatuses), len(expected))
	}

	for _, status := range nexusclient.KnownStatuses {
		exp, found := expected[status]
		if !found {
			t.Errorf("contract: nexusclient.KnownStatuses includes %q but the FSM contract test has no row for it. "+
				"Update task_fsm.go to handle it, then add the expected mapping in this test.", status)
			continue
		}
		ev, apply := eventFromNexusRequestStatus(status)
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
		nexusclient.StatusAllowed,
		nexusclient.StatusApproved,
		nexusclient.StatusExecuted,
		nexusclient.StatusDenied,
		nexusclient.StatusRejected,
		nexusclient.StatusPendingApproval,
	}
	for _, s := range immediate {
		ev, err := eventFromSubmitResponse(nexusclient.SubmitResponse{Status: s})
		if err != nil || ev == "" {
			t.Errorf("submit-response status %q: got (event=%q, err=%v) — expected a defined event", s, ev, err)
		}
	}
}

func TestCompanionTaskFSM_investigateAndNexus(t *testing.T) {
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
	to, err = m.Transition(domain.TaskStatusInvestigating, evNexusPendingApproval)
	if err != nil || to != domain.TaskStatusWaitingForApproval {
		t.Fatalf("pending: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evNexusResolvedAllow)
	if err != nil || to != domain.TaskStatusDone {
		t.Fatalf("allow from investigating: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusWaitingForApproval, evNexusResolvedAllowAwaitInput)
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
