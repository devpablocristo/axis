package enterpriseops

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func validFinding() FindingInput {
	return FindingInput{RunID: uuid.NewString(), FindingType: "job.dead_letter", Severity: "high", ResourceType: "job", ResourceID: uuid.NewString(), Fingerprint: strings.Repeat("b", 64), StateBased: true, Metadata: json.RawMessage(`{}`)}
}
func TestNormalizeFindingRejectsSensitiveMetadata(t *testing.T) {
	in := validFinding()
	in.Metadata = json.RawMessage(`{"clinical_note":"secret"}`)
	if _, err := normalizeFinding(in); err == nil {
		t.Fatal("Nexus must reject sensitive or arbitrary finding metadata")
	}
}
func TestNormalizeFindingAcceptsSafeMetadata(t *testing.T) {
	in := validFinding()
	in.Metadata = json.RawMessage(`{"service":"companion","age_seconds":90}`)
	if _, err := normalizeFinding(in); err != nil {
		t.Fatal(err)
	}
}
