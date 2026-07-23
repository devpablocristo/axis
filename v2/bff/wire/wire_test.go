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

func TestValidateAuthConfigRequiresExplicitDevelopmentLegacyKeys(t *testing.T) {
	base := cfg.Config{
		Environment:        "development",
		IdentityProvider:   "dev",
		InternalAuthSecret: "secret",
		ProductAPIKeys:     "legacy=product|virployee|actor|surface",
	}
	if err := validateAuthConfig(base); err == nil {
		t.Fatal("expected legacy keys without the explicit opt-in to be rejected")
	}
	base.AllowLegacyProductAPIKeys = true
	if err := validateAuthConfig(base); err != nil {
		t.Fatalf("expected explicit development legacy keys to be accepted: %v", err)
	}
	base.Environment = "prd"
	base.IdentityProvider = "clerk"
	base.ClerkIssuerURL = "https://issuer.example"
	if err := validateAuthConfig(base); err == nil {
		t.Fatal("expected legacy product keys to be rejected outside development/test")
	}
}

func TestValidateAuthConfigAllowsUnavailableOptionalParticipants(t *testing.T) {
	config := cfg.Config{
		Environment:        "development",
		IdentityProvider:   "dev",
		InternalAuthSecret: "secret",
		CompanionBaseURL:   "",
		NexusBaseURL:       "",
	}
	if err := validateAuthConfig(config); err != nil {
		t.Fatalf("BFF must start in a degraded state without optional participants: %v", err)
	}
}
