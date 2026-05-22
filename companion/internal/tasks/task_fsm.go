package tasks

import (
	"fmt"
	"strings"
	"sync"

	"github.com/devpablocristo/platform/concurrency/go/fsm"

	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// Eventos de transición de tarea (valores opacos para la FSM).
const (
	evInvestigate                  = "investigate"
	evNexusPendingApproval         = "nexus_pending_approval"
	evNexusResolvedAllow           = "nexus_resolved_allow"
	evNexusResolvedAllowAwaitInput = "nexus_resolved_allow_await_input"
	evNexusResolvedDeny            = "nexus_resolved_deny"
	evStartExecution               = "start_execution"
	evRetryExecution               = "retry_execution"
	evExecutionSucceeded           = "execution_succeeded"
	evExecutionVerified            = "execution_verified"
	evExecutionFailed              = "execution_failed"
)

func normalizeNexusStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

var companionTaskMachine = sync.OnceValue(buildCompanionTaskFSM)

func buildCompanionTaskFSM() *fsm.Machine[string, string] {
	return fsm.New([]fsm.Rule[string, string]{
		{From: domain.TaskStatusNew, Event: evInvestigate, To: domain.TaskStatusInvestigating},
		{From: domain.TaskStatusInvestigating, Event: evInvestigate, To: domain.TaskStatusInvestigating},

		{From: domain.TaskStatusNew, Event: evNexusPendingApproval, To: domain.TaskStatusWaitingForApproval},
		{From: domain.TaskStatusInvestigating, Event: evNexusPendingApproval, To: domain.TaskStatusWaitingForApproval},

		{From: domain.TaskStatusNew, Event: evNexusResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusInvestigating, Event: evNexusResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusWaitingForApproval, Event: evNexusResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusNew, Event: evNexusResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},
		{From: domain.TaskStatusInvestigating, Event: evNexusResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},
		{From: domain.TaskStatusWaitingForApproval, Event: evNexusResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},

		{From: domain.TaskStatusNew, Event: evNexusResolvedDeny, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusInvestigating, Event: evNexusResolvedDeny, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusWaitingForApproval, Event: evNexusResolvedDeny, To: domain.TaskStatusFailed},

		{From: domain.TaskStatusWaitingForInput, Event: evStartExecution, To: domain.TaskStatusExecuting},
		{From: domain.TaskStatusFailed, Event: evRetryExecution, To: domain.TaskStatusExecuting},
		{From: domain.TaskStatusExecuting, Event: evExecutionSucceeded, To: domain.TaskStatusVerifying},
		{From: domain.TaskStatusVerifying, Event: evExecutionVerified, To: domain.TaskStatusDone},
		{From: domain.TaskStatusExecuting, Event: evExecutionFailed, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusVerifying, Event: evExecutionFailed, To: domain.TaskStatusFailed},
	})
}

func eventFromSubmitResponse(sub nexusclient.SubmitResponse) (string, error) {
	return eventFromSubmitResponseWithExecutionPlan(sub, false)
}

func eventFromSubmitResponseWithExecutionPlan(sub nexusclient.SubmitResponse, hasExecutionPlan bool) (string, error) {
	s := normalizeNexusStatus(sub.Status)
	switch s {
	case "allowed", "approved", "executed":
		if hasExecutionPlan {
			return evNexusResolvedAllowAwaitInput, nil
		}
		return evNexusResolvedAllow, nil
	case "denied", "rejected":
		return evNexusResolvedDeny, nil
	case "pending_approval":
		return evNexusPendingApproval, nil
	default:
		return "", fmt.Errorf("unexpected nexus status after submit: %q", sub.Status)
	}
}

// eventFromNexusRequestStatus mapea estado HTTP de Nexus a evento FSM; apply=false = sin cambio.
func eventFromNexusRequestStatus(status string) (event string, apply bool) {
	return eventFromNexusRequestStatusWithExecutionPlan(status, false)
}

func eventFromNexusRequestStatusWithExecutionPlan(status string, hasExecutionPlan bool) (event string, apply bool) {
	s := normalizeNexusStatus(status)
	switch s {
	case "pending_approval", "pending", "evaluated":
		return "", false
	case "allowed", "approved", "executed":
		if hasExecutionPlan {
			return evNexusResolvedAllowAwaitInput, true
		}
		return evNexusResolvedAllow, true
	case "denied", "rejected", "expired", "failed", "cancelled":
		return evNexusResolvedDeny, true
	default:
		return "", false
	}
}
