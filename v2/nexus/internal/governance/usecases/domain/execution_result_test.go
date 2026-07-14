package domain

import "testing"

func TestNormalizeExecutionResultInput(t *testing.T) {
	input, err := NormalizeExecutionResultInput(ExecutionResultInput{
		IdempotencyKey: " result-1 ", BindingHash: " binding ", Status: "succeeded", DurationMS: 12,
		Result: map[string]any{"resource_id": "event-1", "access_token": "secret"},
	})
	if err != nil {
		t.Fatalf("NormalizeExecutionResultInput: %v", err)
	}
	if input.IdempotencyKey != "result-1" || input.Result["access_token"] != "[REDACTED]" {
		t.Fatalf("unexpected normalized result: %+v", input)
	}
}

func TestNormalizeExecutionResultInputRejectsInvalidStatus(t *testing.T) {
	_, err := NormalizeExecutionResultInput(ExecutionResultInput{IdempotencyKey: "result-1", BindingHash: "binding", Status: "running"})
	if err == nil {
		t.Fatal("expected invalid status to fail")
	}
}

func TestNormalizeExecutionResultInputRedactsNestedSecrets(t *testing.T) {
	input, err := NormalizeExecutionResultInput(ExecutionResultInput{
		IdempotencyKey: "result-1",
		BindingHash:    "binding",
		Status:         "succeeded",
		Result: map[string]any{
			"nested": map[string]any{"api_key": "secret", "safe": "value"},
			"items":  []any{map[string]any{"authorization": "Bearer secret"}},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeExecutionResultInput: %v", err)
	}
	nested := input.Result["nested"].(map[string]any)
	items := input.Result["items"].([]any)
	if nested["api_key"] != "[REDACTED]" || nested["safe"] != "value" || items[0].(map[string]any)["authorization"] != "[REDACTED]" {
		t.Fatalf("nested secret leaked: %+v", input.Result)
	}
}
