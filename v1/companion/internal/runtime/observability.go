package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ObservabilityRecorder interface {
	RecordObservabilityEvent(ctx context.Context, event ObservabilityEvent) error
}

type ObservabilityEvent struct {
	ID             uuid.UUID       `json:"id,omitempty"`
	OrgID          string          `json:"org_id"`
	ProductSurface string          `json:"product_surface"`
	RunID          *uuid.UUID      `json:"run_id,omitempty"`
	TaskID         *uuid.UUID      `json:"task_id,omitempty"`
	JobID          *uuid.UUID      `json:"job_id,omitempty"`
	AgentID        string          `json:"agent_id,omitempty"`
	CapabilityID   string          `json:"capability_id,omitempty"`
	EventType      string          `json:"event_type"`
	EventName      string          `json:"event_name"`
	Severity       string          `json:"severity"`
	TraceID        string          `json:"trace_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Redacted       bool            `json:"redacted"`
	OccurredAt     time.Time       `json:"occurred_at,omitempty"`
}

type RunReplay struct {
	Trace  StoredTrace          `json:"trace"`
	Events []ObservabilityEvent `json:"events"`
}

func newObservabilityEvent(trace RunTrace, in RunInput, eventType, eventName string, payload map[string]any) ObservabilityEvent {
	runID, _ := uuid.Parse(trace.RunID)
	var runPtr *uuid.UUID
	if runID != uuid.Nil {
		runPtr = &runID
	}
	raw, err := json.Marshal(redactValue(payload))
	if err != nil {
		raw = json.RawMessage(`{}`)
	}
	return ObservabilityEvent{
		OrgID:          strings.TrimSpace(in.OrgID),
		ProductSurface: strings.TrimSpace(firstNonEmpty(trace.ProductSurface, in.ProductSurface)),
		RunID:          runPtr,
		TaskID:         in.TaskID,
		AgentID:        trace.IdentityChain.AgentID,
		EventType:      eventType,
		EventName:      eventName,
		Severity:       "info",
		TraceID:        trace.RunID,
		Payload:        raw,
		Redacted:       true,
		OccurredAt:     time.Now().UTC(),
	}
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "***"
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactValue(item))
		}
		return out
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return "***"
		}
		return redactValue(decoded)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}
