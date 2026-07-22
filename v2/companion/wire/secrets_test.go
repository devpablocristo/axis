package wire

import (
	"context"
	"testing"

	cfg "github.com/devpablocristo/companion-v2/cmd/config"
)

func TestProductionRequiresSecretRefs(t *testing.T) {
	config := cfg.Config{Environment: "production", InternalAuthSecret: "not-used"}
	if _, err := resolveAttestationKey(context.Background(), config); err == nil {
		t.Fatal("production must reject an inline or derived attestation key")
	}
	if _, err := resolveGoogleCalendarAPI(context.Background(), config); err == nil {
		t.Fatal("production must reject Google Calendar without secret_ref")
	}
}

func TestDevelopmentAttestationKeyIsDerivedWithoutPersistingCredentials(t *testing.T) {
	key, err := resolveAttestationKey(context.Background(), cfg.Config{Environment: "development", InternalAuthSecret: "local-token"})
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("derived key bytes = %d", len(key))
	}
}
