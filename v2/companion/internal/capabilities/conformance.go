package capabilities

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/quotas"
	"github.com/devpablocristo/companion-v2/internal/secrets"
)

type QuotaPolicyChecker interface {
	HasActivePolicies(context.Context, string, string, []string) (bool, error)
}

var (
	manifestVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
	productSurfacePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
)

func validateConformance(ctx context.Context, capability domain.Capability, checker QuotaPolicyChecker) (domain.ConformanceReport, error) {
	report := domain.ConformanceReport{ManifestHash: capability.ManifestHash, Checks: make([]domain.ConformanceCheck, 0, 12)}
	add := func(key string, passed bool, reason string) {
		report.Checks = append(report.Checks, domain.ConformanceCheck{Key: key, Passed: passed, Reason: reason})
	}
	manifest := capability.Manifest
	computedHash, err := domain.HashManifest(manifest)
	if err != nil {
		return report, fmt.Errorf("hash capability manifest: %w", err)
	}
	add("manifest_integrity", capability.ManifestHash != "" && computedHash == capability.ManifestHash, "stored manifest hash must match the normalized manifest")

	add("manifest_version", manifestVersionPattern.MatchString(manifest.Version), "version must be semantic, for example 1.0.0")
	add("product_surface", productSurfacePattern.MatchString(manifest.ProductSurface), "product_surface is required and must be a stable lowercase key")
	add("input_schema", validObjectSchema(manifest.InputSchema), "input_schema must declare a JSON object type")
	add("output_schema", validObjectSchema(manifest.OutputSchema), "output_schema must declare a JSON object type")
	add("required_scopes", len(manifest.RequiredScopes) > 0, "at least one explicit scope is required")

	idempotencyOK := manifest.Idempotency.Mode == "not_applicable" ||
		(manifest.Idempotency.Mode == "required" && len(manifest.Idempotency.KeyFields) > 0)
	if capability.SideEffectClass == "write" {
		idempotencyOK = manifest.Idempotency.Mode == "required" && len(manifest.Idempotency.KeyFields) > 0
	}
	add("idempotency", idempotencyOK, "write capabilities require stable idempotency key fields")

	rollbackOK := manifest.RollbackMode == "none" || manifest.RollbackMode == "manual" || manifest.RollbackMode == "automatic"
	if capability.SideEffectClass == "write" {
		rollbackOK = manifest.RollbackMode == "manual" || manifest.RollbackMode == "automatic"
	}
	if manifest.RollbackMode == "automatic" {
		rollbackOK = rollbackOK && strings.TrimSpace(capability.RollbackCapabilityKey) != ""
	}
	add("rollback", rollbackOK, "write capabilities require manual or automatic rollback; automatic rollback requires rollback_capability_key")

	timeoutRetryOK := manifest.TimeoutMS >= 1 && manifest.TimeoutMS <= 300_000 &&
		manifest.Retry.MaxAttempts >= 1 && manifest.Retry.MaxAttempts <= 10 &&
		manifest.Retry.BackoffMS >= 1 && manifest.Retry.BackoffMS <= 60_000
	add("timeout_retries", timeoutRetryOK, "timeout and retry/backoff must be positive and bounded")
	add("postconditions", len(manifest.Postconditions) > 0, "at least one verifiable postcondition is required")

	governanceOK := capability.EvidenceRequired
	if capability.SideEffectClass == "write" {
		governanceOK = governanceOK && capability.RequiresNexusApproval && manifest.AttestationRequired
	}
	add("governance", governanceOK, "evidence is required; writes also require Nexus approval and signed attestation")

	secretRefsOK := true
	for _, ref := range manifest.SecretRefs {
		if !secrets.ValidRef(ref) {
			secretRefsOK = false
			break
		}
	}
	add("secrets", secretRefsOK, "credentials must be opaque Secret Manager references")

	costOK := oneOf(manifest.CostClass, "free", "low", "medium", "high")
	add("cost_class", costOK, "cost_class must be free, low, medium or high")

	quotaAreasOK := contains(manifest.QuotaAreas, quotas.AreaInbound)
	if capability.SideEffectClass == "write" {
		quotaAreasOK = quotaAreasOK && contains(manifest.QuotaAreas, quotas.AreaExecutors)
	}
	for _, area := range manifest.QuotaAreas {
		quotaAreasOK = quotaAreasOK && oneOf(area, quotas.AreaInbound, quotas.AreaLLM, quotas.AreaEmbeddings, quotas.AreaBytes, quotas.AreaExecutors)
	}
	if quotaAreasOK && checker != nil {
		configured, err := checker.HasActivePolicies(ctx, capability.OrgID, manifest.ProductSurface, manifest.QuotaAreas)
		if err != nil {
			return report, fmt.Errorf("check quota policies: %w", err)
		}
		quotaAreasOK = configured
	} else if checker == nil {
		quotaAreasOK = false
	}
	add("quotas", quotaAreasOK, "all declared quota areas need active organization/product policies; inbound is mandatory and writes also require executors")

	report.Conformant = capability.ManifestHash != ""
	for _, check := range report.Checks {
		report.Conformant = report.Conformant && check.Passed
	}
	return report, nil
}

func validObjectSchema(schema map[string]any) bool {
	typeName, ok := schema["type"].(string)
	return ok && typeName == "object"
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
