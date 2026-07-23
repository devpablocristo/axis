package virployees

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/mcpgovernance"
	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
)

func prepareRuntimeActionV2(
	result dryrun.Result,
	principal executiongate.PrincipalContext,
) (*preparedactions.PreparedActionV2, error) {
	if !result.Intent.Matched || result.RequiredCapability == nil ||
		!result.RequiredCapability.Matched || result.Intent.Arguments == nil {
		return nil, nil
	}
	capability, ok := runtimeCapabilityForIntent(
		result.RuntimeContext.Capabilities,
		result.Intent.CapabilityID,
		result.Intent.CapabilityKey,
	)
	if !ok || strings.TrimSpace(capability.Manifest.ExecutorBindingID) == "" {
		return nil, nil
	}
	if err := mcpgovernance.ValidateJSONSchema(capability.Manifest.InputSchema, result.Intent.Arguments); err != nil {
		return nil, fmt.Errorf("runtime proposed invalid capability arguments: %w", err)
	}
	action, err := preparedactions.NewV2(preparedactions.V2Input{
		CapabilityID: capability.ID, ManifestHash: capability.ManifestHash,
		ExecutorBindingID: capability.Manifest.ExecutorBindingID, Operation: capability.Manifest.Operation,
		InputSchemaHash: capability.Manifest.InputSchemaHash, OutputSchemaHash: capability.Manifest.OutputSchemaHash,
		Arguments: result.Intent.Arguments, RequiredAutonomy: string(capability.RequiredAutonomy),
		PrincipalType: principal.Type, PrincipalID: principal.ID,
	})
	if err != nil {
		return nil, err
	}
	return &action, nil
}

func attachRuntimeActionPreview(result dryrun.Result) (dryrun.Result, error) {
	action, err := prepareRuntimeActionV2(result, executiongate.PrincipalContext{})
	if err != nil || action == nil {
		return result, err
	}
	arguments, err := action.ArgumentsMap()
	if err != nil {
		return dryrun.Result{}, err
	}
	capability, _ := runtimeCapabilityForIntent(
		result.RuntimeContext.Capabilities,
		result.Intent.CapabilityID,
		result.Intent.CapabilityKey,
	)
	result.PreparedAction = &dryrun.PreparedActionProposal{
		SchemaVersion: action.SchemaVersion, CapabilityID: action.CapabilityID,
		ManifestHash: action.ManifestHash, ExecutorBindingID: action.ExecutorBindingID,
		Operation: action.Operation, InputSchemaHash: action.InputSchemaHash,
		OutputSchemaHash: action.OutputSchemaHash, Arguments: arguments,
		RequiredAutonomy: action.RequiredAutonomy, IdempotencyHash: action.IdempotencyHash,
		InputSchema: capability.Manifest.InputSchema,
	}
	result.Draft = dryrun.Draft{
		Status: dryrun.DraftStatusReady, Action: action.Operation, Kind: "generic_action",
		Summary: "Prepare governed action", Fields: []dryrun.DraftField{},
		MissingFields: []dryrun.DraftMissingField{},
		Notes:         []string{"Arguments were validated against the assigned capability schema; no external action was executed."},
	}
	return result, nil
}

func validateRequestedRuntimeAction(
	result dryrun.Result,
	requested preparedactions.PreparedActionV2,
	principal executiongate.PrincipalContext,
) (*preparedactions.PreparedActionV2, error) {
	if err := requested.Validate(); err != nil {
		return nil, err
	}
	expected, err := prepareRuntimeActionV2(result, principal)
	if err != nil {
		return nil, err
	}
	if expected == nil {
		return nil, fmt.Errorf("prepared_action is not available for the proposed capability")
	}
	requestedArguments, err := requested.ArgumentsMap()
	if err != nil {
		return nil, err
	}
	expectedArguments, err := expected.ArgumentsMap()
	if err != nil {
		return nil, err
	}
	requestedRaw, _ := json.Marshal(requestedArguments)
	expectedRaw, _ := json.Marshal(expectedArguments)
	if requested.SchemaVersion != expected.SchemaVersion ||
		requested.CapabilityID != expected.CapabilityID ||
		requested.ManifestHash != expected.ManifestHash ||
		requested.ExecutorBindingID != expected.ExecutorBindingID ||
		requested.Operation != expected.Operation ||
		requested.InputSchemaHash != expected.InputSchemaHash ||
		requested.OutputSchemaHash != expected.OutputSchemaHash ||
		requested.RequiredAutonomy != expected.RequiredAutonomy ||
		!bytes.Equal(requestedRaw, expectedRaw) {
		return nil, fmt.Errorf("prepared_action no longer matches the assigned capability proposal")
	}
	return expected, nil
}

func prepareConfirmedActionV2(
	result dryrun.Result,
	confirmed executiongate.ConfirmedDraft,
	principal executiongate.PrincipalContext,
) (dryrun.Result, *preparedactions.PreparedActionV2, error) {
	capability, ok := runtimeCapability(result.RuntimeContext.Capabilities, result.Intent.CapabilityKey)
	if !ok || strings.TrimSpace(capability.Manifest.ExecutorBindingID) == "" {
		return result, nil, nil
	}
	actionName := strings.ToLower(strings.TrimSpace(confirmed.Action))
	if actionName == "" {
		return dryrun.Result{}, nil, fmt.Errorf("confirmed_draft.action is required")
	}
	if actionName != capability.CapabilityKey && actionName != capability.Manifest.Operation {
		return dryrun.Result{}, nil, fmt.Errorf("confirmed_draft.action must match the capability or manifest operation")
	}
	arguments, fields, err := confirmedArguments(capability, confirmed.Fields)
	if err != nil {
		return dryrun.Result{}, nil, err
	}
	if err := mcpgovernance.ValidateJSONSchema(capability.Manifest.InputSchema, arguments); err != nil {
		return dryrun.Result{}, nil, err
	}
	kind := strings.TrimSpace(confirmed.Kind)
	if kind == "" {
		kind = "generic_action"
	}
	result.Draft = dryrun.Draft{
		Status: dryrun.DraftStatusReady, Action: capability.CapabilityKey, Kind: kind,
		Summary: "Prepare governed action", Fields: fields,
		MissingFields: []dryrun.DraftMissingField{},
		Notes:         []string{"No external action will be executed before deterministic authorization."},
	}
	action, err := preparedactions.NewV2(preparedactions.V2Input{
		CapabilityID: capability.ID, ManifestHash: capability.ManifestHash,
		ExecutorBindingID: capability.Manifest.ExecutorBindingID, Operation: capability.Manifest.Operation,
		InputSchemaHash: capability.Manifest.InputSchemaHash, OutputSchemaHash: capability.Manifest.OutputSchemaHash,
		Arguments: arguments, RequiredAutonomy: string(capability.RequiredAutonomy),
		PrincipalType: principal.Type, PrincipalID: principal.ID,
	})
	if err != nil {
		return dryrun.Result{}, nil, err
	}
	return result, &action, nil
}

func confirmedArguments(capability capabilitydomain.Capability, fields []executiongate.ConfirmedDraftField) (map[string]any, []dryrun.DraftField, error) {
	properties, _ := capability.Manifest.InputSchema["properties"].(map[string]any)
	arguments := make(map[string]any, len(fields))
	draftFields := make([]dryrun.DraftField, 0, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			return nil, nil, fmt.Errorf("confirmed_draft fields require a key")
		}
		if _, exists := arguments[key]; exists {
			return nil, nil, fmt.Errorf("confirmed_draft fields must be unique")
		}
		property, _ := properties[key].(map[string]any)
		value, err := coerceConfirmedValue(strings.TrimSpace(field.Value), property)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", key, err)
		}
		arguments[key] = value
		draftFields = append(draftFields, dryrun.DraftField{
			Key: key, Label: key, Value: strings.TrimSpace(field.Value), Source: "confirmed",
		})
	}
	return arguments, draftFields, nil
}

func coerceConfirmedValue(raw string, schema map[string]any) (any, error) {
	switch schema["type"] {
	case "integer":
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("must be an integer")
		}
		return value, nil
	case "number":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("must be a number")
		}
		return value, nil
	case "boolean":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("must be a boolean")
		}
		return value, nil
	case "array", "object":
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("must be valid JSON")
		}
		return value, nil
	default:
		return raw, nil
	}
}
