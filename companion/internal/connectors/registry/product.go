package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	ai "github.com/devpablocristo/platform/kernels/ai/go"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// productManifestDiscoveryTimeout limita el fetch inicial al boot para que
// un producto caído no bloquee el arranque de Companion.
const productManifestDiscoveryTimeout = 5 * time.Second

// productManifestCacheTTL controla cuánto tiempo se cachea el manifest antes
// de re-fetchear. Refresh manual (POST /v1/connectors/refresh) bypassa el TTL.
const productManifestCacheTTL = 5 * time.Minute

// ProductConnector es el adapter genérico manifest-driven de Companion a
// cualquier producto que implemente el contrato envelope capability_execution.v1.
//
// A diferencia de PontiConnector (switch hardcoded operación→método), este
// connector no conoce las operaciones del producto: descubre el catálogo vía
// GET {base_url}{discovery_path} y ejecuta TODA operación posteando el
// envelope a {base_url}{execute_path}. Un producto puede publicar nuevas
// capabilities sin cambios de código en Axis.
//
// Si la discovery falla al boot (producto caído, sin instalación envelope),
// el connector queda `unavailable`: Capabilities() devuelve nil y
// Validate/Execute fallan con error claro. Refresh() lo reactiva.
type ProductConnector struct {
	client *ProductClient

	mu        sync.RWMutex
	manifest  ai.CapabilityManifest
	cachedAt  time.Time
	available bool
}

// NewProductConnector crea el conector y dispara una discovery best-effort.
// Si client es nil el caller no debe registrarlo en el Registry.
func NewProductConnector(client *ProductClient) *ProductConnector {
	p := &ProductConnector{client: client}
	if client == nil {
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), productManifestDiscoveryTimeout)
	defer cancel()
	if err := p.Refresh(ctx); err != nil {
		slog.Warn("product capability discovery failed at boot — connector marked unavailable until refresh succeeds",
			"product_surface", client.ProductSurface(), "error", err)
	} else {
		slog.Info("product capabilities discovered",
			"product_surface", client.ProductSurface(),
			"manifest_id", p.manifest.ID,
			"version", p.manifest.Version,
			"tools", len(p.manifest.Tools))
	}
	return p
}

func (p *ProductConnector) ID() string   { return p.client.ProductSurface() }
func (p *ProductConnector) Kind() string { return p.client.ProductSurface() }

// Capabilities devuelve el set de capabilities derivadas del manifest
// descubierto, normalizadas con el mismo puente capabilityFromTool que usa
// PontiConnector. Vacío si no hay manifest cacheado.
func (p *ProductConnector) Capabilities() []domain.Capability {
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

// Refresh dispara una nueva discovery contra el producto y actualiza el cache.
func (p *ProductConnector) Refresh(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("product client not configured")
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

// ensureFresh re-fetcha si el cache está vencido, espejando PontiConnector.
func (p *ProductConnector) ensureFresh(ctx context.Context) {
	p.mu.RLock()
	stale := !p.cachedAt.IsZero() && time.Since(p.cachedAt) > productManifestCacheTTL
	missing := !p.available
	p.mu.RUnlock()
	if !stale && !missing {
		return
	}
	if err := p.Refresh(ctx); err != nil {
		slog.Warn("product capability refresh failed",
			"product_surface", p.client.ProductSurface(), "error", err, "stale", stale, "missing", missing)
	}
}

func (p *ProductConnector) Validate(spec domain.ExecutionSpec) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.available {
		return fmt.Errorf("%s connector unavailable: capability manifest not loaded — try POST /v1/connectors/refresh", p.client.ProductSurface())
	}
	if spec.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	for _, tool := range p.manifest.Tools {
		if tool.Name == spec.Operation {
			return nil
		}
	}
	return fmt.Errorf("unknown %s operation: %s", p.client.ProductSurface(), spec.Operation)
}

func (p *ProductConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	p.ensureFresh(ctx)

	surface := p.client.ProductSurface()
	executorRef, ok := p.executorRef(spec.Operation)
	if !ok {
		if !p.isAvailable() {
			return domain.ExecutionResult{}, fmt.Errorf("%s connector unavailable: capability manifest not loaded", surface)
		}
		return domain.ExecutionResult{}, fmt.Errorf("unknown operation: %s", spec.Operation)
	}

	start := time.Now()
	workspace := productWorkspaceFromPayload(spec.Payload)
	envelope := productExecutionEnvelope{
		SchemaVersion:  ProductExecutionSchemaVersion,
		Operation:      spec.Operation,
		ExecutorRef:    executorRef,
		Payload:        spec.Payload,
		Workspace:      workspace,
		IdempotencyKey: spec.IdempotencyKey,
		RunID:          spec.RunID,
		NexusRequestID: nexusRequestIDString(spec.NexusRequestID),
		Actor: productEnvelopeActor{
			ActorID:        spec.ActorID,
			ActorType:      spec.ActorType,
			OnBehalfOf:     spec.OnBehalfOf,
			ProductSurface: spec.ProductSurface,
		},
		OrgID: spec.OrgID,
	}
	if spec.TaskID != nil && *spec.TaskID != uuid.Nil {
		envelope.TaskID = spec.TaskID.String()
	}

	resp, execErr := p.client.ExecuteEnvelope(ctx, spec.OrgID, envelope)

	duration := time.Since(start).Milliseconds()
	status := domain.ExecSuccess
	var errMsg string
	switch {
	case execErr != nil:
		status = domain.ExecFailure
		errMsg = execErr.Error()
	case resp.Status == domain.ExecPartial:
		status = domain.ExecPartial
		errMsg = resp.Error
	case resp.Status != "" && resp.Status != domain.ExecSuccess:
		status = domain.ExecFailure
		errMsg = resp.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("product reported status %q", resp.Status)
		}
	}

	raw := resp.Result
	if raw == nil {
		raw = json.RawMessage(`{}`)
	}

	// Evidence canónico de identidad (mismo set que PontiConnector) + merge
	// passthrough del evidence declarado por el producto. Las claves de
	// identidad/atribución de Axis ganan: el producto no puede pisarlas.
	evidence := map[string]any{
		"source_ref":           fmt.Sprintf("%s.%s", surface, spec.Operation),
		"captured_at":          time.Now().UTC().Format(time.RFC3339),
		"org_id":               spec.OrgID,
		"customer_org_id":      spec.OrgID,
		"actor_id":             spec.ActorID,
		"actor_type":           spec.ActorType,
		"companion_principal":  spec.CompanionPrincipal,
		"on_behalf_of":         spec.OnBehalfOf,
		"service_principal":    spec.ServicePrincipal,
		"product_surface":      spec.ProductSurface,
		"capability_operation": spec.Operation,
	}
	if len(workspace) > 0 {
		evidence["workspace"] = workspace
	}
	for key, value := range resp.Evidence {
		if _, exists := evidence[key]; exists {
			continue
		}
		evidence[key] = value
	}
	evidenceJSON, _ := json.Marshal(evidence)

	externalRef := resp.ExternalRef
	if externalRef == "" {
		externalRef = fmt.Sprintf("%s-%s", surface, spec.Operation)
	}

	return domain.ExecutionResult{
		ID:             uuid.New(),
		ConnectorID:    spec.ConnectorID,
		OrgID:          spec.OrgID,
		ActorID:        spec.ActorID,
		Operation:      spec.Operation,
		Status:         status,
		ExternalRef:    externalRef,
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

// executorRef busca el executor_ref del tool en el manifest cacheado.
func (p *ProductConnector) executorRef(operation string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.available {
		return "", false
	}
	for _, tool := range p.manifest.Tools {
		if tool.Name == operation {
			return tool.ExecutorRef, true
		}
	}
	return "", false
}

func (p *ProductConnector) isAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.available
}

// productWorkspaceFromPayload extrae el objeto `workspace` del payload (Ola
// A: contexto de trabajo opaco del producto). Axis lo transporta tal cual al
// envelope y al evidence; no lo interpreta.
func productWorkspaceFromPayload(payload json.RawMessage) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	var decoded struct {
		Workspace map[string]any `json:"workspace"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil
	}
	if len(decoded.Workspace) == 0 {
		return nil
	}
	return decoded.Workspace
}
