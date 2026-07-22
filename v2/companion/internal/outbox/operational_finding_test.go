package outbox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestOperationalFindingRejectsSensitiveMetadata(t *testing.T) {
	raw, _ := json.Marshal(OperationalFinding{RunID: uuid.NewString(), FindingType: "job.dead_letter", Severity: "high", ResourceType: "job", ResourceID: uuid.NewString(), Fingerprint: strings.Repeat("a", 64), StateBased: true, Metadata: json.RawMessage(`{"patient_name":"secret"}`)})
	if _, err := ParseOperationalFinding(raw); err == nil {
		t.Fatal("finding metadata must reject fields outside the operational allowlist")
	}
}
func TestOperationalFindingAcceptsBoundedOperationalMetadata(t *testing.T) {
	raw, _ := json.Marshal(OperationalFinding{RunID: uuid.NewString(), FindingType: "job.dead_letter", Severity: "high", ResourceType: "job", ResourceID: uuid.NewString(), Fingerprint: strings.Repeat("a", 64), StateBased: true, Metadata: json.RawMessage(`{"job_kind":"assist.process","count":2}`)})
	if _, err := ParseOperationalFinding(raw); err != nil {
		t.Fatalf("expected safe metadata: %v", err)
	}
}
