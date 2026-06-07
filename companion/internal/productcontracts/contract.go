package productcontracts

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/productevals"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/secrets"
)

const (
	StatusPassed = "passed"
	StatusFailed = "failed"
)

type Spec struct {
	Version        int                     `json:"version"`
	OrgID          string                  `json:"org_id"`
	Product        products.Product        `json:"product"`
	Installation   products.Installation   `json:"installation"`
	Identity       IdentitySpec            `json:"identity"`
	Capabilities   []capabilities.Manifest `json:"capabilities"`
	EvalPack       *productevals.Pack      `json:"eval_pack,omitempty"`
	ExpectedErrors []ExpectedError         `json:"expected_errors,omitempty"`
	Runtime        RuntimeReadiness        `json:"runtime,omitempty"`
	Metadata       map[string]any          `json:"metadata,omitempty"`
}

type IdentitySpec struct {
	OrgID            string   `json:"org_id"`
	ProductSurface   string   `json:"product_surface"`
	ActorID          string   `json:"actor_id"`
	ActorType        string   `json:"actor_type"`
	OnBehalfOf       string   `json:"on_behalf_of,omitempty"`
	ServicePrincipal bool     `json:"service_principal,omitempty"`
	Scopes           []string `json:"scopes"`
}

type ExpectedError struct {
	Scenario     string `json:"scenario"`
	ExpectedCode string `json:"expected_code"`
}

type RuntimeReadiness struct {
	Enabled   bool   `json:"enabled"`
	PolicyRef string `json:"policy_ref,omitempty"`
}

type StepResult struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type Report struct {
	Status           string       `json:"status"`
	ProductSurface   string       `json:"product_surface"`
	OrgID            string       `json:"org_id"`
	Steps            []StepResult `json:"steps"`
	BlockingFailures []string     `json:"blocking_failures,omitempty"`
}

func LoadSpec(path string) (Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("read product contract spec: %w", err)
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return Spec{}, fmt.Errorf("parse product contract spec: %w", err)
	}
	return spec, nil
}

func ValidateSpec(spec Spec) Report {
	spec = normalizeSpec(spec)
	steps := []StepResult{
		checkVersion(spec),
		checkProduct(spec),
		checkInstallation(spec),
		checkIdentity(spec),
		checkCapabilities(spec),
		checkExpectedErrors(spec),
		checkEvalPack(spec),
		checkRuntime(spec),
	}
	failures := make([]string, 0)
	for _, step := range steps {
		if step.Status != StatusPassed {
			failures = append(failures, step.ID)
		}
	}
	status := StatusPassed
	if len(failures) > 0 {
		status = StatusFailed
	}
	return Report{
		Status:           status,
		ProductSurface:   spec.Product.ProductSurface,
		OrgID:            spec.OrgID,
		Steps:            steps,
		BlockingFailures: failures,
	}
}

func checkVersion(spec Spec) StepResult {
	if spec.Version != 1 {
		return failed("contract_version", "Contract version", fmt.Sprintf("unsupported contract version %d", spec.Version))
	}
	return passed("contract_version", "Contract version")
}

func checkProduct(spec Spec) StepResult {
	product := spec.Product
	if product.ProductSurface == "" {
		return failed("product_registered", "Product registered", "product.product_surface is required")
	}
	if product.Status != products.ProductStatusActive {
		return failed("product_registered", "Product registered", "product must be active")
	}
	if product.DisplayName == "" {
		return failed("product_registered", "Product registered", "product.display_name is required")
	}
	return passed("product_registered", "Product registered")
}

func checkInstallation(spec Spec) StepResult {
	installation := spec.Installation
	if installation.OrgID != spec.OrgID {
		return failed("installation_active", "Active installation", "installation org_id must match contract org_id")
	}
	if installation.ProductSurface != spec.Product.ProductSurface {
		return failed("installation_active", "Active installation", "installation product_surface must match product")
	}
	if !installation.Enabled {
		return failed("installation_active", "Active installation", "installation must be enabled")
	}
	if installation.BaseURL == "" {
		return failed("installation_active", "Active installation", "enabled installation requires base_url")
	}
	if installation.AuthMode == "" {
		return failed("installation_active", "Active installation", "installation auth_mode is required")
	}
	if authModeNeedsSecretRef(installation.AuthMode) && installation.SecretRef == "" {
		return failed("installation_active", "Active installation", "auth_mode requires secret_ref")
	}
	if installation.SecretRef != "" {
		if err := secrets.ValidateRef(installation.SecretRef); err != nil {
			return failed("installation_active", "Active installation", fmt.Sprintf("invalid secret_ref: %v", err))
		}
	}
	return passed("installation_active", "Active installation")
}

func checkIdentity(spec Spec) StepResult {
	id := spec.Identity
	if id.OrgID != spec.OrgID {
		return failed("identity_context", "Identity/JWT context", "identity org_id must match contract org_id")
	}
	if id.ProductSurface != spec.Product.ProductSurface {
		return failed("identity_context", "Identity/JWT context", "identity product_surface must match product")
	}
	if id.ActorID == "" {
		return failed("identity_context", "Identity/JWT context", "actor_id is required")
	}
	if id.ActorType == "" {
		return failed("identity_context", "Identity/JWT context", "actor_type is required")
	}
	if len(id.Scopes) == 0 {
		return failed("identity_context", "Identity/JWT context", "at least one scope is required")
	}
	return passed("identity_context", "Identity/JWT context")
}

func checkCapabilities(spec Spec) StepResult {
	if len(spec.Capabilities) == 0 {
		return failed("capability_contracts", "Capability contracts", "at least one capability manifest is required")
	}
	var errs []string
	for _, manifest := range spec.Capabilities {
		manifest = manifest.Normalize()
		if manifest.ProductSurface != spec.Product.ProductSurface {
			errs = append(errs, fmt.Sprintf("%s product_surface mismatch", manifest.CapabilityID))
			continue
		}
		_, conformanceErrs := capabilities.CheckManifestConformance(manifest)
		for _, err := range conformanceErrs {
			errs = append(errs, fmt.Sprintf("%s: %s", manifest.CapabilityID, err))
		}
		if manifest.ActionType == capabilities.ActionTypeWrite || manifest.SideEffectType != capabilities.SideEffectRead {
			if !manifest.ApprovalRequired || strings.TrimSpace(manifest.NexusActionType) == "" {
				errs = append(errs, fmt.Sprintf("%s: write/side-effect capability requires Nexus metadata", manifest.CapabilityID))
			}
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return failed("capability_contracts", "Capability contracts", strings.Join(errs, "; "))
	}
	return passed("capability_contracts", "Capability contracts")
}

func checkExpectedErrors(spec Spec) StepResult {
	required := map[string]string{
		"installation_missing": "FORBIDDEN",
		"product_disabled":     "FORBIDDEN",
		"scope_missing":        "FORBIDDEN",
		"write_without_nexus":  "VALIDATION",
	}
	seen := make(map[string]string, len(spec.ExpectedErrors))
	for _, item := range spec.ExpectedErrors {
		seen[strings.TrimSpace(item.Scenario)] = strings.TrimSpace(item.ExpectedCode)
	}
	var errs []string
	for scenario, code := range required {
		if seen[scenario] == "" {
			errs = append(errs, fmt.Sprintf("missing expected error scenario %s", scenario))
			continue
		}
		if seen[scenario] != code {
			errs = append(errs, fmt.Sprintf("scenario %s expected code must be %s", scenario, code))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return failed("expected_errors", "Expected error contract", strings.Join(errs, "; "))
	}
	return passed("expected_errors", "Expected error contract")
}

func checkEvalPack(spec Spec) StepResult {
	if spec.EvalPack == nil {
		return failed("eval_pack", "Product eval pack", "eval_pack is required")
	}
	pack := *spec.EvalPack
	if pack.ProductSurface != spec.Product.ProductSurface {
		return failed("eval_pack", "Product eval pack", "eval pack product_surface must match product")
	}
	if err := productevals.ValidatePack(pack); err != nil {
		return failed("eval_pack", "Product eval pack", err.Error())
	}
	return passed("eval_pack", "Product eval pack")
}

func checkRuntime(spec Spec) StepResult {
	if !spec.Runtime.Enabled {
		return failed("runtime_enablement", "Runtime enablement", "runtime.enabled must be true before production enablement")
	}
	return passed("runtime_enablement", "Runtime enablement")
}

func normalizeSpec(spec Spec) Spec {
	spec.OrgID = strings.TrimSpace(spec.OrgID)
	spec.Product.ProductSurface = strings.TrimSpace(strings.ToLower(spec.Product.ProductSurface))
	spec.Product.DisplayName = strings.TrimSpace(spec.Product.DisplayName)
	spec.Product.Status = strings.TrimSpace(strings.ToLower(spec.Product.Status))
	spec.Installation.OrgID = strings.TrimSpace(spec.Installation.OrgID)
	spec.Installation.ProductSurface = strings.TrimSpace(strings.ToLower(spec.Installation.ProductSurface))
	spec.Installation.BaseURL = strings.TrimSpace(spec.Installation.BaseURL)
	spec.Installation.AuthMode = strings.TrimSpace(strings.ToLower(spec.Installation.AuthMode))
	spec.Installation.SecretRef = strings.TrimSpace(spec.Installation.SecretRef)
	spec.Identity.OrgID = strings.TrimSpace(spec.Identity.OrgID)
	spec.Identity.ProductSurface = strings.TrimSpace(strings.ToLower(spec.Identity.ProductSurface))
	spec.Identity.ActorID = strings.TrimSpace(spec.Identity.ActorID)
	spec.Identity.ActorType = strings.TrimSpace(spec.Identity.ActorType)
	if spec.EvalPack != nil {
		spec.EvalPack.ProductSurface = strings.TrimSpace(strings.ToLower(spec.EvalPack.ProductSurface))
	}
	return spec
}

func authModeNeedsSecretRef(authMode string) bool {
	switch authMode {
	case products.AuthModeAPIKeyRef, products.AuthModeOAuth2, products.AuthModeCustom:
		return true
	default:
		return false
	}
}

func passed(id, title string) StepResult {
	return StepResult{ID: id, Title: title, Status: StatusPassed}
}

func failed(id, title, err string) StepResult {
	return StepResult{ID: id, Title: title, Status: StatusFailed, Error: err}
}
