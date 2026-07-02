package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeObserver struct {
	events []ObservabilityEvent
}

func (f *fakeObserver) RecordObservabilityEvent(_ context.Context, event ObservabilityEvent) error {
	f.events = append(f.events, event)
	return nil
}

func TestObservabilityRedactsSecrets(t *testing.T) {
	t.Parallel()

	event := newObservabilityEvent(RunTrace{RunID: "00000000-0000-0000-0000-000000000001"}, RunInput{OrgID: "org-1"}, "tool", "executed", map[string]any{
		"api_key": "secret",
		"nested":  map[string]any{"client_secret": "hidden", "safe": "ok"},
	})
	raw := string(event.Payload)
	if strings.Contains(raw, `"secret"`) || strings.Contains(raw, "hidden") {
		t.Fatalf("expected secrets redacted, got %s", raw)
	}
	if !strings.Contains(raw, `"api_key":"***"`) || !strings.Contains(raw, `"safe":"ok"`) {
		t.Fatalf("expected redacted payload with safe fields, got %s", raw)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestOrchestratorRecordsObservabilityEvents(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}
	provider := &fakeLLMProvider{responses: []ChatResponse{{Text: "ok"}}}
	orch := NewOrchestrator(provider, &ToolKit{Handlers: map[string]ToolHandler{}}, ContextPorts{})
	orch.SetObservabilityRecorder(observer)
	result, err := orch.Run(context.Background(), RunInput{
		UserID: "user-1", OrgID: "org-1", Message: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "ok" {
		t.Fatalf("unexpected reply %q", result.Reply)
	}
	var names []string
	for _, event := range observer.events {
		names = append(names, event.EventName)
		if !event.Redacted {
			t.Fatalf("expected observability event to be redacted: %+v", event)
		}
		if !json.Valid(event.Payload) {
			t.Fatalf("expected valid event payload: %s", string(event.Payload))
		}
	}
	for _, want := range []string{"started", "request", "completed"} {
		if !containsString(names, want) {
			t.Fatalf("expected observability event %q in %v", want, names)
		}
	}
}
