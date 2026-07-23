package inbound

import (
	"encoding/json"
	"testing"
)

func TestMatchesEventSchemaRejectsMissingRequiredAndExtraFields(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"required":["state"],
		"additionalProperties":false,
		"properties":{"state":{"type":"string","enum":["ready","failed"]}}
	}`)
	for _, test := range []struct {
		name  string
		value string
		ok    bool
	}{
		{name: "valid", value: `{"state":"ready"}`, ok: true},
		{name: "missing", value: `{}`, ok: false},
		{name: "enum", value: `{"state":"unknown"}`, ok: false},
		{name: "extra", value: `{"state":"ready","secret":"x"}`, ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesEventSchema(schema, json.RawMessage(test.value)); got != test.ok {
				t.Fatalf("matchesEventSchema()=%v, want %v", got, test.ok)
			}
		})
	}
}
