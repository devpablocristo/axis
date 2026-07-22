package domain

import (
	"encoding/json"
	"testing"
)

func TestNormalizeManifestProducesStableHash(t *testing.T) {
	first := ManifestInput{
		Version: "1.2.3", ProductSurface: " ProductA ",
		InputSchema:    json.RawMessage(`{"properties":{"x":{"type":"string"}},"type":"object"}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		RequiredScopes: []string{"documents:read", "assist:run", "documents:read"},
		Idempotency:    IdempotencyContract{Mode: "required", KeyFields: []string{"subject_id", "generation"}},
		QuotaAreas:     []string{"executors", "inbound", "INBOUND"},
	}
	second := first
	second.RequiredScopes = []string{"assist:run", "documents:read"}
	second.Idempotency.KeyFields = []string{"generation", "subject_id"}
	second.QuotaAreas = []string{"inbound", "executors"}

	one, oneHash, err := NormalizeManifest(first)
	if err != nil {
		t.Fatal(err)
	}
	two, twoHash, err := NormalizeManifest(second)
	if err != nil {
		t.Fatal(err)
	}
	if oneHash != twoHash || one.ProductSurface != "producta" || len(one.RequiredScopes) != 2 || len(two.QuotaAreas) != 2 {
		t.Fatalf("manifest must normalize deterministically: one=%+v two=%+v hashes=%s/%s", one, two, oneHash, twoHash)
	}
}

func TestNormalizeManifestRejectsNonObjectSchema(t *testing.T) {
	_, _, err := NormalizeManifest(ManifestInput{InputSchema: json.RawMessage(`[]`)})
	if err == nil {
		t.Fatal("array schema payload must be rejected")
	}
}
