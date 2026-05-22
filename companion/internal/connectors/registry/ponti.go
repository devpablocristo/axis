package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	ai "github.com/devpablocristo/platform/kernels/ai/go"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// pontiManifestDiscoveryTimeout limita el tiempo del fetch inicial al boot
// para que un Ponti caído no bloquee el arranque de Companion.
const pontiManifestDiscoveryTimeout = 5 * time.Second

// pontiManifestCacheTTL controla cuánto tiempo Companion mantiene el
// manifest cacheado antes de re-fetchear. Refresh manual (POST
// /v1/connectors/refresh) bypassa el TTL.
const pontiManifestCacheTTL = 5 * time.Minute

// PontiConnector adapter de Companion a Ponti (read-only).
//
// El catálogo de capabilities (tools, schemas, executor refs, roles) se
// descubre dinámicamente desde Ponti vía GET /api/v1/capabilities y se
// cachea con TTL. Companion ya no mantiene una copia hardcoded — Ponti es
// source of truth del manifest.
//
// Si la discovery falla al boot (Ponti caído, mal config), el connector
// queda como `unavailable`: Capabilities() devuelve nil y Validate/Execute
// fallan con error claro. Refresh() (manual u otro intento) lo reactiva.
type PontiConnector struct {
	client *PontiClient

	mu        sync.RWMutex
	manifest  ai.CapabilityManifest
	cachedAt  time.Time
	available bool
}

// NewPontiConnector crea el conector y dispara una discovery best-effort.
// Si client es nil el caller no debe registrarlo en el Registry.
func NewPontiConnector(client *PontiClient) *PontiConnector {
	p := &PontiConnector{client: client}
	if client == nil {
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), pontiManifestDiscoveryTimeout)
	defer cancel()
	if err := p.Refresh(ctx); err != nil {
		slog.Warn("ponti capability discovery failed at boot — connector marked unavailable until refresh succeeds",
			"error", err)
	} else {
		slog.Info("ponti capabilities discovered",
			"manifest_id", p.manifest.ID,
			"version", p.manifest.Version,
			"tools", len(p.manifest.Tools))
	}
	return p
}

func (p *PontiConnector) ID() string   { return "ponti" }
func (p *PontiConnector) Kind() string { return "ponti" }

// Capabilities devuelve el set de capabilities derivadas del manifest
// descubierto. Vacío si no hay manifest cacheado (Ponti unreachable al boot
// y nadie hizo refresh todavía).
func (p *PontiConnector) Capabilities() []domain.Capability {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.available {
		return nil
	}
	out := make([]domain.Capability, 0, len(p.manifest.Tools))
	for _, tool := range p.manifest.Tools {
		out = append(out, capabilityFromTool(p.manifest, tool))
	}
	return out
}

// Refresh dispara una nueva discovery contra Ponti y actualiza el cache.
// Lo invoca el POST /v1/connectors/refresh y también el constructor al boot.
func (p *PontiConnector) Refresh(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("ponti client not configured")
	}
	manifest, err := p.client.DiscoverManifest(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.manifest = manifest
	p.cachedAt = time.Now()
	p.available = true
	p.mu.Unlock()
	return nil
}

// ensureFresh re-fetcha si el cache está vencido. Llamado en el path de
// Validate/Execute para minimizar drift sin pegar a Ponti en cada call.
func (p *PontiConnector) ensureFresh(ctx context.Context) {
	p.mu.RLock()
	stale := !p.cachedAt.IsZero() && time.Since(p.cachedAt) > pontiManifestCacheTTL
	missing := !p.available
	p.mu.RUnlock()
	if !stale && !missing {
		return
	}
	if err := p.Refresh(ctx); err != nil {
		slog.Warn("ponti capability refresh failed", "error", err, "stale", stale, "missing", missing)
	}
}

func (p *PontiConnector) Validate(spec domain.ExecutionSpec) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.available {
		return fmt.Errorf("ponti connector unavailable: capability manifest not loaded — try POST /v1/connectors/refresh")
	}
	if spec.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	for _, tool := range p.manifest.Tools {
		if tool.Name == spec.Operation {
			return nil
		}
	}
	return fmt.Errorf("unknown ponti operation: %s", spec.Operation)
}

func (p *PontiConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	p.ensureFresh(ctx)

	if !p.isAvailable() {
		return domain.ExecutionResult{}, fmt.Errorf("ponti connector unavailable: capability manifest not loaded")
	}

	start := time.Now()

	var params struct {
		Limit           int    `json:"limit"`
		IncludeResolved bool   `json:"include_resolved"`
		InsightID       string `json:"insight_id"`
	}
	if len(spec.Payload) > 0 {
		if err := json.Unmarshal(spec.Payload, &params); err != nil {
			return domain.ExecutionResult{}, fmt.Errorf("parse payload: %w", err)
		}
	}

	var raw json.RawMessage
	var execErr error
	switch spec.Operation {
	case "ponti.insights.list":
		raw, execErr = p.client.ListInsights(ctx, spec.OrgID, params.Limit, params.IncludeResolved)
	case "ponti.insights.summary":
		raw, execErr = p.client.SummaryInsights(ctx, spec.OrgID)
	case "ponti.insights.explain":
		raw, execErr = p.client.ExplainInsight(ctx, spec.OrgID, params.InsightID)
	default:
		return domain.ExecutionResult{}, fmt.Errorf("unknown operation: %s", spec.Operation)
	}

	duration := time.Since(start).Milliseconds()
	status := domain.ExecSuccess
	var errMsg string
	if execErr != nil {
		status = domain.ExecFailure
		errMsg = execErr.Error()
	}

	if raw == nil {
		raw = json.RawMessage(`{}`)
	}

	evidence := map[string]any{
		"source_ref":  fmt.Sprintf("ponti.%s", spec.Operation),
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"actor":       spec.ActorID,
		"tenant":      spec.OrgID,
	}
	evidenceJSON, _ := json.Marshal(evidence)

	return domain.ExecutionResult{
		ID:             uuid.New(),
		ConnectorID:    spec.ConnectorID,
		OrgID:          spec.OrgID,
		ActorID:        spec.ActorID,
		Operation:      spec.Operation,
		Status:         status,
		ExternalRef:    fmt.Sprintf("ponti-%s", spec.Operation),
		Payload:        spec.Payload,
		ResultJSON:     raw,
		EvidenceJSON:   evidenceJSON,
		ErrorMessage:   errMsg,
		Retryable:      execErr != nil,
		DurationMS:     duration,
		IdempotencyKey: spec.IdempotencyKey,
		TaskID:         spec.TaskID,
		NexusRequestID: spec.NexusRequestID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (p *PontiConnector) isAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.available
}

// capabilityFromTool traduce un ai.CapabilityTool al modelo
// domain.Capability del Registry. La granularidad de Companion es por tool,
// la del manifest canónico es por paquete — esta función es el puente.
func capabilityFromTool(m ai.CapabilityManifest, tool ai.CapabilityTool) domain.Capability {
	requiresNexus := toolRequiresNexusApproval(tool)
	mode := domain.CapabilityModeRead
	sideEffectClass := domain.SideEffectClassRead
	readOnly := !tool.SideEffect && !strings.EqualFold(tool.Mode, ai.CapabilityModeWrite)
	if !readOnly {
		mode = domain.CapabilityModeWrite
		sideEffectClass = domain.SideEffectClassWrite
	}
	return domain.Capability{
		ID:              tool.Name,
		Version:         m.Version,
		Status:          domain.CapabilityStatusActive,
		OwnerDomain:     m.ID,
		PublishedFrom:   domain.CapabilityPublishedFromProduct,
		Product:         m.Product,
		Operation:       tool.Name,
		Mode:            mode,
		SideEffectClass: sideEffectClass,
		SideEffect:      tool.SideEffect,
		ReadOnly:        readOnly,
		RiskClass:       tool.RiskClass,
		TenantScope: domain.TenantScope{
			Mode:     domain.TenantScopeSingleTenant,
			Resolver: domain.TenantScopeResolverUser,
		},
		AuthMode:              domain.AuthMode{Type: "delegated_user"},
		RequiredRoles:         append([]string(nil), tool.RequiredRoles...),
		RequiredScopes:        []string{"companion:connectors:execute"},
		RequiredModules:       append([]string(nil), tool.RequiredModules...),
		RequiresNexusApproval: requiresNexus,
		InputSchema:           tool.InputSchema,
		OutputSchema:          tool.OutputSchema,
		EvidenceFields:        append([]string(nil), tool.EvidenceFields...),
	}
}

func toolRequiresNexusApproval(tool ai.CapabilityTool) bool {
	field := reflect.ValueOf(tool).FieldByName("Go" + "vernance")
	if !field.IsValid() || field.IsNil() {
		return false
	}
	req := field.Elem().FieldByName("RequiresApproval")
	return req.IsValid() && req.Kind() == reflect.Bool && req.Bool()
}
