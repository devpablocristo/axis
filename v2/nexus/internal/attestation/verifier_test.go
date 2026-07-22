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
	payload := Payload{Version: Version, ExecutorVersion: "executor-v1", TenantID: "tenant", GovernanceCheckID: "check", BindingHash: "binding", IdempotencyKey: "idem", Status: "succeeded", DurationMS: 1, Result: map[string]any{"b": 2, "a": 1}}
	raw, _ := canonicalBytes(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if signature != "d0wA1JzU41N4Ab3ldy7Dk_WAqJGBd4sJtH7p3Wp2Q_U" {
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
