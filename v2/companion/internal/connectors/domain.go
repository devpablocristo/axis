package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const SchemaVersion = "axis.connector.v1"

const (
	DefaultMaxRequestBytes  int64 = 256 << 10
	DefaultMaxResponseBytes int64 = 1 << 20
	MaxRequestBytes         int64 = 1 << 20
	MaxResponseBytes        int64 = 4 << 20
)

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
var developmentSecretRefPattern = regexp.MustCompile(`^env://[A-Z][A-Z0-9_]{0,127}$`)

type OperationDescriptor struct {
	Name             string         `json:"name"`
	CapabilityID     string         `json:"capability_id"`
	InputSchema      map[string]any `json:"input_schema"`
	OutputSchema     map[string]any `json:"output_schema"`
	MaxRequestBytes  int64          `json:"max_request_bytes"`
	MaxResponseBytes int64          `json:"max_response_bytes"`
	TimeoutMS        int            `json:"timeout_ms"`
}

type Descriptor struct {
	SchemaVersion string                `json:"schema_version"`
	BindingID     string                `json:"binding_id"`
	BaseURL       string                `json:"base_url"`
	ProductID     string                `json:"product_id"`
	SecretRef     string                `json:"secret_ref"`
	Operations    []OperationDescriptor `json:"operations"`
}

type Registration struct {
	OrgID      string     `json:"org_id"`
	Descriptor Descriptor `json:"descriptor"`
}

const MaxRegistrationsJSONBytes = 1 << 20
const MaxRegistrations = 256
const MaxOperationsPerConnector = 512

func ParseRegistrations(raw string, environment string) ([]Registration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > MaxRegistrationsJSONBytes {
		return nil, fmt.Errorf("connector registrations exceed configuration limit")
	}
	var registrations []Registration
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registrations); err != nil {
		return nil, fmt.Errorf("decode connector registrations: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("connector registrations must contain one JSON array")
	}
	if len(registrations) > MaxRegistrations {
		return nil, fmt.Errorf("too many connector registrations")
	}
	seen := make(map[string]struct{}, len(registrations))
	for index := range registrations {
		registration := &registrations[index]
		registration.OrgID = strings.TrimSpace(registration.OrgID)
		if registration.OrgID == "" {
			return nil, fmt.Errorf("connector registration organization is required")
		}
		normalized, err := NormalizeDescriptor(registration.Descriptor, environment)
		if err != nil {
			return nil, fmt.Errorf("connector registration %d: %w", index, err)
		}
		registration.Descriptor = normalized
		key := registryKey(registration.OrgID, normalized.BindingID)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("connector registration organization and binding must be unique")
		}
		seen[key] = struct{}{}
	}
	return registrations, nil
}

func NormalizeDescriptor(input Descriptor, environment string) (Descriptor, error) {
	input.SchemaVersion = strings.TrimSpace(input.SchemaVersion)
	input.BindingID = strings.ToLower(strings.TrimSpace(input.BindingID))
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.ProductID = strings.TrimSpace(input.ProductID)
	input.SecretRef = strings.TrimSpace(input.SecretRef)
	if input.SchemaVersion != SchemaVersion || !identifierPattern.MatchString(input.BindingID) ||
		input.ProductID == "" || input.SecretRef == "" || len(input.Operations) == 0 ||
		len(input.Operations) > MaxOperationsPerConnector {
		return Descriptor{}, fmt.Errorf("connector descriptor is incomplete")
	}
	productID, err := uuid.Parse(input.ProductID)
	if err != nil || productID == uuid.Nil {
		return Descriptor{}, fmt.Errorf("connector product_id is invalid")
	}
	input.ProductID = productID.String()
	if isDevelopment(environment) {
		if !strings.HasPrefix(input.SecretRef, "secretmanager://") &&
			!developmentSecretRefPattern.MatchString(input.SecretRef) {
			return Descriptor{}, fmt.Errorf("connector secret_ref is invalid")
		}
	} else if !strings.HasPrefix(input.SecretRef, "secretmanager://") {
		return Descriptor{}, fmt.Errorf("connector secret_ref must use Secret Manager outside development")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Descriptor{}, fmt.Errorf("connector base_url is invalid")
	}
	if !isDevelopment(environment) && parsed.Scheme != "https" {
		return Descriptor{}, fmt.Errorf("connector base_url must use HTTPS outside development")
	}
	if isDevelopment(environment) && parsed.Scheme != "https" && parsed.Scheme != "http" {
		return Descriptor{}, fmt.Errorf("connector base_url scheme is invalid")
	}
	seen := make(map[string]struct{}, len(input.Operations))
	for index := range input.Operations {
		operation := &input.Operations[index]
		operation.Name = strings.ToLower(strings.TrimSpace(operation.Name))
		if !identifierPattern.MatchString(operation.Name) {
			return Descriptor{}, fmt.Errorf("connector operation is invalid")
		}
		capabilityID, parseErr := uuid.Parse(strings.TrimSpace(operation.CapabilityID))
		if parseErr != nil || capabilityID == uuid.Nil {
			return Descriptor{}, fmt.Errorf("connector operation capability_id is invalid")
		}
		operation.CapabilityID = capabilityID.String()
		if _, duplicate := seen[operation.Name]; duplicate {
			return Descriptor{}, fmt.Errorf("connector operations must be unique")
		}
		seen[operation.Name] = struct{}{}
		if objectType(operation.InputSchema) != "object" || objectType(operation.OutputSchema) != "object" {
			return Descriptor{}, fmt.Errorf("connector operation schemas must be objects")
		}
		if operation.MaxRequestBytes == 0 {
			operation.MaxRequestBytes = DefaultMaxRequestBytes
		}
		if operation.MaxResponseBytes == 0 {
			operation.MaxResponseBytes = DefaultMaxResponseBytes
		}
		if operation.MaxRequestBytes < 1 || operation.MaxRequestBytes > MaxRequestBytes ||
			operation.MaxResponseBytes < 1 || operation.MaxResponseBytes > MaxResponseBytes ||
			operation.TimeoutMS < 1 || operation.TimeoutMS > 300_000 {
			return Descriptor{}, fmt.Errorf("connector operation bounds are invalid")
		}
	}
	return input, nil
}

func (d Descriptor) Operation(name string) (OperationDescriptor, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, operation := range d.Operations {
		if operation.Name == name {
			return operation, true
		}
	}
	return OperationDescriptor{}, false
}

func objectType(schema map[string]any) string {
	value, _ := schema["type"].(string)
	return value
}

func isDevelopment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "development", "dev", "test", "local":
		return true
	default:
		return false
	}
}

type InvokeRequest struct {
	SchemaVersion  string         `json:"schema_version"`
	InvocationID   string         `json:"invocation_id"`
	OrgID          string         `json:"org_id"`
	ProductID      string         `json:"product_id"`
	CapabilityID   string         `json:"capability_id"`
	Operation      string         `json:"operation"`
	Arguments      map[string]any `json:"arguments"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type InvocationResult struct {
	SchemaVersion string         `json:"schema_version"`
	InvocationID  string         `json:"invocation_id"`
	ProductID     string         `json:"product_id"`
	CapabilityID  string         `json:"capability_id"`
	Operation     string         `json:"operation"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload,omitempty"`
	ErrorCode     string         `json:"error_code,omitempty"`
}

type Connector interface {
	Descriptor() Descriptor
	Invoke(context.Context, InvokeRequest) (InvocationResult, error)
	Status(context.Context, string) (InvocationResult, error)
}

type SecretResolver interface {
	Resolve(context.Context, string) ([]byte, error)
}

type SchemaValidator interface {
	Validate(map[string]any, map[string]any) error
}

type SchemaValidatorFunc func(map[string]any, map[string]any) error

func (f SchemaValidatorFunc) Validate(schema map[string]any, value map[string]any) error {
	return f(schema, value)
}

type Registry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
}

func NewRegistry() *Registry {
	return &Registry{connectors: make(map[string]Connector)}
}

func (r *Registry) Register(orgID string, connector Connector) error {
	if connector == nil {
		return fmt.Errorf("connector is required")
	}
	orgID = strings.TrimSpace(orgID)
	bindingID := strings.ToLower(strings.TrimSpace(connector.Descriptor().BindingID))
	if orgID == "" || !identifierPattern.MatchString(bindingID) {
		return fmt.Errorf("connector organization or binding id is invalid")
	}
	key := registryKey(orgID, bindingID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.connectors[key]; exists {
		return fmt.Errorf("connector binding is already registered")
	}
	r.connectors[key] = connector
	return nil
}

func (r *Registry) Resolve(orgID, bindingID string) (Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	connector, ok := r.connectors[registryKey(orgID, bindingID)]
	return connector, ok
}

func registryKey(orgID, bindingID string) string {
	return strings.TrimSpace(orgID) + "\x00" + strings.ToLower(strings.TrimSpace(bindingID))
}

func validResult(result InvocationResult, invocationID, productID, capabilityID, operation string) error {
	if result.SchemaVersion != SchemaVersion || result.InvocationID != invocationID ||
		result.ProductID != productID || result.CapabilityID != capabilityID ||
		result.Operation != operation {
		return fmt.Errorf("connector response binding is invalid")
	}
	switch result.Status {
	case "pending":
		if result.Payload != nil || result.ErrorCode != "" {
			return fmt.Errorf("pending connector response contains a terminal result")
		}
	case "succeeded":
		if result.Payload == nil || result.ErrorCode != "" {
			return fmt.Errorf("successful connector response is invalid")
		}
	case "failed":
		if !identifierPattern.MatchString(result.ErrorCode) || result.Payload != nil {
			return fmt.Errorf("failed connector response is invalid")
		}
	default:
		return fmt.Errorf("connector response status is invalid")
	}
	return nil
}

func marshalRequest(request InvokeRequest) ([]byte, error) {
	request.SchemaVersion = SchemaVersion
	return json.Marshal(request)
}
