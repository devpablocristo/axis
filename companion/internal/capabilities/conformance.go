package capabilities

import (
	"fmt"
	"strings"
	"time"
)

func CheckManifestConformance(manifest Manifest) (map[string]bool, []string) {
	manifest = manifest.Normalize()
	checks := map[string]bool{
		"manifest_valid":        false,
		"version_compatibility": false,
		"schema_contracts":      false,
		"schema_types":          false,
		"required_scopes":       false,
		"side_effect_contract":  false,
		"evidence_schema":       false,
		"nexus_binding":         false,
		"nexus_write_metadata":  false,
		"idempotency":           false,
		"rollback_contract":     false,
		"timeout":               false,
		"retries":               false,
		"postconditions":        false,
		"observability_tags":    false,
		"required_evidence":     false,
		"tenant_configurable":   false,
		"rate_and_cost_classes": false,
	}
	var errs []string

	if err := manifest.Validate(); err != nil {
		errs = append(errs, err.Error())
	} else {
		checks["manifest_valid"] = true
	}
	if manifest.SchemaVersion == SchemaVersion && semverPattern.MatchString(manifest.Version) {
		checks["version_compatibility"] = true
	} else {
		errs = append(errs, "schema_version must be capability_manifest.v1 and version must be semver")
	}
	if validateObjectSchema("input_schema", manifest.InputSchema) == nil &&
		validateObjectSchema("output_schema", manifest.OutputSchema) == nil &&
		validateObjectSchema("evidence_schema", manifest.EvidenceSchema) == nil {
		checks["schema_contracts"] = true
	} else {
		errs = append(errs, "input, output and evidence schemas must be valid object schemas")
	}
	if validateSchemaTypes(manifest.InputSchema) == nil &&
		validateSchemaTypes(manifest.OutputSchema) == nil &&
		validateSchemaTypes(manifest.EvidenceSchema) == nil {
		checks["schema_types"] = true
	} else {
		errs = append(errs, "schemas may only use supported JSON schema primitive types")
	}
	if validateRequiredScopes(manifest.RequiredScopes) == nil {
		checks["required_scopes"] = true
	} else {
		errs = append(errs, "required_scopes must declare at least one non-empty scope")
	}
	if validateSideEffectContract(manifest) == nil {
		checks["side_effect_contract"] = true
	} else {
		errs = append(errs, "action_type and side_effect_type are incompatible")
	}
	if validateEvidenceSchema(manifest.EvidenceSchema) == nil {
		checks["evidence_schema"] = true
	} else {
		errs = append(errs, "evidence_schema must declare at least one evidence property")
	}
	if err := validateRequiredEvidence(manifest.RequiredEvidence, manifest.EvidenceSchema); err == nil {
		checks["required_evidence"] = true
	} else {
		errs = append(errs, err.Error())
	}
	if !manifest.ApprovalRequired || strings.TrimSpace(manifest.NexusActionType) != "" {
		checks["nexus_binding"] = true
	} else {
		errs = append(errs, "approval-required capabilities must declare nexus_action_type")
	}
	if validateNexusWriteMetadata(manifest) == nil {
		checks["nexus_write_metadata"] = true
	} else {
		errs = append(errs, "write or side-effect capabilities must require approval, nexus_action_type and required idempotency")
	}
	if manifest.SideEffectType == SideEffectRead || manifest.IdempotencyMode == IdempotencyRequired {
		checks["idempotency"] = true
	} else {
		errs = append(errs, "side-effect capabilities must require idempotency")
	}
	if !manifest.RollbackSupported || strings.TrimSpace(manifest.RollbackCapabilityID) != "" || manifest.CompensationStrategy == "manual" {
		checks["rollback_contract"] = true
	} else {
		errs = append(errs, "automatic rollback requires rollback_capability_id")
	}
	if _, err := time.ParseDuration(manifest.Timeout); err == nil {
		checks["timeout"] = true
	} else {
		errs = append(errs, fmt.Sprintf("invalid timeout %q", manifest.Timeout))
	}
	if manifest.Retries.MaxAttempts >= 1 {
		checks["retries"] = true
	} else {
		errs = append(errs, "retries.max_attempts must be >= 1")
	}
	if manifest.SideEffectType == SideEffectRead || len(manifest.Postconditions) > 0 {
		checks["postconditions"] = true
	} else {
		errs = append(errs, "side-effect capabilities require postconditions")
	}
	if len(manifest.ObservabilityTags) > 0 {
		checks["observability_tags"] = true
	} else {
		errs = append(errs, "observability_tags are required")
	}
	if manifest.TenantConfigurable || !manifest.EnabledByDefault {
		checks["tenant_configurable"] = true
	} else {
		errs = append(errs, "enabled-by-default capabilities should be tenant configurable")
	}
	if strings.TrimSpace(manifest.RateLimitClass) != "" && strings.TrimSpace(manifest.CostClass) != "" {
		checks["rate_and_cost_classes"] = true
	} else {
		errs = append(errs, "rate_limit_class and cost_class are required")
	}
	return checks, dedupeStrings(errs)
}

func validateRequiredScopes(scopes []string) error {
	for _, scope := range scopes {
		if strings.TrimSpace(scope) != "" {
			return nil
		}
	}
	return fmt.Errorf("required scopes missing")
}

func validateSideEffectContract(manifest Manifest) error {
	switch manifest.ActionType {
	case ActionTypeRead:
		if manifest.SideEffectType != SideEffectRead {
			return fmt.Errorf("read capability cannot declare side-effect %q", manifest.SideEffectType)
		}
	case ActionTypeWrite:
		if manifest.SideEffectType == SideEffectRead {
			return fmt.Errorf("write capability must declare a side-effect type")
		}
	default:
		return fmt.Errorf("invalid action_type %q", manifest.ActionType)
	}
	return nil
}

func validateEvidenceSchema(schema map[string]any) error {
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return fmt.Errorf("evidence properties missing")
	}
	return nil
}

func validateNexusWriteMetadata(manifest Manifest) error {
	if manifest.ActionType != ActionTypeWrite && manifest.SideEffectType == SideEffectRead {
		return nil
	}
	if !manifest.ApprovalRequired || strings.TrimSpace(manifest.NexusActionType) == "" {
		return fmt.Errorf("approval metadata missing")
	}
	if manifest.IdempotencyMode != IdempotencyRequired {
		return fmt.Errorf("idempotency is required")
	}
	return nil
}

func validateSchemaTypes(schema map[string]any) error {
	return walkSchemaTypes(schema)
}

func walkSchemaTypes(node any) error {
	switch typed := node.(type) {
	case map[string]any:
		if rawType, ok := typed["type"]; ok {
			if !schemaTypeAllowed(rawType) {
				return fmt.Errorf("unsupported schema type %v", rawType)
			}
		}
		for _, value := range typed {
			if err := walkSchemaTypes(value); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range typed {
			if err := walkSchemaTypes(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func schemaTypeAllowed(raw any) bool {
	switch value := raw.(type) {
	case string:
		return oneOf(value, "object", "array", "string", "number", "integer", "boolean", "null")
	case []any:
		for _, item := range value {
			s, ok := item.(string)
			if !ok || !oneOf(s, "object", "array", "string", "number", "integer", "boolean", "null") {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
