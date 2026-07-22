package wire

import (
	"context"
	"testing"

	cfg "github.com/devpablocristo/nexus-v2/cmd/config"
)

func TestProductionRequiresExecutorAttestationSecretRef(t *testing.T) {
	_, err := resolveAttestationKey(context.Background(), cfg.Config{Environment: "production", InternalAuthSecret: "not-used"})
	if err == nil {
		t.Fatal("production must reject an inline or derived attestation key")
	}
}

func TestDevelopmentAttestationKeyIsDerived(t *testing.T) {
	key, err := resolveAttestationKey(context.Background(), cfg.Config{Environment: "development", InternalAuthSecret: "local-token"})
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("derived key bytes = %d", len(key))
	}
}
