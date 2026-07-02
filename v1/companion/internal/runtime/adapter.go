package runtime

import (
	"context"
	"log/slog"

	"github.com/devpablocristo/companion/internal/tasks"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// OrchestratorAdapter adapta el runtime.Orchestrator a la interfaz tasks.ChatOrchestrator.
type OrchestratorAdapter struct {
	orch *Orchestrator
}

// NewOrchestratorAdapter crea el adapter.
func NewOrchestratorAdapter(orch *Orchestrator) *OrchestratorAdapter {
	return &OrchestratorAdapter{orch: orch}
}

// Run implementa tasks.ChatOrchestrator.
func (a *OrchestratorAdapter) Run(ctx context.Context, in tasks.OrchestratorInput) (tasks.OrchestratorResult, error) {
	result, err := a.orch.Run(ctx, RunInput{
		UserID:         in.UserID,
		OrgID:          in.OrgID,
		AuthScopes:     in.AuthScopes,
		Identity:       in.Identity,
		Message:        in.Message,
		RouteHint:      in.RouteHint,
		Messages:       convertMessages(in.Messages),
		TaskID:         in.TaskID,
		ProductSurface: in.ProductSurface,
		TenantID:       in.TenantID,
		VirployeeID:    in.VirployeeID,
		AgentID:        in.AgentID,
		Handoff:        in.Handoff,
		Workspace:      in.Workspace,
	})
	if err != nil {
		return tasks.OrchestratorResult{}, err
	}
	slog.Info("companion_runtime_run_completed",
		"run_id", result.Trace.RunID,
		"intent", result.Trace.Intent,
		"customer_org_id", result.Trace.IdentityChain.CustomerOrgID,
		"product_surface", result.Trace.ProductSurface,
		"autonomy", result.Trace.AutonomyLevel,
		"tool_calls", len(result.Trace.ToolCalls),
		"guardrail_events", len(result.Trace.GuardrailEvents),
	)
	return tasks.OrchestratorResult{
		Reply:       result.Reply,
		RunID:       result.Trace.RunID,
		VirployeeID: result.Trace.IdentityChain.VirployeeID,
		AgentID:     result.Trace.IdentityChain.AgentID,
		ToolCalls:   convertToolCalls(result.Trace.ToolCalls),
	}, nil
}

func convertMessages(msgs []taskdomain.TaskMessage) []taskdomain.TaskMessage {
	// Mismo tipo, solo pasa directo — el adapter existe para desacoplar packages
	return msgs
}

func convertToolCalls(calls []ToolTrace) []tasks.OrchestratorToolCall {
	out := make([]tasks.OrchestratorToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, tasks.OrchestratorToolCall{
			Name:           call.Name,
			ToolCallID:     call.ToolCallID,
			Allowed:        call.Allowed,
			DecisionReason: call.DecisionReason,
			DurationMS:     call.DurationMS,
			Error:          call.Error,
			Result:         call.Result,
		})
	}
	return out
}
