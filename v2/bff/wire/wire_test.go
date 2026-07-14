package wire

import (
	"testing"

	cfg "github.com/devpablocristo/bff-v2/cmd/config"
)

func TestValidateAuthConfigRejectsUnsafeProductionDevMode(t *testing.T) {
	err := validateAuthConfig(cfg.Config{Environment: "prd", IdentityProvider: "dev", InternalAuthSecret: "secret"})
	if err == nil {
		t.Fatal("expected production dev authentication to be rejected")
	}
}

func TestValidateAuthConfigRequiresClerkIssuerAndInternalSecret(t *testing.T) {
	if err := validateAuthConfig(cfg.Config{Environment: "prd", IdentityProvider: "clerk", InternalAuthSecret: "secret"}); err == nil {
		t.Fatal("expected missing Clerk issuer to be rejected")
	}
	if err := validateAuthConfig(cfg.Config{Environment: "prd", IdentityProvider: "clerk", ClerkIssuerURL: "https://issuer.example"}); err == nil {
		t.Fatal("expected missing internal secret to be rejected")
	}
}
