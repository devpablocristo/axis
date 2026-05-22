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
		Message:        in.Message,
		Messages:       convertMessages(in.Messages),
		TaskID:         in.TaskID,
		ProductSurface: in.ProductSurface,
	})
	if err != nil {
		return tasks.OrchestratorResult{}, err
	}
	slog.Info("companion_runtime_run_completed",
		"run_id", result.Trace.RunID,
		"intent", result.Trace.Intent,
		"tenant", result.Trace.IdentityChain.Tenant,
		"product_surface", result.Trace.ProductSurface,
		"autonomy", result.Trace.AutonomyLevel,
		"tool_calls", len(result.Trace.ToolCalls),
		"guardrail_events", len(result.Trace.GuardrailEvents),
	)
	return tasks.OrchestratorResult{Reply: result.Reply}, nil
}

func convertMessages(msgs []taskdomain.TaskMessage) []taskdomain.TaskMessage {
	// Mismo tipo, solo pasa directo — el adapter existe para desacoplar packages
	return msgs
}
