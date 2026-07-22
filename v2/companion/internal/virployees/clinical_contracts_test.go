package virployees

import "testing"

func TestNormalizeAssistCapabilityKeyDoesNotTranslateConsumerAliases(t *testing.T) {
	if got := NormalizeAssistCapabilityKey(" Product.Search.Query "); got != "product.search.query" {
		t.Fatalf("consumer capability key changed beyond generic normalization: %q", got)
	}
	if got := NormalizeAssistCapabilityKey(CapabilityClinicalTimelineBuild); got != CapabilityClinicalTimelineBuild {
		t.Fatalf("canonical key changed: %q", got)
	}
}
