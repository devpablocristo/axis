package memories

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeAndHashAreDeterministic(t *testing.T) {
	in, err := normalizeCreate(CreateInput{Title: "  Time zone ", Type: "PREFERENCE", Content: "  America/Argentina/Buenos_Aires  ", ActorID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Time zone" || in.Type != "preference" || in.Sensitivity != "normal" || in.Provenance != "human" {
		t.Fatalf("unexpected normalization: %+v", in)
	}
	if ContentHash(in.Content) != ContentHash(" America/Argentina/Buenos_Aires ") {
		t.Fatal("hash must ignore surrounding whitespace")
	}
}

func TestMemoryCursorRoundTrip(t *testing.T) {
	wantID := uuid.New()
	wantTime := time.Date(2026, 7, 13, 12, 34, 56, 789, time.UTC)
	encoded, err := encodeMemoryCursor(Memory{ID: wantID, UpdatedAt: wantTime})
	if err != nil {
		t.Fatalf("encodeMemoryCursor: %v", err)
	}
	gotTime, gotID, ok, err := decodeMemoryCursor(encoded)
	if err != nil {
		t.Fatalf("decodeMemoryCursor: %v", err)
	}
	if !ok || gotID != wantID || !gotTime.Equal(wantTime) {
		t.Fatalf("cursor mismatch: ok=%v id=%s time=%s", ok, gotID, gotTime)
	}
}

func TestMemoryCursorRejectsMalformedValue(t *testing.T) {
	if _, _, _, err := decodeMemoryCursor("not-a-cursor"); err == nil {
		t.Fatal("expected malformed cursor to fail")
	}
}

func TestNormalizeRejectsInvalidEnums(t *testing.T) {
	_, err := normalizeCreate(CreateInput{Title: "x", Type: "belief", Content: "y", ActorID: "actor"})
	if err == nil {
		t.Fatal("expected invalid type")
	}
}

func TestRedactSensitiveOutsideDetail(t *testing.T) {
	m := redact(Memory{ID: uuid.New(), Content: "secret", Sensitivity: "sensitive"}, false)
	if m.Content != "" || m.Preview != "[REDACTED]" {
		t.Fatalf("sensitive content leaked: %+v", m)
	}
	detail := redact(Memory{Content: "secret", Sensitivity: "sensitive"}, true)
	if detail.Content != "secret" {
		t.Fatal("authorized detail must preserve content")
	}
}

func TestContextHashIsDeterministicAndVersionBound(t *testing.T) {
	id := uuid.New()
	refs := []Reference{{ID: id, Title: "x", Type: "fact", Version: 1, Hash: "abc", Score: .5}}
	a, b := ContextHash(refs), ContextHash(refs)
	if a != b || a == "" {
		t.Fatal("context hash is not deterministic")
	}
	refs[0].Version++
	if ContextHash(refs) == a {
		t.Fatal("context hash must bind the version")
	}
}
