// Package modelkernel adapts the shared AI kernel to Runtime's application
// owned ModelPort. Provider-specific response shapes stop at this boundary.
package modelkernel

import (
	"context"

	ai "github.com/devpablocristo/platform/kernels/ai/go"
	"github.com/devpablocristo/runtime-v2/internal/planner"
)

type Adapter struct {
	provider ai.Provider
}

func New(provider ai.Provider) *Adapter {
	return &Adapter{provider: provider}
}

func (a *Adapter) Complete(ctx context.Context, request planner.ModelRequest) (planner.ModelResponse, error) {
	messages := make([]ai.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		messages = append(messages, ai.Message{Role: message.Role, Content: message.Content})
	}
	tools := make([]ai.Tool, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, ai.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	response, err := a.provider.Chat(ctx, ai.ChatRequest{
		SystemPrompt:   request.SystemPrompt,
		Messages:       messages,
		Tools:          tools,
		ResponseSchema: request.ResponseSchema,
		MaxTokens:      request.MaxTokens,
	})
	if err != nil {
		return planner.ModelResponse{}, err
	}
	calls := make([]planner.ModelToolCall, 0, len(response.ToolCalls))
	for _, call := range response.ToolCalls {
		calls = append(calls, planner.ModelToolCall{Name: call.Name, Args: call.Args})
	}
	return planner.ModelResponse{Text: response.Text, ToolCalls: calls}, nil
}
