package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CostEvent struct {
	ID                 uuid.UUID       `json:"id,omitempty"`
	OrgID              string          `json:"org_id"`
	RunID              *uuid.UUID      `json:"run_id,omitempty"`
	TaskID             *uuid.UUID      `json:"task_id,omitempty"`
	JobID              *uuid.UUID      `json:"job_id,omitempty"`
	AgentID            string          `json:"agent_id,omitempty"`
	CapabilityID       string          `json:"capability_id,omitempty"`
	Model              string          `json:"model,omitempty"`
	CostClass          string          `json:"cost_class,omitempty"`
	EventType          string          `json:"event_type"`
	EstimatedTokens    int64           `json:"estimated_tokens"`
	EstimatedCostCents int64           `json:"estimated_cost_cents"`
	Quantity           int64           `json:"quantity"`
	Payload            json.RawMessage `json:"payload_json"`
	OccurredAt         time.Time       `json:"occurred_at,omitempty"`
}

type CostSummary struct {
	OrgID              string      `json:"org_id"`
	Period             string      `json:"period"`
	EstimatedTokens    int64       `json:"estimated_tokens"`
	EstimatedCostCents int64       `json:"estimated_cost_cents"`
	LLMCalls           int64       `json:"llm_calls"`
	ToolCalls          int64       `json:"tool_calls"`
	JobEvents          int64       `json:"job_events"`
	EmbeddingEvents    int64       `json:"embedding_events"`
	Events             []CostEvent `json:"events,omitempty"`
}

type CostLedger interface {
	RecordCostEvent(ctx context.Context, event CostEvent) error
	GetCostSummary(ctx context.Context, orgID, period string, limit int) (CostSummary, error)
}

func costEventForRun(trace RunTrace, in RunInput) CostEvent {
	runID, _ := uuid.Parse(trace.RunID)
	var runPtr *uuid.UUID
	if runID != uuid.Nil {
		runPtr = &runID
	}
	payload, _ := json.Marshal(redactValue(map[string]any{
		"usage":            trace.Usage,
		"tool_calls":       len(trace.ToolCalls),
		"guardrail_events": len(trace.GuardrailEvents),
	}))
	return CostEvent{
		OrgID:              strings.TrimSpace(in.OrgID),
		RunID:              runPtr,
		TaskID:             in.TaskID,
		AgentID:            trace.IdentityChain.AgentID,
		Model:              trace.Model,
		CostClass:          "llm",
		EventType:          "run",
		EstimatedTokens:    int64(trace.Usage.EstimatedTotalTokens),
		EstimatedCostCents: estimateRunCostCents(trace.Usage),
		Quantity:           maxInt64(1, int64(trace.Usage.LLMCalls)),
		Payload:            payload,
		OccurredAt:         time.Now().UTC(),
	}
}

func costEventForTools(trace RunTrace, in RunInput) CostEvent {
	runID, _ := uuid.Parse(trace.RunID)
	var runPtr *uuid.UUID
	if runID != uuid.Nil {
		runPtr = &runID
	}
	payload, _ := json.Marshal(redactValue(map[string]any{"tool_calls": trace.ToolCalls}))
	return CostEvent{
		OrgID:      strings.TrimSpace(in.OrgID),
		RunID:      runPtr,
		TaskID:     in.TaskID,
		AgentID:    trace.IdentityChain.AgentID,
		Model:      trace.Model,
		CostClass:  "tool",
		EventType:  "tool",
		Quantity:   int64(len(trace.ToolCalls)),
		Payload:    payload,
		OccurredAt: time.Now().UTC(),
	}
}

func estimateRunCostCents(usage RunUsage) int64 {
	tokens := int64(usage.EstimatedTotalTokens)
	if tokens <= 0 {
		return 0
	}
	// Conservative internal estimate: one cent per 1k tokens, rounded up.
	return (tokens + 999) / 1000
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
