package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

// Repository port de persistencia para conectores y resultados de ejecución.
type Repository interface {
	SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error)
	GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	ListConnectors(ctx context.Context) ([]domain.Connector, error)
	UpdateConnector(ctx context.Context, c domain.Connector) (domain.Connector, error)
	DeleteConnector(ctx context.Context, id uuid.UUID) error

	SaveExecution(ctx context.Context, r domain.ExecutionResult) error
	AcquireExecutionLock(ctx context.Context, lockKey string) (bool, error)
	ReleaseExecutionLock(ctx context.Context, lockKey string) error
	GetExecutionByIdempotency(ctx context.Context, taskID uuid.UUID, operation string, nexusRequestID *uuid.UUID, idempotencyKey string) (domain.ExecutionResult, error)
	ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error)
}

// NexusExecutionIntent es la acción exacta que Nexus debe haber aprobado.
type NexusExecutionIntent struct {
	NexusRequestID   uuid.UUID
	OrgID            string
	ActorID          string
	ActorType        string
	ProductSurface   string
	TaskID           *uuid.UUID
	RunID            string
	ToolInvocationID string
	ConnectorID      uuid.UUID
	CapabilityID     string
	Operation        string
	TargetSystem     string
	TargetResource   string
	PayloadHash      string
	IdempotencyKey   string
	RiskHint         string
	ActionBinding    map[string]any
	BindingHash      string
}

const toolIntentSchemaVersion = "tool_intent.v1"

// NexusRequestMeta es el subconjunto de Nexus usado para validar grants.
type NexusRequestMeta struct {
	Status        string
	OrgID         string
	BindingHash   string
	ActionBinding map[string]any
}

// NexusChecker verifica que una ejecución tiene aprobación de Nexus y pertenece al tenant esperado.
type NexusChecker interface {
	AuthorizeExecution(ctx context.Context, intent NexusExecutionIntent) (bool, error)
}

type ProductInstallationGuard interface {
	RequireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error
}

type ProductRuntimeController interface {
	EnforceProductConnectorExecution(ctx context.Context, orgID, productSurface string) error
}

// Usecases lógica de negocio de conectores.
type Usecases struct {
	repo                     Repository
	registry                 *registry.Registry
	checker                  NexusChecker
	installationGuard        ProductInstallationGuard
	productRuntimeController ProductRuntimeController
	rateLimiter              productlimits.Limiter
	dynamicRegistrar         func(context.Context)
}

// NewUsecases crea una nueva instancia de Usecases.
func NewUsecases(repo Repository, reg *registry.Registry, checker NexusChecker) *Usecases {
	return &Usecases{
		repo:     repo,
		registry: reg,
		checker:  checker,
	}
}

func (uc *Usecases) SetProductInstallationGuard(guard ProductInstallationGuard) {
	uc.installationGuard = guard
}

func (uc *Usecases) SetProductRuntimeController(controller ProductRuntimeController) {
	uc.productRuntimeController = controller
}

func (uc *Usecases) SetRateLimiter(limiter productlimits.Limiter) {
	uc.rateLimiter = limiter
}

func (uc *Usecases) SetDynamicConnectorRegistrar(registrar func(context.Context)) {
	uc.dynamicRegistrar = registrar
}

// ListConnectors lista conectores registrados con su estado en DB.
func (uc *Usecases) ListConnectors(ctx context.Context) ([]domain.Connector, error) {
	conns, err := uc.repo.ListConnectors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	return conns, nil
}

// GetConnector obtiene un conector por ID.
func (uc *Usecases) GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error) {
	conn, err := uc.repo.GetConnector(ctx, id)
	if err != nil {
		return domain.Connector{}, fmt.Errorf("get connector: %w", err)
	}
	return conn, nil
}

// SaveConnector crea o actualiza un conector.
func (uc *Usecases) SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	if c.ID == uuid.Nil {
		return uc.repo.SaveConnector(ctx, c)
	}
	return uc.repo.UpdateConnector(ctx, c)
}

// DeleteConnector elimina un conector.
func (uc *Usecases) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	return uc.repo.DeleteConnector(ctx, id)
}

// Execute ejecuta una operación en un conector con gating obligatorio.
func (uc *Usecases) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	config, err := uc.repo.GetConnector(ctx, spec.ConnectorID)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("get connector config: %w", err)
	}
	if !config.Enabled {
		return domain.ExecutionResult{}, ErrDisabled
	}
	if err := ensureConnectorOrg(config.OrgID, spec.OrgID); err != nil {
		return domain.ExecutionResult{}, err
	}

	// Obtener implementación del conector a partir del kind persistido.
	conn, ok := uc.registry.Get(config.Kind)
	if !ok {
		return domain.ExecutionResult{}, ErrNotFound
	}

	var capability domain.Capability
	operationKnown := false
	for _, cap := range conn.Capabilities() {
		if cap.Operation != spec.Operation {
			continue
		}
		operationKnown = true
		capability, err = connectorCapability(conn, cap)
		if err != nil {
			return domain.ExecutionResult{}, err
		}
		break
	}
	if !operationKnown {
		return domain.ExecutionResult{}, ErrOperationUnknown
	}
	if err := validateExecutionContext(spec, capability); err != nil {
		return domain.ExecutionResult{}, err
	}
	productSurface := productSurfaceFor(capability, spec)
	if err := uc.requireActiveInstallation(ctx, spec.OrgID, productSurface, "connector_execution"); err != nil {
		return domain.ExecutionResult{}, err
	}
	if err := uc.enforceProductRuntime(ctx, spec.OrgID, productSurface); err != nil {
		return domain.ExecutionResult{}, err
	}
	if err := productlimits.Enforce(ctx, uc.rateLimiter, productlimits.Key{
		OrgID:          spec.OrgID,
		ProductSurface: productSurface,
		Area:           productlimits.AreaConnectorExecution,
	}, productlimits.DefaultLimit(productlimits.AreaConnectorExecution)); err != nil {
		return domain.ExecutionResult{}, err
	}
	payloadHash, err := payloadHash(spec.Payload)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("%w: payload must be canonical JSON", ErrInvalidPayload)
	}
	actionBinding := buildActionBinding(config, capability, spec, payloadHash)
	bindingHash, err := actionBindingHash(actionBinding)
	if err != nil {
		return domain.ExecutionResult{}, err
	}

	if spec.IdempotencyKey != "" && spec.TaskID != nil {
		existing, err := uc.repo.GetExecutionByIdempotency(ctx, *spec.TaskID, spec.Operation, spec.NexusRequestID, spec.IdempotencyKey)
		if err == nil && existing.ID != uuid.Nil {
			return existing, nil
		}
		if err != nil && !IsNotFound(err) {
			return domain.ExecutionResult{}, fmt.Errorf("get execution by idempotency: %w", err)
		}
	}

	// Gating obligatorio: operations write/side-effect requieren approval/allow en Nexus.
	//
	// IMPORTANTE — esto es un READ-THROUGH PASS, no una decisión local.
	// Companion NO evalúa policies, NO computa risk, NO decide approve/deny.
	// El checker (nexus gateway adapter) consulta a Nexus por HTTP el
	// status del request y se limita a comparar el resultado contra el set
	// "allowed/approved/executed" para autorizar la ejecución del connector.
	// Source of truth = Nexus. Si Nexus cambia la semántica de status, el
	// contract test (companion/internal/tasks/task_fsm_test.go) lo detecta.
	if capability.NeedsNexusApproval() && uc.checker == nil {
		return domain.ExecutionResult{}, ErrUngated
	}
	if capability.NeedsNexusApproval() && spec.NexusRequestID != nil {
		approved, err := uc.checker.AuthorizeExecution(ctx, NexusExecutionIntent{
			NexusRequestID:   *spec.NexusRequestID,
			OrgID:            spec.OrgID,
			ActorID:          spec.ActorID,
			ActorType:        "agent",
			ProductSurface:   productSurfaceFor(capability, spec),
			TaskID:           spec.TaskID,
			RunID:            runIDFor(spec),
			ToolInvocationID: toolInvocationIDFor(capability, spec),
			ConnectorID:      spec.ConnectorID,
			CapabilityID:     capability.ID,
			Operation:        spec.Operation,
			TargetSystem:     config.Kind,
			TargetResource:   config.ID.String(),
			PayloadHash:      payloadHash,
			IdempotencyKey:   spec.IdempotencyKey,
			RiskHint:         capability.RiskClass,
			ActionBinding:    actionBinding,
			BindingHash:      bindingHash,
		})
		if err != nil {
			slog.Error("check nexus approval", "error", err, "nexus_request_id", spec.NexusRequestID)
			return domain.ExecutionResult{}, fmt.Errorf("check nexus approval: %w", err)
		}
		if !approved {
			return domain.ExecutionResult{}, ErrUngated
		}
	} else if capability.NeedsNexusApproval() && spec.NexusRequestID == nil {
		return domain.ExecutionResult{}, ErrUngated
	}

	if err := validatePayloadSchema(spec.Payload, capability.InputSchema); err != nil {
		return domain.ExecutionResult{}, err
	}

	// Validar spec
	if err := conn.Validate(spec); err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("validate spec: %w", err)
	}

	lockKey := executionLockKey(spec)
	if lockKey != "" {
		acquired, err := uc.repo.AcquireExecutionLock(ctx, lockKey)
		if err != nil {
			return domain.ExecutionResult{}, err
		}
		if !acquired {
			return domain.ExecutionResult{}, fmt.Errorf("%w: execution already in progress", ErrConflict)
		}
		defer func() {
			if err := uc.repo.ReleaseExecutionLock(context.Background(), lockKey); err != nil {
				slog.Error("release execution lock", "error", err, "lock_key", lockKey)
			}
		}()

		existing, err := uc.repo.GetExecutionByIdempotency(ctx, *spec.TaskID, spec.Operation, spec.NexusRequestID, spec.IdempotencyKey)
		if err == nil && existing.ID != uuid.Nil {
			return existing, nil
		}
		if err != nil && !IsNotFound(err) {
			return domain.ExecutionResult{}, fmt.Errorf("get execution by idempotency after lock: %w", err)
		}
	}

	// Ejecutar
	result, err := conn.Execute(ctx, spec)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("execute connector %s: %w", conn.ID(), err)
	}
	result.OrgID = spec.OrgID
	result.ActorID = spec.ActorID
	result.IdempotencyKey = spec.IdempotencyKey
	if result.Payload == nil {
		result.Payload = spec.Payload
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	result.EvidenceJSON = buildExecutionEvidence(config, capability, spec, result)

	// Persistir resultado
	if saveErr := uc.repo.SaveExecution(ctx, result); saveErr != nil {
		if IsConflict(saveErr) && spec.IdempotencyKey != "" && spec.TaskID != nil {
			existing, err := uc.repo.GetExecutionByIdempotency(ctx, *spec.TaskID, spec.Operation, spec.NexusRequestID, spec.IdempotencyKey)
			if err == nil && existing.ID != uuid.Nil {
				return existing, nil
			}
		}
		return domain.ExecutionResult{}, fmt.Errorf("save execution result: %w", saveErr)
	}

	logAttrs := []any{
		"execution_id", result.ID.String(),
		"connector_id", spec.ConnectorID.String(),
		"connector_kind", config.Kind,
		"operation", spec.Operation,
		"status", string(result.Status),
		"org_id", spec.OrgID,
		"actor_id", spec.ActorID,
		"product_surface", productSurface,
		"external_ref", result.ExternalRef,
		"duration_ms", result.DurationMS,
	}
	if spec.TaskID != nil {
		logAttrs = append(logAttrs, "task_id", spec.TaskID.String())
	}
	if spec.NexusRequestID != nil {
		logAttrs = append(logAttrs, "nexus_request_id", spec.NexusRequestID.String())
	}
	slog.InfoContext(ctx, "connector_execution_recorded", logAttrs...)

	return result, nil
}

// BuildActionBinding calcula el contrato exacto que debe enviarse a Nexus
// antes de ejecutar esta capability.
func (uc *Usecases) BuildActionBinding(ctx context.Context, spec domain.ExecutionSpec) (map[string]any, string, error) {
	config, err := uc.repo.GetConnector(ctx, spec.ConnectorID)
	if err != nil {
		return nil, "", fmt.Errorf("get connector config: %w", err)
	}
	if err := ensureConnectorOrg(config.OrgID, spec.OrgID); err != nil {
		return nil, "", err
	}
	conn, ok := uc.registry.Get(config.Kind)
	if !ok {
		return nil, "", ErrNotFound
	}
	for _, cap := range conn.Capabilities() {
		if cap.Operation != spec.Operation {
			continue
		}
		capability, err := connectorCapability(conn, cap)
		if err != nil {
			return nil, "", err
		}
		if err := validateExecutionContext(spec, capability); err != nil {
			return nil, "", err
		}
		productSurface := productSurfaceFor(capability, spec)
		if err := uc.requireActiveInstallation(ctx, spec.OrgID, productSurface, "connector_action_binding"); err != nil {
			return nil, "", err
		}
		if err := uc.enforceProductRuntime(ctx, spec.OrgID, productSurface); err != nil {
			return nil, "", err
		}
		payloadHash, err := payloadHash(spec.Payload)
		if err != nil {
			return nil, "", fmt.Errorf("%w: payload must be canonical JSON", ErrInvalidPayload)
		}
		binding := buildActionBinding(config, capability, spec, payloadHash)
		hash, err := actionBindingHash(binding)
		if err != nil {
			return nil, "", err
		}
		return binding, hash, nil
	}
	return nil, "", ErrOperationUnknown
}

func (uc *Usecases) enforceProductRuntime(ctx context.Context, orgID, productSurface string) error {
	if uc.productRuntimeController == nil {
		return nil
	}
	return uc.productRuntimeController.EnforceProductConnectorExecution(ctx, orgID, productSurface)
}

func (uc *Usecases) requireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error {
	if uc.installationGuard == nil {
		return nil
	}
	if err := uc.installationGuard.RequireActiveInstallation(ctx, orgID, productSurface, reason); err != nil {
		if errors.Is(err, products.ErrValidation) {
			return domainerr.Validation(err.Error())
		}
		return fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	return nil
}

// ListExecutions lista resultados de ejecución de un conector.
func (uc *Usecases) ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error) {
	if limit <= 0 {
		limit = 50
	}
	return uc.repo.ListExecutions(ctx, connectorID, limit)
}

// RefreshConnectors invoca Refresh() en cada connector dinámico (los que
// implementan registry.Refresher). Connectors estáticos se ignoran. Los
// errores individuales viajan en cada item — el caller decide si reportar.
func (uc *Usecases) RefreshConnectors(ctx context.Context) []registry.RefreshResult {
	if uc.dynamicRegistrar != nil {
		uc.dynamicRegistrar(ctx)
	}
	return uc.registry.Refresh(ctx)
}

// Capabilities lista las capacidades publicadas de todos los conectores registrados.
func (uc *Usecases) Capabilities(filter domain.CapabilityFilter) []ConnectorCapabilities {
	var out []ConnectorCapabilities
	for _, c := range uc.registry.List() {
		caps := make([]domain.Capability, 0, len(c.Capabilities()))
		for _, cap := range c.Capabilities() {
			manifest, err := connectorCapability(c, cap)
			if err != nil {
				slog.Error("invalid connector capability manifest", "connector", c.ID(), "operation", cap.Operation, "error", err)
				continue
			}
			if !manifest.MatchesFilter(filter) {
				continue
			}
			caps = append(caps, manifest)
		}
		if len(caps) == 0 {
			continue
		}
		out = append(out, ConnectorCapabilities{
			ID:           c.ID(),
			Kind:         c.Kind(),
			Capabilities: caps,
		})
	}
	return out
}

// CapabilityManifests devuelve el catálogo canónico versionado de capabilities
// compilado desde todos los connector manifests disponibles.
func (uc *Usecases) CapabilityManifests(filter domain.CapabilityFilter) ([]capabilities.Manifest, error) {
	manifests := make([]capabilities.Manifest, 0)
	for _, c := range uc.registry.List() {
		for _, cap := range c.Capabilities() {
			manifest, err := capabilities.FromConnectorCapability(c.ID(), c.Kind(), cap)
			if err != nil {
				return nil, err
			}
			if !manifest.ToConnectorCapability().MatchesFilter(filter) {
				continue
			}
			manifests = append(manifests, manifest)
		}
	}
	reg, err := capabilities.NewRegistry(manifests)
	if err != nil {
		return nil, err
	}
	return reg.All(), nil
}

// ConnectorCapabilities agrupa capacidades por conector.
type ConnectorCapabilities struct {
	ID           string
	Kind         string
	Capabilities []domain.Capability
}

func connectorCapability(conn registry.Connector, cap domain.Capability) (domain.Capability, error) {
	manifest, err := capabilities.FromConnectorCapability(conn.ID(), conn.Kind(), cap)
	if err != nil {
		return domain.Capability{}, fmt.Errorf("capability manifest %s.%s: %w", conn.ID(), cap.Operation, err)
	}
	return manifest.ToConnectorCapability().Normalized(conn.ID(), conn.Kind()), nil
}

// NexusCheckerAdapter adapta el nexusclient para verificar aprobaciones.
type NexusCheckerAdapter struct {
	getRequest func(ctx context.Context, id uuid.UUID) (NexusRequestMeta, int, error)
}

// NewNexusCheckerAdapter crea un adaptador para verificar aprobaciones.
func NewNexusCheckerAdapter(getRequest func(ctx context.Context, id uuid.UUID) (NexusRequestMeta, int, error)) *NexusCheckerAdapter {
	return &NexusCheckerAdapter{getRequest: getRequest}
}

// AuthorizeExecution verifica si un request de Nexus fue aprobado y pertenece a la misma org.
func (a *NexusCheckerAdapter) AuthorizeExecution(ctx context.Context, intent NexusExecutionIntent) (bool, error) {
	meta, _, err := a.getRequest(ctx, intent.NexusRequestID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(intent.OrgID) == "" || strings.TrimSpace(meta.OrgID) == "" || strings.TrimSpace(intent.OrgID) != strings.TrimSpace(meta.OrgID) {
		return false, ErrForbidden
	}
	if strings.TrimSpace(intent.BindingHash) == "" || strings.TrimSpace(meta.BindingHash) == "" || strings.TrimSpace(intent.BindingHash) != strings.TrimSpace(meta.BindingHash) {
		return false, ErrUngated
	}
	// Estados que indican que Nexus ya permitió la ejecución.
	return meta.Status == "allowed" || meta.Status == "approved" || meta.Status == "executed", nil
}

// SeedDefaultConnectors registra conectores por defecto en el registry y en DB.
func (uc *Usecases) SeedDefaultConnectors(ctx context.Context) error {
	for _, conn := range uc.registry.List() {
		capsJSON, mErr := json.Marshal(conn.Capabilities())
		if mErr != nil {
			slog.Error("seed connector marshal capabilities", "kind", conn.Kind(), "error", mErr)
			capsJSON = []byte(`[]`)
		}
		// Final boundary: connector rows are customer-org-owned credentials/config.
		// Static registry entries publish capability schemas only; they must not
		// create org_id='' rows that later act as global execution wildcard.
		slog.InfoContext(ctx, "registered connector capability template", "kind", conn.Kind(), "capabilities", string(capsJSON))
	}
	return nil
}

// ignore para compilación
var _ = time.Now

func validatePayloadSchema(payload json.RawMessage, schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	if typ, ok := schema["type"].(string); ok && typ != "" && typ != "object" {
		return fmt.Errorf("%w: input_schema must describe an object", ErrInvalidPayload)
	}

	var data map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &data); err != nil {
			return fmt.Errorf("%w: payload must be a JSON object", ErrInvalidPayload)
		}
	}
	if data == nil {
		data = make(map[string]any)
	}

	required, ok := requiredSchemaKeys(schema["required"])
	if !ok {
		if _, exists := schema["required"]; exists {
			return fmt.Errorf("%w: input_schema.required must be an array", ErrInvalidPayload)
		}
		return nil
	}
	for _, key := range required {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := data[key]; !exists {
			return fmt.Errorf("%w: missing required field %q", ErrInvalidPayload, key)
		}
	}
	return nil
}

func requiredSchemaKeys(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case nil:
		return nil, false
	case []any:
		keys := make([]string, 0, len(values))
		for _, item := range values {
			keys = append(keys, fmt.Sprint(item))
		}
		return keys, true
	case []string:
		return values, true
	default:
		return nil, false
	}
}

func ensureConnectorOrg(connectorOrgID, specOrgID string) error {
	connectorOrgID = strings.TrimSpace(connectorOrgID)
	specOrgID = strings.TrimSpace(specOrgID)
	if specOrgID == "" {
		return ErrForbidden
	}
	if connectorOrgID != "" && connectorOrgID == specOrgID {
		return nil
	}
	return ErrForbidden
}

func validateExecutionContext(spec domain.ExecutionSpec, capability domain.Capability) error {
	if strings.TrimSpace(spec.OrgID) == "" {
		return ErrForbidden
	}
	if capability.HasSideEffect() && strings.TrimSpace(spec.ActorID) == "" {
		return ErrForbidden
	}
	if (capability.HasSideEffect() || capability.Idempotency.Required) && strings.TrimSpace(spec.IdempotencyKey) == "" {
		return fmt.Errorf("%w: idempotency_key is required for %s", ErrInvalidPayload, capability.Operation)
	}
	if spec.TaskID == nil && len(capability.RequiredScopes) > 0 && !hasRequiredScopes(spec.AuthScopes, capability.RequiredScopes) {
		return ErrForbidden
	}
	return nil
}

func hasRequiredScopes(have []string, required []string) bool {
	seen := make(map[string]struct{}, len(have))
	for _, scope := range have {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			seen[scope] = struct{}{}
		}
	}
	for _, scope := range required {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; !ok {
			return false
		}
	}
	return true
}

func payloadHash(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func mustPayloadHash(raw json.RawMessage) string {
	hash, err := payloadHash(raw)
	if err != nil {
		return ""
	}
	return hash
}

func actionBindingHash(binding map[string]any) (string, error) {
	raw, err := json.Marshal(binding)
	if err != nil {
		return "", fmt.Errorf("marshal action binding: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func buildActionBinding(config domain.Connector, capability domain.Capability, spec domain.ExecutionSpec, payloadHash string) map[string]any {
	binding := map[string]any{
		"schema_version":      toolIntentSchemaVersion,
		"org_id":              strings.TrimSpace(spec.OrgID),
		"actor_id":            strings.TrimSpace(spec.ActorID),
		"actor_type":          firstNonEmpty(spec.ActorType, "agent"),
		"companion_principal": firstNonEmpty(spec.CompanionPrincipal, "companion.employee_ai"),
		"on_behalf_of":        strings.TrimSpace(spec.OnBehalfOf),
		"service_principal":   spec.ServicePrincipal,
		"product_surface":     productSurfaceFor(capability, spec),
		"run_id":              runIDFor(spec),
		"tool_invocation_id":  toolInvocationIDFor(capability, spec),
		"connector_id":        spec.ConnectorID.String(),
		"capability_id":       capability.ID,
		"capability_version":  capability.Version,
		"operation":           spec.Operation,
		"action_type":         capability.ActionType,
		"target_system":       config.Kind,
		"target_resource":     config.ID.String(),
		"payload_hash":        payloadHash,
		"idempotency_key":     strings.TrimSpace(spec.IdempotencyKey),
		"risk_hint":           capability.RiskClass,
		"side_effect_type":    capability.SideEffectType,
		"nexus_action_type":   capability.NexusActionType,
		"cost_class":          capability.CostClass,
		"rate_limit_class":    capability.RateLimitClass,
		"observability_tags":  append([]string(nil), capability.ObservabilityTags...),
	}
	if spec.TaskID != nil {
		binding["task_id"] = spec.TaskID.String()
	}
	return binding
}

func runIDFor(spec domain.ExecutionSpec) string {
	if value := strings.TrimSpace(spec.RunID); value != "" {
		return value
	}
	if spec.TaskID != nil {
		return spec.TaskID.String()
	}
	return "connector-execution:" + strings.TrimSpace(spec.IdempotencyKey)
}

func toolInvocationIDFor(capability domain.Capability, spec domain.ExecutionSpec) string {
	if value := strings.TrimSpace(spec.ToolInvocationID); value != "" {
		return value
	}
	return strings.TrimSpace(capability.Operation) + ":" + strings.TrimSpace(spec.IdempotencyKey)
}

func productSurfaceFor(capability domain.Capability, spec domain.ExecutionSpec) string {
	if value := strings.TrimSpace(spec.ProductSurface); value != "" {
		return value
	}
	if value := strings.TrimSpace(capability.Product); value != "" {
		return value
	}
	return "companion"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func executionLockKey(spec domain.ExecutionSpec) string {
	if spec.TaskID == nil || strings.TrimSpace(spec.IdempotencyKey) == "" {
		return ""
	}
	nexusID := "none"
	if spec.NexusRequestID != nil {
		nexusID = spec.NexusRequestID.String()
	}
	return fmt.Sprintf("connector-execution:%s:%s:%s:%s", spec.TaskID.String(), spec.Operation, nexusID, strings.TrimSpace(spec.IdempotencyKey))
}

func buildExecutionEvidence(config domain.Connector, capability domain.Capability, spec domain.ExecutionSpec, result domain.ExecutionResult) json.RawMessage {
	evidence := map[string]any{
		"actor_id":            strings.TrimSpace(spec.ActorID),
		"actor_type":          firstNonEmpty(spec.ActorType, "agent"),
		"companion_principal": firstNonEmpty(spec.CompanionPrincipal, "companion.employee_ai"),
		"on_behalf_of":        strings.TrimSpace(spec.OnBehalfOf),
		"service_principal":   spec.ServicePrincipal,
		"org_id":              strings.TrimSpace(spec.OrgID),
		"connector_id":        spec.ConnectorID.String(),
		"connector_kind":      config.Kind,
		"capability_id":       capability.ID,
		"capability_version":  capability.Version,
		"operation":           spec.Operation,
		"mode":                capability.Mode,
		"side_effect_class":   capability.SideEffectClass,
		"side_effect":         capability.HasSideEffect(),
		"risk_class":          capability.RiskClass,
		"payload":             sanitizeJSONPayload(spec.Payload),
		"result":              sanitizeJSONPayload(result.ResultJSON),
		"external_ref":        result.ExternalRef,
		"status":              result.Status,
		"error_message":       result.ErrorMessage,
		"duration_ms":         result.DurationMS,
		"idempotency_key":     spec.IdempotencyKey,
		"action_binding":      buildActionBinding(config, capability, spec, mustPayloadHash(spec.Payload)),
		"created_at":          result.CreatedAt.UTC().Format(time.RFC3339Nano),
		"verification":        "unsigned",
		"attestation_ready":   true,
	}
	if spec.TaskID != nil {
		evidence["task_id"] = spec.TaskID.String()
	}
	if spec.NexusRequestID != nil {
		evidence["nexus_request_id"] = spec.NexusRequestID.String()
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func sanitizeJSONPayload(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "[unparseable]"
	}
	return sanitizeValue(value)
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "***"
				continue
			}
			out[key] = sanitizeValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeValue(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	for _, token := range []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}
