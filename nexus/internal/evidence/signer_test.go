package evidence

import (
	"testing"

	evidencedomain "github.com/devpablocristo/nexus/internal/evidence/usecases/domain"
)

func TestNewSigner_EmptyKey(t *testing.T) {
	t.Parallel()
	_, err := NewSigner("", "")
	if err == nil {
		t.Fatal("expected error for empty signing key")
	}
}

func TestNewSigner_DefaultKeyID(t *testing.T) {
	t.Parallel()
	s, err := NewSigner("my-secret", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.keyID != "default" {
		t.Errorf("keyID = %q, want %q", s.keyID, "default")
	}
}

func TestSign_ProducesValidSignature(t *testing.T) {
	t.Parallel()
	s, err := NewSigner("test-key", "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sig := s.Sign([]byte(`{"test":"data"}`))
	if sig.Algorithm != "hmac-sha256" {
		t.Errorf("algorithm = %q, want %q", sig.Algorithm, "hmac-sha256")
	}
	if sig.KeyID != "k1" {
		t.Errorf("key_id = %q, want %q", sig.KeyID, "k1")
	}
	if sig.Value == "" {
		t.Error("signature value should not be empty")
	}
	if sig.SignedAt == "" {
		t.Error("signed_at should not be empty")
	}
}

func TestSign_DeterministicForSameInput(t *testing.T) {
	t.Parallel()
	s, err := NewSigner("test-key", "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := []byte(`{"action":"transfer","amount":1000}`)
	sig1 := s.Sign(payload)
	sig2 := s.Sign(payload)
	if sig1.Value != sig2.Value {
		t.Error("same key + same payload should produce same HMAC value")
	}
}

func TestSign_DifferentKeyProducesDifferentSignature(t *testing.T) {
	t.Parallel()
	s1, _ := NewSigner("key-a", "k1")
	s2, _ := NewSigner("key-b", "k1")
	payload := []byte(`{"test":"data"}`)
	sig1 := s1.Sign(payload)
	sig2 := s2.Sign(payload)
	if sig1.Value == sig2.Value {
		t.Error("different keys should produce different signatures")
	}
}

func TestSignPack_SetsSignatureOnPack(t *testing.T) {
	t.Parallel()
	s, err := NewSigner("test-key", "k1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pack := &evidencedomain.EvidencePack{
		Version: "1.0",
		Request: evidencedomain.RequestEvidence{
			ID:        "abc-123",
			Requester: evidencedomain.Requester{Type: "agent", ID: "bot"},
		},
	}
	if err := s.SignPack(pack); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pack.Signature.Algorithm != "hmac-sha256" {
		t.Errorf("algorithm = %q, want %q", pack.Signature.Algorithm, "hmac-sha256")
	}
	if pack.Signature.Value == "" {
		t.Error("signature value should not be empty after signing")
	}
}
