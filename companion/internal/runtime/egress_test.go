package runtime

import (
	"encoding/json"
	"testing"
)

func TestValidateEgressPayloadRejectsPrivateTargets(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"webhook_url": "http://169.254.169.254/latest/meta-data"})
	event := ValidateEgressPayload(payload)
	if event == nil || event.Type != "ssrf" {
		t.Fatalf("expected ssrf guardrail, got %+v", event)
	}
}

func TestValidateEgressPayloadAllowsPublicHTTPS(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"webhook_url": "https://api.example.com/callback"})
	if event := ValidateEgressPayload(payload); event != nil {
		t.Fatalf("expected public egress payload to pass, got %+v", event)
	}
}
