package planner

import (
	"context"
	"encoding/json"
)

// ModelPort is the application-owned boundary to a structured language model.
// Provider SDK types must be translated by an outbound adapter before entering
// the planner.
type ModelPort interface {
	Complete(context.Context, ModelRequest) (ModelResponse, error)
}

type ModelRequest struct {
	SystemPrompt   string
	Messages       []ModelMessage
	Tools          []ModelTool
	ResponseSchema map[string]any
	MaxTokens      int
}

type ModelMessage struct {
	Role    string
	Content string
}

type ModelTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ModelResponse struct {
	Text      string
	ToolCalls []ModelToolCall
}

type ModelToolCall struct {
	Name string
	Args json.RawMessage
}
