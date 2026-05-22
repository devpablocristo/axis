package tasks

import (
	"fmt"
	"strings"
	"sync"

	"github.com/devpablocristo/platform/concurrency/go/fsm"

	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
)

// Eventos de transición de tarea (valores opacos para la FSM).
const (
	evInvestigate                       = "investigate"
	evGovernancePendingApproval         = "governance_pending_approval"
	evGovernanceResolvedAllow           = "governance_resolved_allow"
	evGovernanceResolvedAllowAwaitInput = "governance_resolved_allow_await_input"
	evGovernanceResolvedDeny            = "governance_resolved_deny"
	evStartExecution                    = "start_execution"
	evRetryExecution                    = "retry_execution"
	evExecutionSucceeded                = "execution_succeeded"
	evExecutionVerified                 = "execution_verified"
	evExecutionFailed                   = "execution_failed"
)

func normalizeGovernanceStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

var companionTaskMachine = sync.OnceValue(buildCompanionTaskFSM)

func buildCompanionTaskFSM() *fsm.Machine[string, string] {
	return fsm.New([]fsm.Rule[string, string]{
		{From: domain.TaskStatusNew, Event: evInvestigate, To: domain.TaskStatusInvestigating},
		{From: domain.TaskStatusInvestigating, Event: evInvestigate, To: domain.TaskStatusInvestigating},

		{From: domain.TaskStatusNew, Event: evGovernancePendingApproval, To: domain.TaskStatusWaitingForApproval},
		{From: domain.TaskStatusInvestigating, Event: evGovernancePendingApproval, To: domain.TaskStatusWaitingForApproval},

		{From: domain.TaskStatusNew, Event: evGovernanceResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusInvestigating, Event: evGovernanceResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusWaitingForApproval, Event: evGovernanceResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusNew, Event: evGovernanceResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},
		{From: domain.TaskStatusInvestigating, Event: evGovernanceResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},
		{From: domain.TaskStatusWaitingForApproval, Event: evGovernanceResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},

		{From: domain.TaskStatusNew, Event: evGovernanceResolvedDeny, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusInvestigating, Event: evGovernanceResolvedDeny, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusWaitingForApproval, Event: evGovernanceResolvedDeny, To: domain.TaskStatusFailed},

		{From: domain.TaskStatusWaitingForInput, Event: evStartExecution, To: domain.TaskStatusExecuting},
		{From: domain.TaskStatusFailed, Event: evRetryExecution, To: domain.TaskStatusExecuting},
		{From: domain.TaskStatusExecuting, Event: evExecutionSucceeded, To: domain.TaskStatusVerifying},
		{From: domain.TaskStatusVerifying, Event: evExecutionVerified, To: domain.TaskStatusDone},
		{From: domain.TaskStatusExecuting, Event: evExecutionFailed, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusVerifying, Event: evExecutionFailed, To: domain.TaskStatusFailed},
	})
}

func eventFromSubmitResponse(sub governanceclient.SubmitResponse) (string, error) {
	return eventFromSubmitResponseWithExecutionPlan(sub, false)
}

func eventFromSubmitResponseWithExecutionPlan(sub governanceclient.SubmitResponse, hasExecutionPlan bool) (string, error) {
	s := normalizeGovernanceStatus(sub.Status)
	switch s {
	case "allowed", "approved", "executed":
		if hasExecutionPlan {
			return evGovernanceResolvedAllowAwaitInput, nil
		}
		return evGovernanceResolvedAllow, nil
	case "denied", "rejected":
		return evGovernanceResolvedDeny, nil
	case "pending_approval":
		return evGovernancePendingApproval, nil
	default:
		return "", fmt.Errorf("unexpected governance status after submit: %q", sub.Status)
	}
}

// eventFromGovernanceRequestStatus mapea estado HTTP de Governance a evento FSM; apply=false = sin cambio.
func eventFromGovernanceRequestStatus(status string) (event string, apply bool) {
	return eventFromGovernanceRequestStatusWithExecutionPlan(status, false)
}

func eventFromGovernanceRequestStatusWithExecutionPlan(status string, hasExecutionPlan bool) (event string, apply bool) {
	s := normalizeGovernanceStatus(status)
	switch s {
	case "pending_approval", "pending", "evaluated":
		return "", false
	case "allowed", "approved", "executed":
		if hasExecutionPlan {
			return evGovernanceResolvedAllowAwaitInput, true
		}
		return evGovernanceResolvedAllow, true
	case "denied", "rejected", "expired", "failed", "cancelled":
		return evGovernanceResolvedDeny, true
	default:
		return "", false
	}
}
