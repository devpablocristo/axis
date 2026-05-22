package requests

import (
	"context"
	"testing"

	"github.com/google/uuid"

	requestdomain "github.com/devpablocristo/nexus/governance/internal/requests/usecases/domain"
)

func TestHMACAttestationVerifier(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	att := requestdomain.Attestation{
		RequestID:    uuid.New(),
		Status:       "success",
		ProviderRefs: map[string]any{"tx_id": "tx-1"},
		Attester:     "pymes",
		Metadata:     map[string]any{"connector_execution_id": "exec-1"},
	}
	signature, err := SignAttestationForTest(secret, att)
	if err != nil {
		t.Fatal(err)
	}
	att.Signature = signature

	verifier, err := NewHMACAttestationVerifier(string(secret))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(context.Background(), att); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}

	att.ProviderRefs["tx_id"] = "tx-2"
	if err := verifier.Verify(context.Background(), att); err == nil {
		t.Fatal("expected tampered attestation to fail verification")
	}
}
