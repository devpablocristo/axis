package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEnvResolverResolvesAndRedactsSecretValue(t *testing.T) {
	t.Parallel()

	resolver := NewEnvResolverWithLookup(func(name string) (string, bool) {
		if name == "AXIS_TEST_SECRET" {
			return "plain-secret", true
		}
		return "", false
	})
	secret, err := resolver.Resolve(context.Background(), "env:AXIS_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value() != "plain-secret" || secret.Ref() != "env:AXIS_TEST_SECRET" {
		t.Fatalf("unexpected secret: ref=%s value=%s", secret.Ref(), secret.Value())
	}
	formatted := fmt.Sprintf("%v %#v", secret, secret)
	if strings.Contains(formatted, "plain-secret") {
		t.Fatalf("secret string leaked value: %s", formatted)
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "plain-secret") || !strings.Contains(string(raw), `"redacted":true`) {
		t.Fatalf("secret json leaked value: %s", string(raw))
	}
}

func TestEnvResolverRejectsUnsupportedAndMissingSecrets(t *testing.T) {
	t.Parallel()

	resolver := NewEnvResolverWithLookup(func(string) (string, bool) { return "", false })
	if _, err := resolver.Resolve(context.Background(), "vault:axis/products/demo"); !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("expected unsupported scheme, got %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), "env:AXIS_MISSING_SECRET"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected missing secret, got %v", err)
	}
}

func TestValidateRefRejectsInlineOrMalformedRefs(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"plain-secret", "env:", "env:BAD NAME", "https://secret.example/key"} {
		if err := ValidateRef(ref); err == nil {
			t.Fatalf("expected invalid ref %q to fail", ref)
		}
	}
	for _, ref := range []string{"env:AXIS_PRODUCT_API_KEY", "vault:axis/products/demo", "secretmanager:projects/demo/secrets/key"} {
		if err := ValidateRef(ref); err != nil {
			t.Fatalf("expected valid ref %q, got %v", ref, err)
		}
	}
}
