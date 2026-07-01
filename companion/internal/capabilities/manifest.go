package capabilities

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

const (
	SchemaVersion = "capability_manifest.v1"

	ActionTypeRead  = "read"
	ActionTypeWrite = "write"

	SideEffectRead    = "read"
	SideEffectWrite   = "write"
	SideEffectNotify  = "notify"
	SideEffectExecute = "execute"

	RiskLow      = "low"
	RiskMedium   = "medium"
	RiskHigh     = "high"
	RiskCritical = "critical"

	IdempotencyNone     = "none"
	IdempotencyOptional = "optional"
	IdempotencyRequired = "required"

	DefaultInvokeActionType     = "agent.capability.invoke"
	DefaultCompensateActionType = "agent.capability.compensate"
)

var (
	ErrInvalidManifest   = errors.New("invalid capability manifest")
	ErrDuplicateManifest = errors.New("duplicate capability manifest")

	semverPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

type RetryPolicy struct {
	MaxAttempts int    `json:"max_attempts"`
	Backoff     string `json:"backoff,omitempty"`
}

type Manifest struct {
	SchemaVersion        string         `json:"schema_version"`
	CapabilityID         string         `json:"capability_id"`
	Version              string         `json:"version"`
	DisplayName          string         `json:"display_name"`
	Description          string         `json:"description"`
	Owner                string         `json:"owner"`
	ProductSurface       string         `json:"product_surface"`
	Connector            string         `json:"connector"`
	ActionType           string         `json:"action_type"`
	RiskLevel            string         `json:"risk_level"`
	SideEffectType       string         `json:"side_effect_type"`
	AuthMode             string         `json:"auth_mode"`
	RequiredScopes       []string       `json:"required_scopes"`
	InputSchema          map[string]any `json:"input_schema"`
	OutputSchema         map[string]any `json:"output_schema"`
	EvidenceSchema       map[string]any `json:"evidence_schema"`
	RequiredEvidence     []string       `json:"required_evidence"`
	IdempotencyMode      string         `json:"idempotency_mode"`
	RollbackSupported    bool           `json:"rollback_supported"`
	RollbackCapabilityID string         `json:"rollback_capability_id,omitempty"`
	CompensationStrategy string         `json:"compensation_strategy"`
	NexusActionType      string         `json:"nexus_action_type,omitempty"`
	ApprovalRequired     bool           `json:"approval_required"`
	TenantConfigurable   bool           `json:"tenant_configurable"`
	EnabledByDefault     bool           `json:"enabled_by_default"`
	RateLimitClass       string         `json:"rate_limit_class"`
	CostClass            string         `json:"cost_class"`
	Timeout              string         `json:"timeout"`
	Retries              RetryPolicy    `json:"retries"`
	Postconditions       []string       `json:"postconditions"`
	Preconditions        []string       `json:"preconditions"`
	ObservabilityTags    []string       `json:"observability_tags"`
}

type Capability struct {
	CapabilityID   string `json:"capability_id"`
	CapabilityKey  string `json:"capability_key"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	ProductID      string `json:"product_id,omitempty"`
	ProductSurface string `json:"product_surface,omitempty"`
	ToolID         string `json:"tool_id,omitempty"`
	Mode           string `json:"mode"`
	RiskClass      string `json:"risk_class"`
	Status         string `json:"status"`
}

type Tool struct {
	ToolID        string `json:"tool_id"`
	ToolKey       string `json:"tool_key"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ConnectorID   string `json:"connector_id,omitempty"`
	ConnectorKey  string `json:"connector_key,omitempty"`
	Operation     string `json:"operation"`
	SideEffect    bool   `json:"side_effect"`
	Status        string `json:"status"`
	CapabilityID  string `json:"capability_id,omitempty"`
	CapabilityKey string `json:"capability_key,omitempty"`
}

type manifestSet struct {
	Capabilities []Manifest `json:"capabilities"`
}

type Registry struct {
	manifests   []Manifest
	byIDVersion map[string]Manifest
	byOperation map[string]Manifest
}

func NewRegistry(manifests []Manifest) (*Registry, error) {
	reg := &Registry{
		manifests:   make([]Manifest, 0, len(manifests)),
		byIDVersion: make(map[string]Manifest, len(manifests)),
		byOperation: make(map[string]Manifest, len(manifests)),
	}
	byOperationVersion := make(map[string]Manifest, len(manifests))
	for _, manifest := range manifests {
		normalized := manifest.Normalize()
		if err := normalized.Validate(); err != nil {
			return nil, err
		}
		key := normalized.Key()
		if _, exists := reg.byIDVersion[key]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateManifest, key)
		}
		opKey := operationKey(normalized.Connector, normalized.CapabilityID)
		opVersionKey := opKey + "@" + normalized.Version
		if existing, exists := byOperationVersion[opVersionKey]; exists {
			return nil, fmt.Errorf("%w: operation %s already owned by %s@%s", ErrDuplicateManifest, opVersionKey, existing.CapabilityID, existing.Version)
		}
		byOperationVersion[opVersionKey] = normalized
		if existing, exists := reg.byOperation[opKey]; !exists || compareSemver(normalized.Version, existing.Version) > 0 {
			reg.byOperation[opKey] = normalized
		}
		reg.byIDVersion[key] = normalized
		reg.manifests = append(reg.manifests, normalized)
	}
	sort.Slice(reg.manifests, func(i, j int) bool {
		if reg.manifests[i].Connector == reg.manifests[j].Connector {
			if reg.manifests[i].CapabilityID == reg.manifests[j].CapabilityID {
				return reg.manifests[i].Version < reg.manifests[j].Version
			}
			return reg.manifests[i].CapabilityID < reg.manifests[j].CapabilityID
		}
		return reg.manifests[i].Connector < reg.manifests[j].Connector
	})
	return reg, nil
}

func LoadFS(fsys fs.FS, root string) (*Registry, error) {
	root = strings.Trim(strings.TrimSpace(root), "/")
	if root == "" {
		root = "."
	}
	var manifests []Manifest
	if err := fs.WalkDir(fsys, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		decoded, err := decodeManifestFile(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		manifests = append(manifests, decoded...)
		return nil
	}); err != nil {
		return nil, err
	}
	return NewRegistry(manifests)
}

func (r *Registry) All() []Manifest {
	if r == nil {
		return nil
	}
	out := make([]Manifest, len(r.manifests))
	copy(out, r.manifests)
	return out
}

func (r *Registry) Lookup(capabilityID, version string) (Manifest, bool) {
	if r == nil {
		return Manifest{}, false
	}
	manifest, ok := r.byIDVersion[keyFor(capabilityID, version)]
	return manifest, ok
}

func (r *Registry) LookupOperation(connector, operation string) (Manifest, bool) {
	if r == nil {
		return Manifest{}, false
	}
	manifest, ok := r.byOperation[operationKey(connector, operation)]
	return manifest, ok
}

func (m Manifest) Key() string {
	return keyFor(m.CapabilityID, m.Version)
}

func (m Manifest) Normalize() Manifest {
	m.SchemaVersion = firstNonEmpty(m.SchemaVersion, SchemaVersion)
	m.CapabilityID = strings.TrimSpace(m.CapabilityID)
	m.Version = firstNonEmpty(m.Version, "1.0.0")
	m.DisplayName = firstNonEmpty(m.DisplayName, strings.ReplaceAll(m.CapabilityID, ".", " "))
	m.Description = strings.TrimSpace(m.Description)
	m.Owner = strings.TrimSpace(m.Owner)
	m.ProductSurface = strings.TrimSpace(m.ProductSurface)
	m.Connector = strings.TrimSpace(m.Connector)
	m.ActionType = strings.ToLower(firstNonEmpty(m.ActionType, ActionTypeRead))
	m.RiskLevel = strings.ToLower(firstNonEmpty(m.RiskLevel, RiskLow))
	m.SideEffectType = strings.ToLower(firstNonEmpty(m.SideEffectType, SideEffectRead))
	m.AuthMode = strings.TrimSpace(m.AuthMode)
	m.RequiredScopes = cleanList(m.RequiredScopes)
	m.RequiredEvidence = cleanList(m.RequiredEvidence)
	m.IdempotencyMode = strings.ToLower(firstNonEmpty(m.IdempotencyMode, IdempotencyNone))
	m.CompensationStrategy = strings.ToLower(firstNonEmpty(m.CompensationStrategy, "none"))
	m.NexusActionType = strings.TrimSpace(m.NexusActionType)
	if m.ApprovalRequired && m.NexusActionType == "" {
		m.NexusActionType = DefaultInvokeActionType
	}
	m.RateLimitClass = strings.ToLower(firstNonEmpty(m.RateLimitClass, "standard"))
	m.CostClass = strings.ToLower(firstNonEmpty(m.CostClass, "low"))
	m.Timeout = firstNonEmpty(m.Timeout, "30s")
	if m.Retries.MaxAttempts == 0 {
		m.Retries.MaxAttempts = 1
	}
	if m.Retries.Backoff == "" {
		m.Retries.Backoff = "none"
	}
	m.Postconditions = cleanList(m.Postconditions)
	m.Preconditions = cleanList(m.Preconditions)
	if len(m.Preconditions) == 0 {
		m.Preconditions = []string{"customer_org_context"}
	}
	m.ObservabilityTags = cleanList(m.ObservabilityTags)
	if len(m.ObservabilityTags) == 0 {
		m.ObservabilityTags = []string{"connector:" + m.Connector, "risk:" + m.RiskLevel, "action:" + m.ActionType}
	}
	if m.InputSchema == nil {
		m.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if m.OutputSchema == nil {
		m.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if m.EvidenceSchema == nil {
		m.EvidenceSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return m
}

func (m Manifest) Validate() error {
	m = m.Normalize()
	required := map[string]string{
		"capability_id":    m.CapabilityID,
		"version":          m.Version,
		"display_name":     m.DisplayName,
		"description":      m.Description,
		"owner":            m.Owner,
		"product_surface":  m.ProductSurface,
		"connector":        m.Connector,
		"action_type":      m.ActionType,
		"risk_level":       m.RiskLevel,
		"side_effect_type": m.SideEffectType,
		"auth_mode":        m.AuthMode,
		"idempotency_mode": m.IdempotencyMode,
		"rate_limit_class": m.RateLimitClass,
		"cost_class":       m.CostClass,
		"timeout":          m.Timeout,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidManifest, field)
		}
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version %q", ErrInvalidManifest, m.SchemaVersion)
	}
	if !semverPattern.MatchString(m.Version) {
		return fmt.Errorf("%w: version must be semver", ErrInvalidManifest)
	}
	if !oneOf(m.ActionType, ActionTypeRead, ActionTypeWrite) {
		return fmt.Errorf("%w: invalid action_type %q", ErrInvalidManifest, m.ActionType)
	}
	if !oneOf(m.RiskLevel, RiskLow, RiskMedium, RiskHigh, RiskCritical) {
		return fmt.Errorf("%w: invalid risk_level %q", ErrInvalidManifest, m.RiskLevel)
	}
	if !oneOf(m.SideEffectType, SideEffectRead, SideEffectWrite, SideEffectNotify, SideEffectExecute) {
		return fmt.Errorf("%w: invalid side_effect_type %q", ErrInvalidManifest, m.SideEffectType)
	}
	if !oneOf(m.IdempotencyMode, IdempotencyNone, IdempotencyOptional, IdempotencyRequired) {
		return fmt.Errorf("%w: invalid idempotency_mode %q", ErrInvalidManifest, m.IdempotencyMode)
	}
	if _, err := time.ParseDuration(m.Timeout); err != nil {
		return fmt.Errorf("%w: invalid timeout %q", ErrInvalidManifest, m.Timeout)
	}
	if m.Retries.MaxAttempts < 1 {
		return fmt.Errorf("%w: retries.max_attempts must be >= 1", ErrInvalidManifest)
	}
	if m.ApprovalRequired && m.NexusActionType == "" {
		return fmt.Errorf("%w: nexus_action_type is required when approval_required=true", ErrInvalidManifest)
	}
	if m.ActionType == ActionTypeWrite && !m.ApprovalRequired {
		return fmt.Errorf("%w: write capabilities require approval", ErrInvalidManifest)
	}
	if m.SideEffectType != SideEffectRead && !m.ApprovalRequired {
		return fmt.Errorf("%w: side-effect capabilities require approval", ErrInvalidManifest)
	}
	if m.RollbackSupported && strings.TrimSpace(m.RollbackCapabilityID) == "" && m.CompensationStrategy != "manual" {
		return fmt.Errorf("%w: rollback_capability_id is required for automatic rollback", ErrInvalidManifest)
	}
	if err := validateObjectSchema("input_schema", m.InputSchema); err != nil {
		return err
	}
	if err := validateObjectSchema("output_schema", m.OutputSchema); err != nil {
		return err
	}
	if err := validateObjectSchema("evidence_schema", m.EvidenceSchema); err != nil {
		return err
	}
	if err := validateRequiredEvidence(m.RequiredEvidence, m.EvidenceSchema); err != nil {
		return err
	}
	return nil
}

func (m Manifest) ToConnectorCapability() connectordomain.Capability {
	sideEffect := m.SideEffectType != SideEffectRead || m.ActionType == ActionTypeWrite
	return connectordomain.Capability{
		ID:                    m.CapabilityID,
		Version:               m.Version,
		Status:                connectordomain.CapabilityStatusActive,
		DisplayName:           m.DisplayName,
		Description:           m.Description,
		Owner:                 m.Owner,
		OwnerDomain:           m.Owner,
		PublishedFrom:         connectordomain.CapabilityPublishedFromProduct,
		Product:               m.ProductSurface,
		ProductSurface:        m.ProductSurface,
		Connector:             m.Connector,
		Operation:             m.CapabilityID,
		ActionType:            m.ActionType,
		Mode:                  m.ActionType,
		SideEffectType:        m.SideEffectType,
		SideEffectClass:       m.SideEffectType,
		SideEffect:            sideEffect,
		ReadOnly:              !sideEffect,
		RiskClass:             m.RiskLevel,
		TenantScope:           connectordomain.TenantScope{Mode: connectordomain.TenantScopeSingleTenant, Resolver: connectordomain.TenantScopeResolverUser},
		AuthMode:              connectordomain.AuthMode{Type: m.AuthMode},
		RequiredScopes:        append([]string(nil), m.RequiredScopes...),
		RequiresNexusApproval: m.ApprovalRequired,
		ApprovalPolicy:        connectordomain.ApprovalPolicy{Required: m.ApprovalRequired},
		InputSchema:           cloneMap(m.InputSchema),
		OutputSchema:          cloneMap(m.OutputSchema),
		EvidenceSchema:        cloneMap(m.EvidenceSchema),
		EvidenceFields:        append([]string(nil), m.RequiredEvidence...),
		EvidenceRequired:      append([]string(nil), m.RequiredEvidence...),
		Idempotency:           connectordomain.IdempotencyContract{Required: m.IdempotencyMode == IdempotencyRequired},
		IdempotencyMode:       m.IdempotencyMode,
		Rollback:              connectordomain.RollbackContract{Supported: m.RollbackSupported, CapabilityID: m.RollbackCapabilityID},
		CompensationStrategy:  m.CompensationStrategy,
		NexusActionType:       m.NexusActionType,
		TenantConfigurable:    m.TenantConfigurable,
		EnabledByDefault:      m.EnabledByDefault,
		RateLimitClass:        m.RateLimitClass,
		CostClass:             m.CostClass,
		Timeout:               m.Timeout,
		Retries:               connectordomain.RetryPolicy{MaxAttempts: m.Retries.MaxAttempts, Backoff: m.Retries.Backoff},
		Postconditions:        append([]string(nil), m.Postconditions...),
		Preconditions:         append([]string(nil), m.Preconditions...),
		ObservabilityTags:     append([]string(nil), m.ObservabilityTags...),
	}
}

func FromConnectorCapability(connector, kind string, capability connectordomain.Capability) (Manifest, error) {
	c := capability.Normalized(connector, kind)
	inputSchema := repairHistoricalSchema(c.InputSchema)
	outputSchema := repairHistoricalSchema(c.OutputSchema)
	evidenceSchema := repairHistoricalEvidenceSchema(c.EvidenceSchema, c.EvidenceRequired)
	actionType := firstNonEmpty(c.ActionType, c.Mode)
	sideEffectType := firstNonEmpty(c.SideEffectType, c.SideEffectClass)
	manifest := Manifest{
		SchemaVersion:        SchemaVersion,
		CapabilityID:         firstNonEmpty(c.ID, c.Operation),
		Version:              firstNonEmpty(c.Version, "1.0.0"),
		DisplayName:          firstNonEmpty(c.DisplayName, strings.ReplaceAll(firstNonEmpty(c.ID, c.Operation), ".", " ")),
		Description:          firstNonEmpty(c.Description, "Capability "+firstNonEmpty(c.ID, c.Operation)+" on "+kind+" connector."),
		Owner:                firstNonEmpty(c.Owner, c.OwnerDomain, kind),
		ProductSurface:       firstNonEmpty(c.ProductSurface, c.Product, kind, "companion"),
		Connector:            firstNonEmpty(c.Connector, kind, connector),
		ActionType:           actionType,
		RiskLevel:            firstNonEmpty(c.RiskClass, RiskLow),
		SideEffectType:       sideEffectType,
		AuthMode:             firstNonEmpty(c.AuthMode.Type, connectordomain.AuthModeHybrid),
		RequiredScopes:       append([]string(nil), c.RequiredScopes...),
		InputSchema:          inputSchema,
		OutputSchema:         outputSchema,
		EvidenceSchema:       evidenceSchema,
		RequiredEvidence:     append([]string(nil), c.EvidenceRequired...),
		IdempotencyMode:      firstNonEmpty(c.IdempotencyMode, idempotencyMode(c)),
		RollbackSupported:    c.Rollback.Supported,
		RollbackCapabilityID: c.Rollback.CapabilityID,
		CompensationStrategy: firstNonEmpty(c.CompensationStrategy, compensationStrategy(c)),
		NexusActionType:      c.NexusActionType,
		ApprovalRequired:     c.NeedsNexusApproval(),
		TenantConfigurable:   true,
		EnabledByDefault:     true,
		RateLimitClass:       firstNonEmpty(c.RateLimitClass, "standard"),
		CostClass:            firstNonEmpty(c.CostClass, "low"),
		Timeout:              firstNonEmpty(c.Timeout, "30s"),
		Retries:              RetryPolicy{MaxAttempts: c.Retries.MaxAttempts, Backoff: c.Retries.Backoff},
		Postconditions:       append([]string(nil), c.Postconditions...),
		Preconditions:        append([]string(nil), c.Preconditions...),
		ObservabilityTags:    append([]string(nil), c.ObservabilityTags...),
	}
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func decodeManifestFile(raw []byte) ([]Manifest, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: empty file", ErrInvalidManifest)
	}
	var set manifestSet
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&set); err == nil && len(set.Capabilities) > 0 {
		return set.Capabilities, nil
	}
	var single Manifest
	dec = json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&single); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	return []Manifest{single}, nil
}

func validateObjectSchema(label string, schema map[string]any) error {
	if len(schema) == 0 {
		return fmt.Errorf("%w: %s is required", ErrInvalidManifest, label)
	}
	typ, ok := schema["type"].(string)
	if !ok || typ != "object" {
		return fmt.Errorf("%w: %s.type must be object", ErrInvalidManifest, label)
	}
	props, _ := schema["properties"].(map[string]any)
	required, ok := requiredKeys(schema["required"])
	if !ok {
		if _, exists := schema["required"]; exists {
			return fmt.Errorf("%w: %s.required must be an array of strings", ErrInvalidManifest, label)
		}
		return nil
	}
	for _, key := range required {
		if _, exists := props[key]; !exists {
			return fmt.Errorf("%w: %s.required references missing property %q", ErrInvalidManifest, label, key)
		}
	}
	return nil
}

func validateRequiredEvidence(required []string, schema map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	for _, key := range required {
		if _, ok := props[key]; !ok {
			return fmt.Errorf("%w: required_evidence references missing evidence_schema property %q", ErrInvalidManifest, key)
		}
	}
	return nil
}

func repairHistoricalSchema(schema map[string]any) map[string]any {
	out := cloneMap(schema)
	if len(out) == 0 {
		out = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	props, _ := out["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	if required, ok := requiredKeys(out["required"]); ok {
		for _, key := range required {
			if _, exists := props[key]; !exists {
				props[key] = map[string]any{"type": "string"}
			}
		}
	}
	out["properties"] = props
	return out
}

func repairHistoricalEvidenceSchema(schema map[string]any, evidence []string) map[string]any {
	out := repairHistoricalSchema(schema)
	props, _ := out["properties"].(map[string]any)
	for _, key := range evidence {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := props[key]; !ok {
			props[key] = map[string]any{"type": "string"}
		}
	}
	out["properties"] = props
	return out
}

func requiredKeys(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case nil:
		return nil, false
	case []string:
		return cleanList(values), true
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return cleanList(out), true
	default:
		return nil, false
	}
}

func idempotencyMode(capability connectordomain.Capability) string {
	if capability.Idempotency.Required || capability.HasSideEffect() {
		return IdempotencyRequired
	}
	return IdempotencyNone
}

func compensationStrategy(capability connectordomain.Capability) string {
	if capability.Rollback.Supported {
		return "capability"
	}
	return "none"
}

func keyFor(capabilityID, version string) string {
	return strings.TrimSpace(capabilityID) + "@" + strings.TrimSpace(version)
}

func operationKey(connector, operation string) string {
	return strings.TrimSpace(connector) + ":" + strings.TrimSpace(operation)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func compareSemver(a, b string) int {
	ap := strings.SplitN(a, "-", 2)[0]
	bp := strings.SplitN(b, "-", 2)[0]
	aa := strings.Split(ap, ".")
	bb := strings.Split(bp, ".")
	for i := 0; i < 3; i++ {
		av := 0
		bv := 0
		if i < len(aa) {
			_, _ = fmt.Sscanf(aa[i], "%d", &av)
		}
		if i < len(bb) {
			_, _ = fmt.Sscanf(bb[i], "%d", &bv)
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return strings.Compare(a, b)
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}
