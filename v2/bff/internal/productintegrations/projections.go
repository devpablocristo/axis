package productintegrations

// ProductInvocationProjection translates either public contract version to
// the participant snapshot used by the assist/event runtime.
func ProductInvocationProjection(contract Contract) (ServiceSection, bool, error) {
	if contract.SchemaVersion == SchemaVersion {
		section, ok := contract.Services["companion"]
		return section, ok, nil
	}
	if contract.SchemaVersion != FunctionalSchemaVersion {
		return ServiceSection{}, false, nil
	}
	if len(contract.Entrypoints) == 0 && len(contract.Capabilities) == 0 &&
		len(contract.Events) == 0 && len(contract.ConnectorBindings) == 0 {
		return ServiceSection{}, false, nil
	}
	section := ServiceSection{
		SchemaVersion: FunctionalSchemaVersion,
		APIContracts:  []APIContract{{Name: "axis.product-edge", Version: "v1"}},
		Events:        append([]EventContract(nil), contract.Events...),
	}
	for _, entrypoint := range contract.Entrypoints {
		switch entrypoint.Kind {
		case "virployee":
			section.VirployeeIDs = append(section.VirployeeIDs, entrypoint.ID)
		case "routing_pool":
			section.PoolIDs = append(section.PoolIDs, entrypoint.ID)
		}
	}
	for _, capability := range contract.Capabilities {
		section.Capabilities = append(section.Capabilities, CapabilityRef{
			ID: capability.ID.String(), Key: capability.LegacyKey,
			Version: capability.Version, ManifestHash: capability.ManifestHash,
		})
	}
	return section, true, nil
}

// GovernanceProjection projects governed functional operations without
// exposing Nexus (or any other implementation) in the public v3 contract.
func GovernanceProjection(contract Contract) (ServiceSection, bool, error) {
	if contract.SchemaVersion == SchemaVersion {
		section, ok := contract.Services["nexus"]
		return section, ok, nil
	}
	if contract.SchemaVersion != FunctionalSchemaVersion || len(contract.GovernedOperations) == 0 {
		return ServiceSection{}, false, nil
	}
	section := ServiceSection{
		SchemaVersion:      FunctionalSchemaVersion,
		APIContracts:       []APIContract{{Name: "axis.governance", Version: "v1"}},
		GovernedOperations: append([]GovernedOperation(nil), contract.GovernedOperations...),
		AccessModes:        []string{"via_orchestrator"},
	}
	return section, true, nil
}
