package virployees

import "testing"

func TestNormalizeAssistCapabilityKeyCanonicalizesLegacyAliases(t *testing.T) {
	tests := map[string]string{
		"medmory.search.query":   CapabilityClinicalRecordsSearch,
		"medmory.timeline.read":  CapabilityClinicalTimelineBuild,
		"medmory.timeline.build": CapabilityClinicalTimelineBuild,
	}
	for alias, expected := range tests {
		got, deprecated := NormalizeAssistCapabilityKey(alias)
		if !deprecated || got != expected {
			t.Fatalf("NormalizeAssistCapabilityKey(%q) = %q,%v", alias, got, deprecated)
		}
	}
	canonical, deprecated := NormalizeAssistCapabilityKey(CapabilityClinicalTimelineBuild)
	if deprecated || canonical != CapabilityClinicalTimelineBuild {
		t.Fatalf("canonical key changed: %q,%v", canonical, deprecated)
	}
}
