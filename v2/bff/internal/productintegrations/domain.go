package productintegrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/productedge"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	SchemaVersion           = "axis.product-integration.v2"
	FunctionalSchemaVersion = "axis.product-integration.v3"
)

var (
	codePattern    = regexp.MustCompile(`^[a-z][a-z0-9._:-]{0,127}$`)
	versionPattern = regexp.MustCompile(`^v[1-9][0-9]*$`)
	hashPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type APIContract struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CapabilityRef = productedge.CapabilityRef

type EventContract = productedge.EventContract

type WebhookSubscription struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	SecretRef  string   `json:"secret_ref"`
}

type ServiceSection struct {
	SchemaVersion      string                `json:"schema_version"`
	APIContracts       []APIContract         `json:"api_contracts"`
	VirployeeIDs       []uuid.UUID           `json:"virployee_ids,omitempty"`
	PoolIDs            []uuid.UUID           `json:"pool_ids,omitempty"`
	Capabilities       []CapabilityRef       `json:"capabilities,omitempty"`
	ActionTypes        []string              `json:"action_types,omitempty"`
	GovernedOperations []GovernedOperation   `json:"governed_operations,omitempty"`
	AccessModes        []string              `json:"access_modes,omitempty"`
	Events             []EventContract       `json:"events,omitempty"`
	Webhooks           []WebhookSubscription `json:"webhooks,omitempty"`
}

type AuthenticationRequirements struct {
	Mode   string   `json:"mode"`
	Scopes []string `json:"scopes"`
}

type Limits struct {
	MaxRequestBytes int64 `json:"max_request_bytes"`
	MaxResultBytes  int64 `json:"max_result_bytes"`
	RatePerMinute   int   `json:"rate_per_minute"`
}

type Contract struct {
	SchemaVersion      string                     `json:"schema_version"`
	RequiredServices   []string                   `json:"required_services,omitempty"`
	Authentication     AuthenticationRequirements `json:"authentication"`
	Limits             Limits                     `json:"limits"`
	Services           map[string]ServiceSection  `json:"services,omitempty"`
	Entrypoints        []Entrypoint               `json:"entrypoints,omitempty"`
	Capabilities       []FunctionalCapability     `json:"capabilities,omitempty"`
	Events             []EventContract            `json:"events,omitempty"`
	GovernedOperations []GovernedOperation        `json:"governed_operations,omitempty"`
	ConnectorBindings  []ConnectorBinding         `json:"connector_bindings,omitempty"`
}

type Entrypoint struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}

type FunctionalCapability struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Version           string    `json:"version"`
	ManifestHash      string    `json:"manifest_hash"`
	ExecutorBindingID string    `json:"executor_binding_id"`
	Operation         string    `json:"operation"`
	InputSchemaHash   string    `json:"input_schema_hash"`
	OutputSchemaHash  string    `json:"output_schema_hash"`
	LegacyKey         string    `json:"legacy_key,omitempty"`
}

type GovernedOperation struct {
	CapabilityID   uuid.UUID `json:"capability_id"`
	Operation      string    `json:"operation"`
	RequiredScopes []string  `json:"required_scopes"`
}

type ConnectorBinding struct {
	ID          string `json:"id"`
	ConnectorID string `json:"connector_id"`
	Operation   string `json:"operation"`
	SecretRef   string `json:"secret_ref,omitempty"`
}

// FunctionalContract is the public, service-neutral v3 wire shape.
type FunctionalContract struct {
	SchemaVersion      string                     `json:"schema_version"`
	Authentication     AuthenticationRequirements `json:"authentication"`
	Limits             Limits                     `json:"limits"`
	Entrypoints        []Entrypoint               `json:"entrypoints,omitempty"`
	Capabilities       []FunctionalCapability     `json:"capabilities,omitempty"`
	Events             []EventContract            `json:"events,omitempty"`
	GovernedOperations []GovernedOperation        `json:"governed_operations,omitempty"`
	ConnectorBindings  []ConnectorBinding         `json:"connector_bindings,omitempty"`
}

type CreateVersionInput struct {
	Contract Contract `json:"contract"`
}

type Integration struct {
	ID              uuid.UUID  `json:"id"`
	OrgID           uuid.UUID  `json:"org_id"`
	ProductID       uuid.UUID  `json:"product_id"`
	ProductSurface  string     `json:"product_surface"`
	Lifecycle       string     `json:"lifecycle"`
	ActiveVersionID *uuid.UUID `json:"active_version_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Version struct {
	ID               uuid.UUID  `json:"id"`
	IntegrationID    uuid.UUID  `json:"integration_id"`
	Revision         int64      `json:"revision"`
	SchemaVersion    string     `json:"schema_version"`
	Contract         Contract   `json:"contract"`
	ContractHash     string     `json:"contract_hash"`
	RequiredServices []string   `json:"required_services"`
	Status           string     `json:"status"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	ActivatedBy      string     `json:"activated_by,omitempty"`
	ActivatedAt      *time.Time `json:"activated_at,omitempty"`
}

func (v Version) MarshalJSON() ([]byte, error) {
	type versionAlias Version
	raw, err := json.Marshal(versionAlias(v))
	if err != nil || v.SchemaVersion != FunctionalSchemaVersion {
		return raw, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	delete(out, "required_services")
	return json.Marshal(out)
}

type ValidationCheck struct {
	Service string `json:"service"`
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ServiceSnapshot struct {
	Service     string    `json:"service"`
	VersionID   uuid.UUID `json:"version_id"`
	Valid       bool      `json:"valid"`
	ContentHash string    `json:"content_hash"`
}

type ValidationReport struct {
	ID               uuid.UUID                  `json:"id"`
	VersionID        uuid.UUID                  `json:"version_id"`
	ContractHash     string                     `json:"contract_hash"`
	Valid            bool                       `json:"valid"`
	Checks           []ValidationCheck          `json:"checks"`
	ServiceSnapshots map[string]ServiceSnapshot `json:"service_snapshots"`
	CreatedBy        string                     `json:"created_by"`
	CreatedAt        time.Time                  `json:"created_at"`
}

type Readiness struct {
	ProductID      uuid.UUID                   `json:"product_id"`
	ProductSurface string                      `json:"product_surface"`
	Lifecycle      string                      `json:"lifecycle"`
	Status         string                      `json:"status"`
	ContractHash   string                      `json:"contract_hash,omitempty"`
	Version        int64                       `json:"version,omitempty"`
	Services       map[string]ServiceReadiness `json:"services"`
	CheckedAt      time.Time                   `json:"checked_at"`
}

type ServiceReadiness struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

type CreateCredentialInput struct {
	ServicePrincipal string   `json:"service_principal"`
	Scopes           []string `json:"scopes"`
}

type Credential struct {
	ID               uuid.UUID  `json:"id"`
	OrgID            uuid.UUID  `json:"org_id"`
	ProductID        uuid.UUID  `json:"product_id"`
	IntegrationID    uuid.UUID  `json:"integration_id"`
	KeyPrefix        string     `json:"key_prefix"`
	ServicePrincipal string     `json:"service_principal"`
	Scopes           []string   `json:"scopes"`
	Status           string     `json:"status"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	RotatedAt        *time.Time `json:"rotated_at,omitempty"`
	RevokedBy        string     `json:"revoked_by,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	Secret           string     `json:"secret,omitempty"`
}

type MachineBinding = productedge.MachineBinding

func normalizeContract(in Contract) (Contract, error) {
	in.SchemaVersion = strings.TrimSpace(in.SchemaVersion)
	switch in.SchemaVersion {
	case SchemaVersion:
		return normalizeLegacyContract(in)
	case FunctionalSchemaVersion:
		return normalizeFunctionalContract(in)
	default:
		return Contract{}, domainerr.Validation("unsupported product integration schema version")
	}
}

func normalizeLegacyContract(in Contract) (Contract, error) {
	if len(in.RequiredServices) == 0 || len(in.RequiredServices) > 3 {
		return Contract{}, domainerr.Validation("required services are invalid")
	}
	services := make(map[string]struct{}, len(in.RequiredServices))
	for i := range in.RequiredServices {
		in.RequiredServices[i] = strings.ToLower(strings.TrimSpace(in.RequiredServices[i]))
		if in.RequiredServices[i] != "bff" && in.RequiredServices[i] != "companion" && in.RequiredServices[i] != "nexus" {
			return Contract{}, domainerr.Validation("required service is unsupported")
		}
		services[in.RequiredServices[i]] = struct{}{}
	}
	if _, ok := services["bff"]; !ok {
		return Contract{}, domainerr.Validation("bff must be a required service")
	}
	in.RequiredServices = in.RequiredServices[:0]
	for _, service := range []string{"bff", "companion", "nexus"} {
		if _, ok := services[service]; ok {
			in.RequiredServices = append(in.RequiredServices, service)
		}
	}
	in.Authentication.Mode = strings.ToLower(strings.TrimSpace(in.Authentication.Mode))
	if in.Authentication.Mode != "api_key" {
		return Contract{}, domainerr.Validation("authentication mode must be api_key")
	}
	if len(in.Authentication.Scopes) == 0 || len(in.Authentication.Scopes) > 64 {
		return Contract{}, domainerr.Validation("authentication scopes are required")
	}
	var err error
	in.Authentication.Scopes, err = normalizeCodes(in.Authentication.Scopes)
	if err != nil {
		return Contract{}, domainerr.Validation("authentication scope is invalid")
	}
	if in.Limits.MaxRequestBytes < 1 || in.Limits.MaxRequestBytes > 256<<20 ||
		in.Limits.MaxResultBytes < 1 || in.Limits.MaxResultBytes > 256<<20 ||
		in.Limits.RatePerMinute < 1 || in.Limits.RatePerMinute > 100000 {
		return Contract{}, domainerr.Validation("integration limits are invalid")
	}
	if len(in.Services) != len(in.RequiredServices) {
		return Contract{}, domainerr.Validation("service sections must match required services")
	}
	normalizedServices := make(map[string]ServiceSection, len(in.Services))
	for name, section := range in.Services {
		name = strings.ToLower(strings.TrimSpace(name))
		if _, required := services[name]; !required {
			return Contract{}, domainerr.Validation("unexpected service section")
		}
		normalized, err := normalizeServiceSection(name, section)
		if err != nil {
			return Contract{}, err
		}
		normalizedServices[name] = normalized
	}
	in.Services = normalizedServices
	return in, nil
}

func normalizeServiceSection(service string, in ServiceSection) (ServiceSection, error) {
	in.SchemaVersion = strings.TrimSpace(in.SchemaVersion)
	if in.SchemaVersion != SchemaVersion && in.SchemaVersion != FunctionalSchemaVersion {
		return ServiceSection{}, domainerr.Validation(service + " section schema version is invalid")
	}
	if len(in.APIContracts) == 0 || len(in.APIContracts) > 32 {
		return ServiceSection{}, domainerr.Validation(service + " API contracts are required")
	}
	seenAPIs := map[string]struct{}{}
	for i := range in.APIContracts {
		in.APIContracts[i].Name = strings.ToLower(strings.TrimSpace(in.APIContracts[i].Name))
		in.APIContracts[i].Version = strings.ToLower(strings.TrimSpace(in.APIContracts[i].Version))
		if !codePattern.MatchString(in.APIContracts[i].Name) || !versionPattern.MatchString(in.APIContracts[i].Version) {
			return ServiceSection{}, domainerr.Validation(service + " API contract is invalid")
		}
		key := in.APIContracts[i].Name + "@" + in.APIContracts[i].Version
		if _, ok := seenAPIs[key]; ok {
			return ServiceSection{}, domainerr.Validation(service + " API contracts must be unique")
		}
		seenAPIs[key] = struct{}{}
	}
	slices.SortFunc(in.APIContracts, func(a, b APIContract) int {
		return strings.Compare(a.Name+"@"+a.Version, b.Name+"@"+b.Version)
	})
	if len(in.VirployeeIDs) > 256 || len(in.PoolIDs) > 256 || len(in.Capabilities) > 512 {
		return ServiceSection{}, domainerr.Validation(service + " entrypoint limits exceeded")
	}
	for _, id := range append(slices.Clone(in.VirployeeIDs), in.PoolIDs...) {
		if id == uuid.Nil {
			return ServiceSection{}, domainerr.Validation(service + " entrypoint ID is invalid")
		}
	}
	for i := range in.Capabilities {
		ref := &in.Capabilities[i]
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Key = strings.ToLower(strings.TrimSpace(ref.Key))
		ref.Version = strings.TrimSpace(ref.Version)
		ref.ManifestHash = strings.ToLower(strings.TrimSpace(ref.ManifestHash))
		if ref.ID != "" {
			id, err := uuid.Parse(ref.ID)
			if err != nil || id == uuid.Nil {
				return ServiceSection{}, domainerr.Validation("capability ID is invalid")
			}
			ref.ID = id.String()
		}
		keyValid := codePattern.MatchString(ref.Key)
		if in.SchemaVersion == FunctionalSchemaVersion {
			keyValid = ref.Key == "" || keyValid
		}
		idRequired := in.SchemaVersion == FunctionalSchemaVersion
		if (idRequired && ref.ID == "") || !keyValid || ref.Version == "" ||
			len(ref.Version) > 64 || !hashPattern.MatchString(ref.ManifestHash) {
			return ServiceSection{}, domainerr.Validation("capability reference is invalid")
		}
	}
	slices.SortFunc(in.Capabilities, func(a, b CapabilityRef) int {
		return strings.Compare(a.Key+"@"+a.Version, b.Key+"@"+b.Version)
	})
	var err error
	if len(in.ActionTypes) > 0 {
		in.ActionTypes, err = normalizeCodes(in.ActionTypes)
		if err != nil {
			return ServiceSection{}, domainerr.Validation("action type reference is invalid")
		}
	}
	if len(in.AccessModes) > 0 {
		in.AccessModes, err = normalizeCodes(in.AccessModes)
		if err != nil {
			return ServiceSection{}, domainerr.Validation("access mode is invalid")
		}
		for _, mode := range in.AccessModes {
			if mode != "direct" && mode != "via_companion" && mode != "via_orchestrator" {
				return ServiceSection{}, domainerr.Validation("access mode must be direct, via_companion, or via_orchestrator")
			}
		}
	}
	if service == "nexus" && len(in.AccessModes) == 0 {
		return ServiceSection{}, domainerr.Validation("nexus access mode is required")
	}
	if len(in.Events) > 128 || len(in.Webhooks) > 32 {
		return ServiceSection{}, domainerr.Validation(service + " event or webhook limits exceeded")
	}
	for i := range in.Events {
		event := &in.Events[i]
		event.Type = strings.ToLower(strings.TrimSpace(event.Type))
		event.Version = strings.ToLower(strings.TrimSpace(event.Version))
		event.SchemaHash = strings.ToLower(strings.TrimSpace(event.SchemaHash))
		if !codePattern.MatchString(event.Type) || !versionPattern.MatchString(event.Version) ||
			!hashPattern.MatchString(event.SchemaHash) || !validJSONObject(event.Schema) {
			return ServiceSection{}, domainerr.Validation("event contract is invalid")
		}
		sum := sha256.Sum256(event.Schema)
		if hex.EncodeToString(sum[:]) != event.SchemaHash {
			return ServiceSection{}, domainerr.Validation("event schema hash does not match")
		}
	}
	for i := range in.Webhooks {
		hook := &in.Webhooks[i]
		hook.URL = strings.TrimSpace(hook.URL)
		hook.SecretRef = strings.TrimSpace(hook.SecretRef)
		parsed, parseErr := url.Parse(hook.URL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
			!strings.HasPrefix(hook.SecretRef, "secret://") || len(hook.SecretRef) > 512 {
			return ServiceSection{}, domainerr.Validation("webhook configuration is unsafe")
		}
		hook.EventTypes, err = normalizeCodes(hook.EventTypes)
		if err != nil || len(hook.EventTypes) == 0 {
			return ServiceSection{}, domainerr.Validation("webhook event types are invalid")
		}
	}
	return in, nil
}

func normalizeCodes(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !codePattern.MatchString(value) {
			return nil, domainerr.Validation("code is invalid")
		}
		if _, exists := seen[value]; exists {
			return nil, domainerr.Validation("codes must be unique")
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out, nil
}

func validJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 64<<10 || !json.Valid(raw) {
		return false
	}
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && value != nil
}

func contractHash(contract Contract) (string, error) {
	body, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
