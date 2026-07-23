package preparedactions

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPreparedActionV2BindsCapabilityManifestExecutorAndArguments(t *testing.T) {
	input := V2Input{
		CapabilityID: uuid.New(), ManifestHash: strings.Repeat("a", 64),
		ExecutorBindingID: "connector.records", Operation: "records.search",
		InputSchemaHash: strings.Repeat("b", 64), OutputSchemaHash: strings.Repeat("c", 64),
		Arguments: map[string]any{"query": "labs"}, RequiredAutonomy: "A2",
	}
	action, err := NewV2(input)
	if err != nil {
		t.Fatal(err)
	}
	first, err := action.PayloadHash()
	if err != nil {
		t.Fatal(err)
	}
	input.Arguments = map[string]any{"query": "imaging"}
	changed, err := NewV2(input)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := changed.PayloadHash()
	if first == second {
		t.Fatal("changing arguments must invalidate the prepared action hash")
	}
}

func TestPreparedActionV2RejectsMissingExecutorBinding(t *testing.T) {
	_, err := NewV2(V2Input{
		CapabilityID: uuid.New(), ManifestHash: strings.Repeat("a", 64),
		InputSchemaHash: strings.Repeat("b", 64), OutputSchemaHash: strings.Repeat("c", 64),
		Arguments: map[string]any{}, RequiredAutonomy: "A1",
	})
	if err == nil {
		t.Fatal("missing executor binding must fail closed")
	}
}
