package agents

import "testing"

func TestResolveUsesGenericProductProfile(t *testing.T) {
	t.Parallel()

	profile := DefaultRegistry().Resolve(
		"demo",
		"general.assist",
		"A2",
		[]string{"companion:capabilities:read"},
		[]string{"remember", "demo_orders_search", "pymes_customers_search"},
	)

	if profile.ID != "product.demo.generic" {
		t.Fatalf("expected generic product profile, got %q", profile.ID)
	}
	if profile.ProductSurface != "demo" {
		t.Fatalf("expected demo surface, got %q", profile.ProductSurface)
	}
	if !contains(profile.AllowedTools, "demo_orders_search") {
		t.Fatalf("expected demo capability to be allowed: %+v", profile.AllowedTools)
	}
	if contains(profile.AllowedTools, "pymes_customers_search") {
		t.Fatalf("generic demo profile leaked pymes tool: %+v", profile.AllowedTools)
	}
}

func TestResolveKeepsPymesCompatibilityThroughGenericProfile(t *testing.T) {
	t.Parallel()

	profile := DefaultRegistry().Resolve(
		"pymes",
		"general.assist",
		"A2",
		[]string{"companion:capabilities:read"},
		[]string{"remember", "recall", "pymes_customers_search"},
	)

	if profile.ID == "pymes.default" {
		t.Fatal("pymes should use the generic product profile, not a hardcoded default")
	}
	if !contains(profile.AllowedTools, "pymes_customers_search") {
		t.Fatalf("expected pymes compatibility tool to be allowed: %+v", profile.AllowedTools)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
