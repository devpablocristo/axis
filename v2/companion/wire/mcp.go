package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/mcpgovernance"
	"github.com/devpablocristo/companion-v2/internal/virployees"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

type mcpWriteGateAdapter struct{ virployees *virployees.UseCases }

func (a mcpWriteGateAdapter) SupportsMCPAction(capabilityKey string) bool {
	return a.virployees != nil && (capabilityKey == preparedactions.ActionCreate || capabilityKey == preparedactions.ActionDelete)
}

func (a mcpWriteGateAdapter) PrepareMCPAction(ctx context.Context, in mcpgovernance.WriteGateInput) (mcpgovernance.WriteGateResult, error) {
	if !a.SupportsMCPAction(in.Capability.CapabilityKey) {
		return mcpgovernance.WriteGateResult{}, fmt.Errorf("no governed executor is registered for capability %s", in.Capability.CapabilityKey)
	}
	fields := make([]executiongate.ConfirmedDraftField, 0, len(in.Arguments))
	keys := make([]string, 0, len(in.Arguments))
	for key := range in.Arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fieldKey := key
		if in.Capability.CapabilityKey == preparedactions.ActionDelete && key == "event_id" {
			fieldKey = "event_reference"
		}
		fields = append(fields, executiongate.ConfirmedDraftField{Key: fieldKey, Value: mcpFieldValue(in.Arguments[key])})
	}
	principal := executiongate.PrincipalContext{Type: in.Context.PrincipalType, ID: in.Context.PrincipalID}
	binding := preparedactions.MCPContextBinding{
		TenantID: in.Context.TenantID, ActorID: in.Context.ActorID,
		VirployeeID: in.Context.VirployeeID.String(), SubjectID: in.Context.SubjectID.String(),
		AssignmentID: in.Context.AssignmentID.String(), AssignmentVersion: in.Context.AssignmentVersion,
		CapabilityKey: in.Capability.CapabilityKey, CapabilityVersion: in.Capability.Manifest.Version,
		ManifestHash: in.Capability.ManifestHash, PolicyVersion: in.PolicyVersion,
		AuthorityHash: in.AuthorityHash, ContextHash: in.ContextHash, PayloadHash: in.PayloadHash,
		IdempotencyHash: mcpgovernance.HashString(in.IdempotencyKey),
	}
	if in.Context.CaseID != uuid.Nil {
		binding.CaseID = in.Context.CaseID.String()
	}
	result, err := a.virployees.ExecutionGateFromMCP(
		ctx, in.Context.TenantID, in.Context.VirployeeID,
		mcpgovernance.BuildDeterministicActionInput(in.Capability.CapabilityKey),
		&executiongate.ConfirmedDraft{Action: in.Capability.CapabilityKey, Kind: mcpDraftKind(in.Capability.CapabilityKey), Fields: fields},
		principal, binding,
	)
	if err != nil {
		return mcpgovernance.WriteGateResult{}, err
	}
	out := mcpgovernance.WriteGateResult{Status: "blocked", BindingHash: result.BindingHash}
	if result.Governance == nil {
		out.DecisionReason = result.Gate.NextStep
		return out, nil
	}
	out.ApprovalID = result.Governance.ApprovalID
	out.DecisionReason = result.Governance.DecisionReason
	if result.Governance.Decision == "require_approval" && strings.TrimSpace(result.Governance.ApprovalID) != "" {
		out.Status = "pending_approval"
	}
	return out, nil
}

func mcpFieldValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ",")
	default:
		raw, err := json.Marshal(value)
		if err == nil {
			return string(raw)
		}
		return fmt.Sprint(value)
	}
}

func mcpDraftKind(capabilityKey string) string {
	switch capabilityKey {
	case preparedactions.ActionCreate:
		return "calendar_event"
	case preparedactions.ActionDelete:
		return "calendar_event_delete"
	default:
		return "capability_action"
	}
}
