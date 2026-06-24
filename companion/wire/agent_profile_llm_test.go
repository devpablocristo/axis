package wire

import (
	"encoding/json"
	"testing"
)

func TestRuntimeLLMConfigFromMap(t *testing.T) {
	t.Parallel()

	// JSON-decoded maps deliver numbers as float64, matching how llm_config_json
	// is unmarshalled from postgres.
	var cfg map[string]any
	if err := json.Unmarshal([]byte(`{"model":"gemini-2.5-pro","max_tokens":4096,"temperature":0.3}`), &cfg); err != nil {
		t.Fatal(err)
	}

	got := runtimeLLMConfigFromMap(cfg)
	if got.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want gemini-2.5-pro", got.Model)
	}
	if got.MaxTokens != 4096 {
		t.Fatalf("max_tokens = %d, want 4096", got.MaxTokens)
	}
	if got.Temperature != 0.3 {
		t.Fatalf("temperature = %v, want 0.3", got.Temperature)
	}
}

func TestRuntimeLLMConfigFromMapEmptyAndInvalid(t *testing.T) {
	t.Parallel()

	if got := runtimeLLMConfigFromMap(nil); got.Model != "" || got.MaxTokens != 0 || got.Temperature != 0 {
		t.Fatalf("nil config should yield zero value, got %+v", got)
	}

	// Zero/negative max_tokens must be ignored so the runtime applies its default.
	cfg := map[string]any{"max_tokens": float64(0), "model": "  ", "temperature": "nan"}
	got := runtimeLLMConfigFromMap(cfg)
	if got.MaxTokens != 0 {
		t.Fatalf("max_tokens = %d, want 0 (ignored)", got.MaxTokens)
	}
	if got.Model != "" {
		t.Fatalf("blank model should trim to empty, got %q", got.Model)
	}
	if got.Temperature != 0 {
		t.Fatalf("invalid temperature should be ignored, got %v", got.Temperature)
	}
}
