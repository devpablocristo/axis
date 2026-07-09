package runtraces

import (
	"strings"
	"testing"
)

func TestInputPreviewRedactsAndTruncates(t *testing.T) {
	input := "token=super-secret " + strings.Repeat("x", InputPreviewLimit+20)

	preview := InputPreview(input)

	if strings.Contains(preview, "super-secret") {
		t.Fatalf("preview leaked secret: %q", preview)
	}
	if !strings.Contains(preview, "token=[REDACTED]") {
		t.Fatalf("preview did not redact token: %q", preview)
	}
	if len([]rune(preview)) > InputPreviewLimit {
		t.Fatalf("preview length = %d, want <= %d", len([]rune(preview)), InputPreviewLimit)
	}
}

func TestRedactValueRedactsNestedSensitiveKeys(t *testing.T) {
	out := RedactValue(map[string]any{
		"authorization": "Bearer abc",
		"nested": map[string]any{
			"api_key": "raw-key",
			"name":    "Sofia",
		},
	}).(map[string]any)

	if out["authorization"] != "[REDACTED]" {
		t.Fatalf("authorization was not redacted: %+v", out)
	}
	nested := out["nested"].(map[string]any)
	if nested["api_key"] != "[REDACTED]" || nested["name"] != "Sofia" {
		t.Fatalf("unexpected nested redaction: %+v", nested)
	}
}

func TestBindingHashIsDeterministic(t *testing.T) {
	first, err := BindingHash(map[string]any{
		"tenant_id":      "tenant-1",
		"capability_key": "calendar.events.create",
		"input_hash":     "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BindingHash(map[string]any{
		"input_hash":     "abc",
		"capability_key": "calendar.events.create",
		"tenant_id":      "tenant-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("hashes should be deterministic, got %q and %q", first, second)
	}
}
