package contracts

import "testing"

func TestValidateSchemaRequiredConstAndTypes(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"schema_version", "org_id", "success"},
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "const": "tool_intent.v1"},
			"org_id":         map[string]any{"type": "string", "minLength": float64(1)},
			"success":        map[string]any{"type": "boolean"},
		},
	}
	errs := ValidateSchema(map[string]any{
		"schema_version": "wrong",
		"org_id":         " ",
		"success":        "yes",
		"extra":          true,
	}, schema)
	if len(errs) != 5 {
		t.Fatalf("expected 5 validation errors, got %d: %#v", len(errs), errs)
	}
}

func TestValidateSchemaAcceptsValidPayload(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"schema_version", "org_id"},
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "const": "tool_intent.v1"},
			"org_id":         map[string]any{"type": "string", "minLength": float64(1)},
		},
	}
	errs := ValidateSchema(map[string]any{"schema_version": "tool_intent.v1", "org_id": "org-1"}, schema)
	if len(errs) != 0 {
		t.Fatalf("expected valid payload, got %#v", errs)
	}
}
