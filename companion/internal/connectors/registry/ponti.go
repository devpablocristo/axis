package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

const pontiDefaultNexusActionType = "agent.capability.invoke"

// PontiConnector adapter de Companion a Ponti.
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
	var opPayload pontiOperationPayload
	if len(spec.Payload) > 0 {
		if err := json.Unmarshal(spec.Payload, &params); err != nil {
			return domain.ExecutionResult{}, fmt.Errorf("parse payload: %w", err)
		}
		if err := json.Unmarshal(spec.Payload, &opPayload); err != nil {
			return domain.ExecutionResult{}, fmt.Errorf("parse operation payload: %w", err)
		}
	}

	var raw json.RawMessage
	var execErr error
	readWrap := false
	switch spec.Operation {
	case "ponti.insights.list":
		raw, execErr = p.client.ListInsights(ctx, spec.OrgID, params.Limit, params.IncludeResolved)
	case "ponti.insights.summary":
		raw, execErr = p.client.SummaryInsights(ctx, spec.OrgID)
	case "ponti.insights.explain":
		raw, execErr = p.client.ExplainInsight(ctx, spec.OrgID, params.InsightID)
	case "ponti.dashboard.summary":
		raw, execErr = p.client.DashboardSummary(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.stock.summary":
		raw, execErr = p.client.StockSummary(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.workorders.list":
		raw, execErr = p.client.WorkOrdersList(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.workorders.metrics":
		raw, execErr = p.client.WorkOrdersMetrics(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.lots.summary":
		raw, execErr = p.client.LotsSummary(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.supplies.summary":
		raw, execErr = p.client.SuppliesSummary(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.reports.field_crop.summary":
		raw, execErr = p.client.ReportSummary(ctx, spec.OrgID, "field-crop", opPayload)
		readWrap = true
	case "ponti.reports.investor_contribution.summary":
		raw, execErr = p.client.ReportSummary(ctx, spec.OrgID, "investor-contribution", opPayload)
		readWrap = true
	case "ponti.reports.summary_results.summary":
		raw, execErr = p.client.ReportSummary(ctx, spec.OrgID, "summary-results", opPayload)
		readWrap = true
	case "ponti.data_integrity.summary":
		raw, execErr = p.client.DataIntegritySummary(ctx, spec.OrgID, opPayload)
		readWrap = true
	case "ponti.insight.resolve.prepare":
		raw, execErr = p.client.PrepareInsightResolve(ctx, spec.OrgID, spec.Payload)
	case "ponti.workorder.draft.prepare":
		raw, execErr = p.client.PrepareWorkOrderDraft(ctx, spec.OrgID, spec.Payload)
	case "ponti.stock_adjustment.prepare":
		raw, execErr = p.client.PrepareStockAdjustment(ctx, spec.OrgID, spec.Payload)
	case "ponti.workorder_draft.create":
		raw, execErr = p.client.CreateWorkOrderDraft(ctx, spec.OrgID, spec.Payload, nexusRequestIDString(spec.NexusRequestID))
		raw = wrapPontiDraftExecution(spec, raw, execErr)
	case "ponti.insight_resolution.draft":
		raw, execErr = p.client.DraftInsightResolution(ctx, spec.OrgID, spec.Payload, nexusRequestIDString(spec.NexusRequestID))
		raw = wrapPontiDraftExecution(spec, raw, execErr)
	case "ponti.stock_count.draft":
		raw, execErr = p.client.DraftStockCount(ctx, spec.OrgID, spec.Payload, nexusRequestIDString(spec.NexusRequestID))
		raw = wrapPontiDraftExecution(spec, raw, execErr)
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
	if readWrap && execErr == nil {
		raw = wrapPontiReadResult(spec, opPayload, raw)
	}

	evidence := map[string]any{
		"source_ref":           fmt.Sprintf("ponti.%s", spec.Operation),
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
	if workspace := workspaceEvidence(opPayload); len(workspace) > 0 {
		evidence["workspace"] = workspace
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

func wrapPontiReadResult(spec domain.ExecutionSpec, payload pontiOperationPayload, raw json.RawMessage) json.RawMessage {
	var decoded map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	items := firstArray(decoded, "items", "rows", "data")
	summary := map[string]any{}
	if value, ok := decoded["summary"].(map[string]any); ok {
		summary = value
	} else if value, ok := decoded["metrics"].(map[string]any); ok {
		summary = value
	}
	totals := map[string]any{}
	for _, key := range []string{"page_info", "net_total_usd", "total_liters", "total_kilograms", "sum_sowed", "sum_cost", "totals", "totals_row"} {
		if value, ok := decoded[key]; ok {
			totals[key] = value
		}
	}
	filters := map[string]any{}
	_ = json.Unmarshal(spec.Payload, &filters)
	out := map[string]any{
		"source":      spec.Operation,
		"workspace":   workspaceEvidence(payload),
		"filters":     filters,
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"summary":     summary,
		"totals":      totals,
		"items":       items,
		"warnings":    []any{},
		"raw":         decoded,
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return encoded
}

func wrapPontiDraftExecution(spec domain.ExecutionSpec, raw json.RawMessage, execErr error) json.RawMessage {
	if execErr != nil {
		return raw
	}
	var decoded any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	draftID := draftIDFromRaw(decoded)
	nexusRequestID := nexusRequestIDString(spec.NexusRequestID)
	out := map[string]any{
		"status":           "draft",
		"action":           spec.Operation,
		"write_performed":  spec.Operation == "ponti.workorder_draft.create",
		"draft_id":         draftID,
		"execution_status": "draft_created",
		"nexus_request_id": nexusRequestID,
		"audit_ref":        fmt.Sprintf("ponti.%s:%v", spec.Operation, draftID),
		"result":           decoded,
		"evidence": map[string]any{
			"source_ref":        "ponti." + spec.Operation,
			"captured_at":       time.Now().UTC().Format(time.RFC3339),
			"tenant_scope":      spec.OrgID,
			"approval_required": true,
			"nexus_request_id":  nexusRequestID,
		},
	}
	if spec.Operation != "ponti.workorder_draft.create" {
		out["execution_status"] = "draft_staged"
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return encoded
}

func firstArray(decoded map[string]any, keys ...string) []any {
	for _, key := range keys {
		if arr, ok := decoded[key].([]any); ok {
			return arr
		}
	}
	return []any{}
}

func workspaceEvidence(payload pontiOperationPayload) map[string]any {
	out := map[string]any{}
	set := func(key string, value *int64) {
		if value != nil && *value > 0 {
			out[key] = *value
		}
	}
	set("customer_id", firstInt64Ptr(payload.Workspace.CustomerID, payload.CustomerID))
	set("project_id", firstInt64Ptr(payload.Workspace.ProjectID, payload.ProjectID))
	set("campaign_id", firstInt64Ptr(payload.Workspace.CampaignID, payload.CampaignID))
	set("field_id", firstInt64Ptr(payload.Workspace.FieldID, payload.FieldID))
	return out
}

func nexusRequestIDString(id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	return id.String()
}

func draftIDFromRaw(decoded any) any {
	switch value := decoded.(type) {
	case float64, string:
		return value
	case map[string]any:
		for _, key := range []string{"draft_id", "id"} {
			if v, ok := value[key]; ok {
				return v
			}
		}
		if result, ok := value["result"].(map[string]any); ok {
			return draftIDFromRaw(result)
		}
	}
	return nil
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
	idempotency := domain.IdempotencyContract{}
	idempotencyMode := "none"
	nexusActionType := ""
	approvalPolicy := domain.ApprovalPolicy{Required: requiresNexus}
	if !readOnly {
		idempotency = domain.IdempotencyContract{
			Required:  true,
			KeyFields: []string{"tenant_id", "task_id", "operation", "idempotency_key"},
		}
		idempotencyMode = "required"
	}
	if requiresNexus {
		nexusActionType = strings.TrimSpace(tool.Governance.ActionType)
		if nexusActionType == "" {
			nexusActionType = pontiDefaultNexusActionType
		}
	}
	return domain.Capability{
		ID:              tool.Name,
		Version:         m.Version,
		Status:          domain.CapabilityStatusActive,
		OwnerDomain:     m.ID,
		PublishedFrom:   domain.CapabilityPublishedFromProduct,
		Product:         m.Product,
		Operation:       tool.Name,
		ActionType:      mode,
		Mode:            mode,
		SideEffectType:  sideEffectClass,
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
		ApprovalPolicy:        approvalPolicy,
		InputSchema:           tool.InputSchema,
		OutputSchema:          tool.OutputSchema,
		EvidenceFields:        append([]string(nil), tool.EvidenceFields...),
		EvidenceRequired:      append([]string(nil), tool.EvidenceFields...),
		Idempotency:           idempotency,
		IdempotencyMode:       idempotencyMode,
		NexusActionType:       nexusActionType,
		Postconditions:        append([]string(nil), tool.EvidenceFields...),
	}
}

func toolRequiresNexusApproval(tool ai.CapabilityTool) bool {
	return tool.Governance != nil && tool.Governance.RequiresApproval
}
