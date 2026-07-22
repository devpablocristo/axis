package virployees

import (
	"encoding/json"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

const (
	CapabilityClinicalRecordsSearch = "clinical.records.search"
	CapabilityClinicalTimelineBuild = "clinical.timeline.build"
)

// NormalizeAssistCapabilityKey applies only Axis' generic capability-key
// normalization. Consumer-specific aliases belong in the consumer adapter.
func NormalizeAssistCapabilityKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isClinicalAssistCapability(key string) bool {
	return key == CapabilityClinicalRecordsSearch || key == CapabilityClinicalTimelineBuild
}

func validateClinicalAssistInput(key string, raw json.RawMessage) error {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return domainerr.Validation("clinical capability input must be a JSON object")
	}
	allowed := map[string]struct{}{}
	switch key {
	case CapabilityClinicalRecordsSearch:
		for _, field := range []string{"query", "limit", "cursor"} {
			allowed[field] = struct{}{}
		}
		query, ok := value["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return domainerr.Validation("query is required for clinical.records.search")
		}
	case CapabilityClinicalTimelineBuild:
		for _, field := range []string{"date_from", "date_to", "order", "max_events", "focus"} {
			allowed[field] = struct{}{}
		}
	}
	for field := range value {
		if _, ok := allowed[field]; !ok {
			return domainerr.Validation("clinical capability input contains unsupported field " + field)
		}
	}
	return nil
}
