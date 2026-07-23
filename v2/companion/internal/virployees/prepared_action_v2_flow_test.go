package virployees

import (
	"strings"
	"testing"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

func TestRuntimeCapabilityArgumentsBecomeSchemaBoundPreparedActionV2(t *testing.T) {
	capabilityID := uuid.New()
	result := dryrun.Result{
		RuntimeContext: runtimecontext.Context{Capabilities: []capabilitydomain.Capability{{
			ID: capabilityID, CapabilityKey: "grain.fields.manage",
			RequiredAutonomy: virployeedomain.AutonomyA2,
			ManifestHash:     strings.Repeat("a", 64),
			Manifest: capabilitydomain.Manifest{
				ExecutorBindingID: "grain-connector", Operation: "fields.manage",
				InputSchemaHash: strings.Repeat("b", 64), OutputSchemaHash: strings.Repeat("c", 64),
				InputSchema: map[string]any{
					"type": "object", "required": []any{"field_id"},
					"properties": map[string]any{"field_id": map[string]any{"type": "string"}},
				},
			},
		}}},
		Intent: dryrun.Intent{
			Matched: true, CapabilityID: capabilityID.String(),
			CapabilityKey: "grain.fields.manage", Arguments: map[string]any{"field_id": "north-1"},
		},
		RequiredCapability: &dryrun.RequiredCapability{
			ID: capabilityID.String(), CapabilityKey: "grain.fields.manage",
			RequiredAutonomy: virployeedomain.AutonomyA2, Matched: true,
		},
		Decision: dryrun.DecisionAllowed,
	}
	got, err := attachRuntimeActionPreview(result)
	if err != nil {
		t.Fatal(err)
	}
	if got.PreparedAction == nil ||
		got.PreparedAction.CapabilityID != capabilityID.String() ||
		got.PreparedAction.ExecutorBindingID != "grain-connector" ||
		got.PreparedAction.Arguments["field_id"] != "north-1" {
		t.Fatalf("unexpected prepared action: %+v", got.PreparedAction)
	}
}

func TestRuntimeCapabilityArgumentsFailClosedOnSchemaMismatch(t *testing.T) {
	result := dryrun.Result{
		RuntimeContext: runtimecontext.Context{Capabilities: []capabilitydomain.Capability{{
			ID: uuid.New(), CapabilityKey: "grain.fields.manage",
			RequiredAutonomy: virployeedomain.AutonomyA2,
			ManifestHash:     strings.Repeat("a", 64),
			Manifest: capabilitydomain.Manifest{
				ExecutorBindingID: "grain-connector", Operation: "fields.manage",
				InputSchemaHash: strings.Repeat("b", 64), OutputSchemaHash: strings.Repeat("c", 64),
				InputSchema: map[string]any{
					"type": "object", "required": []any{"field_id"},
					"properties": map[string]any{"field_id": map[string]any{"type": "string"}},
				},
			},
		}}},
		Intent: dryrun.Intent{
			Matched: true, CapabilityKey: "grain.fields.manage",
			Arguments: map[string]any{"field_id": 42},
		},
		RequiredCapability: &dryrun.RequiredCapability{
			CapabilityKey: "grain.fields.manage", Matched: true,
		},
		Decision: dryrun.DecisionAllowed,
	}
	if _, err := attachRuntimeActionPreview(result); err == nil {
		t.Fatal("schema-invalid runtime arguments must fail closed")
	}
}
