package attestation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestVerifierRejectsTamperedResult(t *testing.T) {
	key := DeriveDevelopmentKey("internal-token")
	verifier, err := NewVerifier(key)
	if err != nil {
		t.Fatal(err)
	}
	payload := Payload{Version: Version, ExecutorVersion: "executor-v1", OrgID: "org", GovernanceCheckID: "check", BindingHash: "binding", IdempotencyKey: "idem", Status: "succeeded", DurationMS: 1, Result: map[string]any{"b": 2, "a": 1}}
	raw, _ := canonicalBytes(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if signature != "jQxdBPJV0YMOLOyIZIuYJMGRWtWnWaRb2k2OuWXXivk" {
		t.Fatalf("signature drifted from shared golden vector: %s", signature)
	}
	if err := verifier.Verify(payload, signature); err != nil {
		t.Fatal(err)
	}
	payload.Result["a"] = 2
	if err := verifier.Verify(payload, signature); err == nil {
		t.Fatal("tampered result must fail")
	}
}
