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
