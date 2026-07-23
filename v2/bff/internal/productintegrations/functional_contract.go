package productintegrations

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func normalizeFunctionalContract(in Contract) (Contract, error) {
	if len(in.RequiredServices) != 0 || len(in.Services) != 0 {
		return Contract{}, domainerr.Validation("v3 contracts cannot contain service sections")
	}
	in.Authentication.Mode = strings.ToLower(strings.TrimSpace(in.Authentication.Mode))
	if in.Authentication.Mode != "api_key" {
		return Contract{}, domainerr.Validation("authentication mode must be api_key")
	}
	var err error
	in.Authentication.Scopes, err = normalizeCodes(in.Authentication.Scopes)
	if err != nil || len(in.Authentication.Scopes) == 0 || len(in.Authentication.Scopes) > 64 {
		return Contract{}, domainerr.Validation("authentication scopes are invalid")
	}
	if in.Limits.MaxRequestBytes < 1 || in.Limits.MaxRequestBytes > 256<<20 ||
		in.Limits.MaxResultBytes < 1 || in.Limits.MaxResultBytes > 256<<20 ||
		in.Limits.RatePerMinute < 1 || in.Limits.RatePerMinute > 100000 {
		return Contract{}, domainerr.Validation("integration limits are invalid")
	}
	if len(in.Entrypoints) > 512 || len(in.Capabilities) > 512 || len(in.Events) > 128 ||
		len(in.GovernedOperations) > 512 || len(in.ConnectorBindings) > 512 {
		return Contract{}, domainerr.Validation("functional contract limits exceeded")
	}

	entrypoints := make(map[string]struct{}, len(in.Entrypoints))
	for i := range in.Entrypoints {
		entrypoint := &in.Entrypoints[i]
		entrypoint.Kind = strings.ToLower(strings.TrimSpace(entrypoint.Kind))
		if (entrypoint.Kind != "virployee" && entrypoint.Kind != "routing_pool") || entrypoint.ID == uuid.Nil {
			return Contract{}, domainerr.Validation("entrypoint is invalid")
		}
		key := entrypoint.Kind + ":" + entrypoint.ID.String()
		if _, exists := entrypoints[key]; exists {
			return Contract{}, domainerr.Validation("entrypoints must be unique")
		}
		entrypoints[key] = struct{}{}
	}
	slices.SortFunc(in.Entrypoints, func(a, b Entrypoint) int {
		return strings.Compare(a.Kind+":"+a.ID.String(), b.Kind+":"+b.ID.String())
	})

	capabilities := make(map[uuid.UUID]FunctionalCapability, len(in.Capabilities))
	for i := range in.Capabilities {
		capability := &in.Capabilities[i]
		capability.Name = strings.TrimSpace(capability.Name)
		capability.Version = strings.TrimSpace(capability.Version)
		capability.ManifestHash = strings.ToLower(strings.TrimSpace(capability.ManifestHash))
		capability.ExecutorBindingID = strings.ToLower(strings.TrimSpace(capability.ExecutorBindingID))
		capability.Operation = strings.ToLower(strings.TrimSpace(capability.Operation))
		capability.InputSchemaHash = strings.ToLower(strings.TrimSpace(capability.InputSchemaHash))
		capability.OutputSchemaHash = strings.ToLower(strings.TrimSpace(capability.OutputSchemaHash))
		capability.LegacyKey = strings.ToLower(strings.TrimSpace(capability.LegacyKey))
		if capability.ID == uuid.Nil || capability.Name == "" || len(capability.Name) > 200 ||
			capability.Version == "" || len(capability.Version) > 64 ||
			!hashPattern.MatchString(capability.ManifestHash) ||
			!codePattern.MatchString(capability.ExecutorBindingID) ||
			!codePattern.MatchString(capability.Operation) ||
			!hashPattern.MatchString(capability.InputSchemaHash) ||
			!hashPattern.MatchString(capability.OutputSchemaHash) ||
			(capability.LegacyKey != "" && !codePattern.MatchString(capability.LegacyKey)) {
			return Contract{}, domainerr.Validation("functional capability is invalid")
		}
		if _, exists := capabilities[capability.ID]; exists {
			return Contract{}, domainerr.Validation("capability IDs must be unique")
		}
		capabilities[capability.ID] = *capability
	}
	slices.SortFunc(in.Capabilities, func(a, b FunctionalCapability) int {
		return strings.Compare(a.ID.String(), b.ID.String())
	})

	events := map[string]struct{}{}
	for i := range in.Events {
		event := &in.Events[i]
		event.Type = strings.ToLower(strings.TrimSpace(event.Type))
		event.Version = strings.ToLower(strings.TrimSpace(event.Version))
		event.SchemaHash = strings.ToLower(strings.TrimSpace(event.SchemaHash))
		if !codePattern.MatchString(event.Type) || !versionPattern.MatchString(event.Version) ||
			!hashPattern.MatchString(event.SchemaHash) || !validJSONObject(event.Schema) {
			return Contract{}, domainerr.Validation("event contract is invalid")
		}
		sum := sha256.Sum256(event.Schema)
		if hex.EncodeToString(sum[:]) != event.SchemaHash {
			return Contract{}, domainerr.Validation("event schema hash does not match")
		}
		key := event.Type + "@" + event.Version
		if _, exists := events[key]; exists {
			return Contract{}, domainerr.Validation("event contracts must be unique")
		}
		events[key] = struct{}{}
	}
	slices.SortFunc(in.Events, func(a, b EventContract) int {
		return strings.Compare(a.Type+"@"+a.Version, b.Type+"@"+b.Version)
	})

	operations := map[string]struct{}{}
	for i := range in.GovernedOperations {
		operation := &in.GovernedOperations[i]
		operation.Operation = strings.ToLower(strings.TrimSpace(operation.Operation))
		if operation.CapabilityID == uuid.Nil || !codePattern.MatchString(operation.Operation) {
			return Contract{}, domainerr.Validation("governed operation is invalid")
		}
		capability, exists := capabilities[operation.CapabilityID]
		if !exists {
			return Contract{}, domainerr.Validation("governed operation capability is not declared")
		}
		if capability.Operation != operation.Operation {
			return Contract{}, domainerr.Validation("governed operation does not match the capability operation")
		}
		operation.RequiredScopes, err = normalizeCodes(operation.RequiredScopes)
		if err != nil || len(operation.RequiredScopes) == 0 {
			return Contract{}, domainerr.Validation("governed operation scopes are invalid")
		}
		key := operation.CapabilityID.String() + ":" + operation.Operation
		if _, exists := operations[key]; exists {
			return Contract{}, domainerr.Validation("governed operations must be unique")
		}
		operations[key] = struct{}{}
		for _, scope := range operation.RequiredScopes {
			if !slices.Contains(in.Authentication.Scopes, scope) {
				return Contract{}, domainerr.Validation("governed operation scope is not authorized")
			}
		}
	}
	slices.SortFunc(in.GovernedOperations, func(a, b GovernedOperation) int {
		return strings.Compare(a.CapabilityID.String()+":"+a.Operation, b.CapabilityID.String()+":"+b.Operation)
	})

	bindings := map[string]ConnectorBinding{}
	for i := range in.ConnectorBindings {
		binding := &in.ConnectorBindings[i]
		binding.ID = strings.ToLower(strings.TrimSpace(binding.ID))
		binding.ConnectorID = strings.ToLower(strings.TrimSpace(binding.ConnectorID))
		binding.Operation = strings.ToLower(strings.TrimSpace(binding.Operation))
		binding.SecretRef = strings.TrimSpace(binding.SecretRef)
		if !codePattern.MatchString(binding.ID) || !codePattern.MatchString(binding.ConnectorID) ||
			!codePattern.MatchString(binding.Operation) ||
			(binding.SecretRef != "" && (!strings.HasPrefix(binding.SecretRef, "secret://") || len(binding.SecretRef) > 512)) {
			return Contract{}, domainerr.Validation("connector binding is invalid")
		}
		if _, exists := bindings[binding.ID]; exists {
			return Contract{}, domainerr.Validation("connector binding IDs must be unique")
		}
		bindings[binding.ID] = *binding
	}
	slices.SortFunc(in.ConnectorBindings, func(a, b ConnectorBinding) int {
		return strings.Compare(a.ID, b.ID)
	})
	for _, capability := range in.Capabilities {
		binding, exists := bindings[capability.ExecutorBindingID]
		if !exists || binding.Operation != capability.Operation {
			return Contract{}, domainerr.Validation("capability executor binding is not declared")
		}
	}
	return in, nil
}

// TranslateV2ToV3 converts a validated legacy contract to the functional
// service-neutral shape. Capability UUIDs must already be present; a technical
// key is retained only as an optional migration alias.
func TranslateV2ToV3(in Contract) (FunctionalContract, error) {
	legacy, err := normalizeLegacyContract(in)
	if err != nil {
		return FunctionalContract{}, err
	}
	out := FunctionalContract{
		SchemaVersion:  FunctionalSchemaVersion,
		Authentication: legacy.Authentication,
		Limits:         legacy.Limits,
	}
	if section, ok := legacy.Services["companion"]; ok {
		for _, id := range section.VirployeeIDs {
			out.Entrypoints = append(out.Entrypoints, Entrypoint{Kind: "virployee", ID: id})
		}
		for _, id := range section.PoolIDs {
			out.Entrypoints = append(out.Entrypoints, Entrypoint{Kind: "routing_pool", ID: id})
		}
		for _, capability := range section.Capabilities {
			id, parseErr := uuid.Parse(capability.ID)
			if parseErr != nil || id == uuid.Nil {
				return FunctionalContract{}, domainerr.Validation("v2 capability is missing its canonical UUID")
			}
			out.Capabilities = append(out.Capabilities, FunctionalCapability{
				ID: id, Name: capability.Key, Version: capability.Version,
				ManifestHash: capability.ManifestHash, LegacyKey: capability.Key,
				ExecutorBindingID: "legacy.executor", Operation: "invoke",
				InputSchemaHash: capability.ManifestHash, OutputSchemaHash: capability.ManifestHash,
			})
			if !slices.ContainsFunc(out.ConnectorBindings, func(binding ConnectorBinding) bool {
				return binding.ID == "legacy.executor"
			}) {
				out.ConnectorBindings = append(out.ConnectorBindings, ConnectorBinding{
					ID: "legacy.executor", ConnectorID: "legacy.executor", Operation: "invoke",
				})
			}
		}
		out.Events = append(out.Events, section.Events...)
	}
	if section, ok := legacy.Services["nexus"]; ok {
		for _, action := range section.ActionTypes {
			for _, capability := range out.Capabilities {
				if capability.LegacyKey == action {
					out.GovernedOperations = append(out.GovernedOperations, GovernedOperation{
						CapabilityID: capability.ID, Operation: "invoke",
						RequiredScopes: []string{"assist.write"},
					})
				}
			}
		}
	}
	normalized, err := normalizeFunctionalContract(Contract{
		SchemaVersion: out.SchemaVersion, Authentication: out.Authentication, Limits: out.Limits,
		Entrypoints: out.Entrypoints, Capabilities: out.Capabilities, Events: out.Events,
		GovernedOperations: out.GovernedOperations, ConnectorBindings: out.ConnectorBindings,
	})
	if err != nil {
		return FunctionalContract{}, err
	}
	return FunctionalContract{
		SchemaVersion: normalized.SchemaVersion, Authentication: normalized.Authentication, Limits: normalized.Limits,
		Entrypoints: normalized.Entrypoints, Capabilities: normalized.Capabilities, Events: normalized.Events,
		GovernedOperations: normalized.GovernedOperations, ConnectorBindings: normalized.ConnectorBindings,
	}, nil
}
