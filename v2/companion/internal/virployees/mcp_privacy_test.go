package virployees

import (
	"testing"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

func TestMCPGovernanceInputSendsOnlyMetadataToNexus(t *testing.T) {
	virployeeID := uuid.New()
	result := dryrun.Result{
		Input: "sensitive patient request",
		RuntimeContext: runtimecontext.Context{
			Virployee: domain.Virployee{ID: virployeeID, SupervisorUserID: "supervisor"},
			Capabilities: []capabilitydomain.Capability{{
				CapabilityKey: "calendar.events.create",
				Manifest:      capabilitydomain.Manifest{ProductSurface: "calendar"},
			}},
		},
		Intent: dryrun.Intent{CapabilityKey: "calendar.events.create", Domain: "calendar", Resource: "patient-name"},
		Draft:  dryrun.Draft{Fields: []dryrun.DraftField{{Key: "title", Value: "sensitive diagnosis"}}},
	}
	binding := &preparedactions.MCPContextBinding{
		CapabilityVersion: "1.0.0", ManifestHash: "manifest", ContextHash: "context",
		PayloadHash: "payload", IdempotencyHash: "idempotency", AssignmentID: uuid.NewString(), AssignmentVersion: 2,
		SubjectID: uuid.NewString(), CaseID: uuid.NewString(),
	}
	input := governanceInput("tenant", result, "binding", binding)
	if input.Reason != "MCP capability invocation" || input.TargetResource != binding.CaseID || input.ResourceType != "case" {
		t.Fatalf("MCP metadata envelope was not applied: %+v", input)
	}
	if input.ActionType != "calendar.events.create" || input.ProductSurface != "calendar" || input.BindingHash != "binding" {
		t.Fatalf("required governance metadata was lost: %+v", input)
	}
}
