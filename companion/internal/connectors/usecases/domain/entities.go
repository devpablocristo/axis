package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Connector configuración de un conector a sistema externo.
type Connector struct {
	ID         uuid.UUID
	OrgID      string
	Name       string
	Kind       string // pymes, whatsapp, slack, email, calendar, mock
	Enabled    bool
	ConfigJSON json.RawMessage // credenciales/config (sin secretos en claro)
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Capability capacidad que ofrece un conector.
type Capability struct {
	ID                 string                `json:"id,omitempty"`
	Version            string                `json:"version,omitempty"`
	Status             string                `json:"status,omitempty"`
	OwnerDomain        string                `json:"owner_domain,omitempty"`
	PublishedFrom      string                `json:"published_from,omitempty"`
	Product            string                `json:"product,omitempty"`
	Operation          string                `json:"operation"` // send_whatsapp, create_purchase, etc.
	Mode               string                `json:"mode"`      // read o write
	SideEffectClass    string                `json:"side_effect_class,omitempty"`
	SideEffect         bool                  `json:"side_effect"`
	ReadOnly           bool                  `json:"read_only"`
	RiskClass          string                `json:"risk_class,omitempty"`
	TenantScope        TenantScope           `json:"tenant_scope,omitempty"`
	AuthMode           AuthMode              `json:"auth_mode,omitempty"`
	RequiredRoles      []string              `json:"required_roles,omitempty"`
	RequiredScopes     []string              `json:"required_scopes,omitempty"`
	RequiredModules    []string              `json:"required_modules,omitempty"`
	RequiresGovernance bool                  `json:"requires_governance"`
	ApprovalPolicy     ApprovalPolicy        `json:"approval_policy,omitempty"`
	InputSchema        map[string]any        `json:"input_schema,omitempty"`
	OutputSchema       map[string]any        `json:"output_schema,omitempty"`
	EvidenceFields     []string              `json:"evidence_fields,omitempty"` // alias de compatibilidad
	EvidenceRequired   []string              `json:"evidence_required,omitempty"`
	DataClassification DataClassification    `json:"data_classification,omitempty"`
	Idempotency        IdempotencyContract   `json:"idempotency,omitempty"`
	Observability      ObservabilityContract `json:"observability,omitempty"`
	ErrorContract      ErrorContract         `json:"error_contract,omitempty"`
	Rollback           RollbackContract      `json:"rollback,omitempty"`
}

type TenantScope struct {
	Mode     string `json:"mode,omitempty"`
	Resolver string `json:"resolver,omitempty"`
}

type AuthMode struct {
	Type string `json:"type,omitempty"`
}

type ApprovalPolicy struct {
	Required bool   `json:"required"`
	PolicyID string `json:"policy_id,omitempty"`
}

type DataClassification struct {
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}

type IdempotencyContract struct {
	Required  bool     `json:"required"`
	KeyFields []string `json:"key_fields,omitempty"`
}

type ObservabilityContract struct {
	EmitTrace      bool     `json:"emit_trace"`
	EmitAuditEvent bool     `json:"emit_audit_event"`
	Metrics        []string `json:"metrics,omitempty"`
}

type ErrorContract struct {
	TypedErrors []string `json:"typed_errors,omitempty"`
}

type RollbackContract struct {
	Supported    bool   `json:"supported"`
	CapabilityID string `json:"capability_id,omitempty"`
}

type CapabilityDecision struct {
	CapabilityID        string   `json:"capability_id"`
	Operation           string   `json:"operation"`
	SideEffectClass     string   `json:"side_effect_class"`
	RiskClass           string   `json:"risk_class"`
	RequiresGovernance  bool     `json:"requires_governance"`
	RequiredScopes      []string `json:"required_scopes,omitempty"`
	IdempotencyRequired bool     `json:"idempotency_required"`
}

type CapabilityFilter struct {
	TenantID           string
	Roles              []string
	Scopes             []string
	Modules            []string
	MaxRiskClass       string
	IncludeWrites      bool
	EnforcePermissions bool
}

// ExecutionSpec especificación de una ejecución en un conector.
type ExecutionSpec struct {
	ConnectorID         uuid.UUID
	OrgID               string
	ActorID             string
	ProductSurface      string
	AuthScopes          []string
	RunID               string
	ToolInvocationID    string
	Operation           string
	Payload             json.RawMessage
	IdempotencyKey      string
	TaskID              *uuid.UUID
	GovernanceRequestID *uuid.UUID
}

// ExecutionResult resultado de una ejecución.
type ExecutionResult struct {
	ID                  uuid.UUID
	ConnectorID         uuid.UUID
	OrgID               string
	ActorID             string
	Operation           string
	Status              string // success, failure, partial
	ExternalRef         string // referencia en el sistema externo
	Payload             json.RawMessage
	ResultJSON          json.RawMessage
	EvidenceJSON        json.RawMessage
	ErrorMessage        string
	Retryable           bool
	DurationMS          int64
	IdempotencyKey      string
	TaskID              *uuid.UUID
	GovernanceRequestID *uuid.UUID
	CreatedAt           time.Time
}

// ExecutionStatus valores de estado de ejecución.
const (
	ExecSuccess = "success"
	ExecFailure = "failure"
	ExecPartial = "partial"
)

const (
	CapabilityModeRead  = "read"
	CapabilityModeWrite = "write"

	CapabilityStatusActive = "active"

	CapabilityPublishedFromProduct = "product"

	TenantScopeSingleTenant = "single_tenant_required"
	TenantScopeResolverUser = "user"

	AuthModeHybrid = "hybrid"

	SideEffectClassRead    = "read"
	SideEffectClassWrite   = "write"
	SideEffectClassNotify  = "notify"
	SideEffectClassExecute = "execute"

	RiskClassLow      = "low"
	RiskClassMedium   = "medium"
	RiskClassHigh     = "high"
	RiskClassCritical = "critical"
)

// HasSideEffect mantiene compatibilidad entre variantes del contrato de capability.
func (c Capability) HasSideEffect() bool {
	mode := strings.TrimSpace(strings.ToLower(c.Mode))
	sideEffectClass := strings.TrimSpace(strings.ToLower(c.SideEffectClass))
	return c.SideEffect || mode == CapabilityModeWrite || sideEffectClass == SideEffectClassWrite || sideEffectClass == SideEffectClassNotify || sideEffectClass == SideEffectClassExecute || !c.ReadOnly && mode != CapabilityModeRead
}

// NeedsGovernance indica si Nexus debe aprobar/permitir antes de ejecutar.
func (c Capability) NeedsGovernance() bool {
	return c.RequiresGovernance || c.ApprovalPolicy.Required || c.HasSideEffect()
}

// Normalized completa defaults del contrato v1 sin perder compatibilidad con
// capabilities declaradas por conectores existentes.
func (c Capability) Normalized(connectorID, kind string) Capability {
	kind = strings.TrimSpace(kind)
	if c.ID == "" {
		c.ID = strings.TrimSpace(c.Operation)
		if c.ID == "" {
			c.ID = strings.Trim(strings.TrimSpace(kind)+".unknown", ".")
		}
	}
	if c.Version == "" {
		c.Version = "1.0.0"
	}
	if c.Status == "" {
		c.Status = CapabilityStatusActive
	}
	if c.OwnerDomain == "" {
		c.OwnerDomain = kind
	}
	if c.PublishedFrom == "" {
		c.PublishedFrom = CapabilityPublishedFromProduct
	}
	if c.Product == "" {
		c.Product = kind
	}
	if c.Mode == "" {
		if c.ReadOnly && !c.SideEffect {
			c.Mode = CapabilityModeRead
		} else {
			c.Mode = CapabilityModeWrite
		}
	}
	if c.SideEffectClass == "" {
		if strings.EqualFold(c.Mode, CapabilityModeRead) {
			c.SideEffectClass = SideEffectClassRead
		} else {
			c.SideEffectClass = SideEffectClassWrite
		}
	}
	if c.RiskClass == "" {
		c.RiskClass = RiskClassLow
	}
	if c.TenantScope.Mode == "" {
		c.TenantScope.Mode = TenantScopeSingleTenant
	}
	if c.TenantScope.Resolver == "" {
		c.TenantScope.Resolver = TenantScopeResolverUser
	}
	if c.AuthMode.Type == "" {
		c.AuthMode.Type = AuthModeHybrid
	}
	if c.InputSchema == nil {
		c.InputSchema = map[string]any{"type": "object"}
	}
	if c.OutputSchema == nil {
		c.OutputSchema = map[string]any{"type": "object"}
	}
	if len(c.EvidenceRequired) == 0 && len(c.EvidenceFields) > 0 {
		c.EvidenceRequired = append([]string(nil), c.EvidenceFields...)
	}
	if len(c.EvidenceFields) == 0 && len(c.EvidenceRequired) > 0 {
		c.EvidenceFields = append([]string(nil), c.EvidenceRequired...)
	}
	if c.NeedsGovernance() {
		c.ApprovalPolicy.Required = true
	}
	if c.HasSideEffect() {
		if len(c.Idempotency.KeyFields) == 0 {
			c.Idempotency.KeyFields = []string{"tenant_id", "task_id", "operation", "idempotency_key"}
		}
		c.Idempotency.Required = true
	}
	if len(c.Observability.Metrics) == 0 {
		c.Observability.Metrics = []string{"latency_ms", "success_rate", "error_rate"}
	}
	c.Observability.EmitTrace = true
	c.Observability.EmitAuditEvent = true
	if len(c.ErrorContract.TypedErrors) == 0 {
		c.ErrorContract.TypedErrors = []string{"unauthorized", "out_of_scope", "precondition_failed", "conflict"}
	}
	_ = connectorID
	return c
}

func (c Capability) RuntimeDecision() CapabilityDecision {
	return CapabilityDecision{
		CapabilityID:        c.ID,
		Operation:           c.Operation,
		SideEffectClass:     c.SideEffectClass,
		RiskClass:           c.RiskClass,
		RequiresGovernance:  c.NeedsGovernance(),
		RequiredScopes:      append([]string(nil), c.RequiredScopes...),
		IdempotencyRequired: c.Idempotency.Required,
	}
}

func (c Capability) MatchesFilter(filter CapabilityFilter) bool {
	c = c.Normalized("", c.Product)
	if !filter.IncludeWrites && c.HasSideEffect() {
		return false
	}
	if filter.MaxRiskClass != "" && riskRank(c.RiskClass) > riskRank(filter.MaxRiskClass) {
		return false
	}
	if filter.EnforcePermissions {
		if !hasAll(filter.Roles, c.RequiredRoles) {
			return false
		}
		if !hasAll(filter.Scopes, c.RequiredScopes) {
			return false
		}
		if !hasAll(filter.Modules, c.RequiredModules) {
			return false
		}
	}
	return true
}

func hasAll(have, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, value := range have {
		if normalized := strings.TrimSpace(value); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	for _, value := range required {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, ok := set[normalized]; !ok {
			return false
		}
	}
	return true
}

func riskRank(risk string) int {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case RiskClassLow, "":
		return 1
	case RiskClassMedium:
		return 2
	case RiskClassHigh:
		return 3
	case RiskClassCritical:
		return 4
	default:
		return 4
	}
}
