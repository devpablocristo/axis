package attestation

import "testing"

func TestSignerIsCanonicalAndBindsEveryExecutionField(t *testing.T) {
	key := DeriveDevelopmentKey("internal-token")
	signer, err := NewSigner(key, "executor-v1")
	if err != nil {
		t.Fatal(err)
	}
	base := Payload{OrgID: "org", GovernanceCheckID: "check", BindingHash: "binding", IdempotencyKey: "idem", Status: "succeeded", DurationMS: 1, Result: map[string]any{"b": 2, "a": 1}}
	first, err := signer.Sign(base)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := signer.Sign(Payload{OrgID: "org", GovernanceCheckID: "check", BindingHash: "binding", IdempotencyKey: "idem", Status: "succeeded", DurationMS: 1, Result: map[string]any{"a": 1, "b": 2}})
	if first.Signature != second.Signature {
		t.Fatal("map insertion order must not change canonical signature")
	}
	if first.Signature != "jQxdBPJV0YMOLOyIZIuYJMGRWtWnWaRb2k2OuWXXivk" {
		t.Fatalf("signature drifted from shared golden vector: %s", first.Signature)
	}
	base.BindingHash = "tampered"
	tampered, _ := signer.Sign(base)
	if first.Signature == tampered.Signature || first.Version != Version || first.ExecutorVersion != "executor-v1" {
		t.Fatal("signature must bind the execution envelope")
	}
}
