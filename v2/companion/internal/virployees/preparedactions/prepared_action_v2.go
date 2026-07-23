package preparedactions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const V2SchemaVersion = "axis.prepared-action.v2"

const MaxArgumentsBytes = 1 << 20

var bindingIdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

// PreparedActionV2 is the provider-neutral action authorized by the execution
// gate. Dispatch is exclusively by ExecutorBindingID; capability keys and
// product names are descriptive compatibility aliases and never select code.
type PreparedActionV2 struct {
	SchemaVersion     string                    `json:"schema_version"`
	CapabilityID      string                    `json:"capability_id"`
	ManifestHash      string                    `json:"manifest_hash"`
	ExecutorBindingID string                    `json:"executor_binding_id"`
	Operation         string                    `json:"operation"`
	InputSchemaHash   string                    `json:"input_schema_hash"`
	OutputSchemaHash  string                    `json:"output_schema_hash"`
	Arguments         json.RawMessage           `json:"arguments"`
	RequiredAutonomy  string                    `json:"required_autonomy"`
	IdempotencyHash   string                    `json:"idempotency_hash,omitempty"`
	PrincipalType     string                    `json:"principal_type,omitempty"`
	PrincipalID       string                    `json:"principal_id,omitempty"`
	AssistContext     *AssistContextBinding     `json:"assist_context,omitempty"`
	ProfessionalScope *ProfessionalScopeBinding `json:"professional_scope,omitempty"`
	MCPContext        *MCPContextBinding        `json:"mcp_context,omitempty"`
}

type V2Input struct {
	CapabilityID      uuid.UUID
	ManifestHash      string
	ExecutorBindingID string
	Operation         string
	InputSchemaHash   string
	OutputSchemaHash  string
	Arguments         map[string]any
	RequiredAutonomy  string
	PrincipalType     string
	PrincipalID       string
}

func NewV2(input V2Input) (PreparedActionV2, error) {
	arguments, err := json.Marshal(input.Arguments)
	if err != nil {
		return PreparedActionV2{}, fmt.Errorf("encode prepared action arguments: %w", err)
	}
	action := PreparedActionV2{
		SchemaVersion: V2SchemaVersion, CapabilityID: input.CapabilityID.String(),
		ManifestHash:      strings.ToLower(strings.TrimSpace(input.ManifestHash)),
		ExecutorBindingID: strings.ToLower(strings.TrimSpace(input.ExecutorBindingID)),
		Operation:         strings.ToLower(strings.TrimSpace(input.Operation)),
		InputSchemaHash:   strings.ToLower(strings.TrimSpace(input.InputSchemaHash)),
		OutputSchemaHash:  strings.ToLower(strings.TrimSpace(input.OutputSchemaHash)),
		Arguments:         arguments, RequiredAutonomy: strings.TrimSpace(input.RequiredAutonomy),
		PrincipalType: strings.ToLower(strings.TrimSpace(input.PrincipalType)),
		PrincipalID:   strings.TrimSpace(input.PrincipalID),
	}
	if err := action.Validate(); err != nil {
		return PreparedActionV2{}, err
	}
	return action, nil
}

func (a PreparedActionV2) Validate() error {
	if a.SchemaVersion != V2SchemaVersion {
		return fmt.Errorf("prepared action schema version is invalid")
	}
	if id, err := uuid.Parse(strings.TrimSpace(a.CapabilityID)); err != nil || id == uuid.Nil {
		return fmt.Errorf("prepared action capability_id is invalid")
	}
	if !validHash(a.ManifestHash) || !validHash(a.InputSchemaHash) || !validHash(a.OutputSchemaHash) {
		return fmt.Errorf("prepared action contract hashes are invalid")
	}
	if !bindingIdentifierPattern.MatchString(a.ExecutorBindingID) || !bindingIdentifierPattern.MatchString(a.Operation) {
		return fmt.Errorf("prepared action executor binding is invalid")
	}
	if len(a.Arguments) == 0 || len(a.Arguments) > MaxArgumentsBytes {
		return fmt.Errorf("prepared action arguments are missing or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(a.Arguments))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil || arguments == nil {
		return fmt.Errorf("prepared action arguments must be a JSON object")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("prepared action arguments must contain one JSON object")
	}
	if strings.TrimSpace(a.RequiredAutonomy) == "" {
		return fmt.Errorf("prepared action required autonomy is missing")
	}
	if (strings.TrimSpace(a.PrincipalType) == "") != (strings.TrimSpace(a.PrincipalID) == "") {
		return fmt.Errorf("prepared action principal binding is incomplete")
	}
	return nil
}

func (a PreparedActionV2) PayloadHash() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (a PreparedActionV2) ArgumentsMap() (map[string]any, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	var arguments map[string]any
	if err := json.Unmarshal(a.Arguments, &arguments); err != nil {
		return nil, err
	}
	return arguments, nil
}

func validHash(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
