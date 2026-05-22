// Package watchers implementa la observación proactiva del estado del negocio.
package watchers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/platform/concurrency/go/worker"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"
	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// NexusGateway port para enviar solicitudes a Nexus.
type NexusGateway interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error)
	GetRequest(ctx context.Context, id string) (nexusclient.RequestSummary, int, error)
	ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error)
}

// ConnectorExecutor ejecuta side effects usando el pipeline controlado de connectors.
type ConnectorExecutor interface {
	ListConnectors(ctx context.Context) ([]connectordomain.Connector, error)
	BuildActionBinding(ctx context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error)
	Execute(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error)
}

// CreateWatcherInput es la entrada para crear un watcher.
type CreateWatcherInput struct {
	OrgID       string
	Name        string
	WatcherType domain.WatcherType
	Config      json.RawMessage
	Enabled     bool
}

// UpdateWatcherInput es la entrada para actualizar un watcher.
type UpdateWatcherInput struct {
	Name    *string
	Config  *json.RawMessage
	Enabled *bool
}

// ChatNotifier permite al watcher empujar alertas proactivas al chat del suscriptor.
type ChatNotifier interface {
	// NotifyAlert crea un mensaje de sistema en la conversación activa del suscriptor.
	// Si no hay conversación activa, crea una nueva tarea-chat con la alerta.
	NotifyAlert(ctx context.Context, orgID, message string) error
}

// Usecases contiene la lógica de negocio del módulo watchers.
type Usecases struct {
	repo     Repository
	nexus    NexusGateway
	executor ConnectorExecutor
	notifier ChatNotifier // nil = sin notificaciones al chat
}

// NewUsecases crea los usecases del módulo watchers.
func NewUsecases(repo Repository, nexus NexusGateway) *Usecases {
	return &Usecases{repo: repo, nexus: nexus}
}

// SetNotifier inyecta el notificador de chat. Opcional.
func (uc *Usecases) SetNotifier(n ChatNotifier) {
	uc.notifier = n
}

// SetConnectorExecutor enruta acciones con side effect por connectors.
func (uc *Usecases) SetConnectorExecutor(executor ConnectorExecutor) {
	uc.executor = executor
}

// --- CRUD ---

// Create crea un nuevo watcher.
func (uc *Usecases) Create(ctx context.Context, input CreateWatcherInput) (domain.Watcher, error) {
	w := domain.Watcher{
		OrgID:       input.OrgID,
		Name:        input.Name,
		WatcherType: input.WatcherType,
		Config:      input.Config,
		Enabled:     input.Enabled,
	}
	return uc.repo.CreateWatcher(ctx, w)
}

// Get obtiene un watcher por ID.
func (uc *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.Watcher, error) {
	return uc.repo.GetWatcher(ctx, id)
}

// List lista watchers de una organización.
func (uc *Usecases) List(ctx context.Context, orgID string) ([]domain.Watcher, error) {
	return uc.repo.ListWatchers(ctx, orgID)
}

// Update actualiza un watcher.
func (uc *Usecases) Update(ctx context.Context, id uuid.UUID, input UpdateWatcherInput) (domain.Watcher, error) {
	w, err := uc.repo.GetWatcher(ctx, id)
	if err != nil {
		return domain.Watcher{}, fmt.Errorf("get watcher for update: %w", err)
	}
	if input.Name != nil {
		w.Name = *input.Name
	}
	if input.Config != nil {
		w.Config = *input.Config
	}
	if input.Enabled != nil {
		w.Enabled = *input.Enabled
	}
	return uc.repo.UpdateWatcher(ctx, w)
}

// Delete elimina un watcher.
func (uc *Usecases) Delete(ctx context.Context, id uuid.UUID) error {
	return uc.repo.DeleteWatcher(ctx, id)
}

// ListProposals lista propuestas de un watcher.
func (uc *Usecases) ListProposals(ctx context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error) {
	return uc.repo.ListProposalsByWatcher(ctx, watcherID, limit)
}

// --- Ejecución ---

// actionTypeForWatcher mapea tipo de watcher a action_type de Nexus.
func actionTypeForWatcher(wt domain.WatcherType) string {
	switch wt {
	case domain.WatcherStaleWorkOrders:
		return "work_order.delay_notify"
	case domain.WatcherUnconfirmedAppointments:
		return "notification.send"
	case domain.WatcherLowStock:
		return "notification.send"
	case domain.WatcherInactiveCustomers:
		return "vehicle.service_reminder"
	case domain.WatcherRevenueDrop:
		return "notification.send"
	default:
		return "notification.send"
	}
}

// RunWatcher ejecuta un watcher: consulta Pymes, crea propuestas, evalúa con Nexus, ejecuta si permite.
func (uc *Usecases) RunWatcher(ctx context.Context, watcherID uuid.UUID) (*domain.WatcherResult, error) {
	w, err := uc.repo.GetWatcher(ctx, watcherID)
	if err != nil {
		return nil, fmt.Errorf("get watcher: %w", err)
	}
	if !w.Enabled {
		return nil, ErrWatcherDisabled
	}

	items, err := uc.queryProductCapability(ctx, w)
	if err != nil {
		slog.Error("watcher query capability failed", "watcher_id", w.ID, "error", err)
		return nil, fmt.Errorf("query product capability: %w", err)
	}

	result := &domain.WatcherResult{Found: len(items)}

	for _, item := range items {
		proposal, err := uc.processItem(ctx, w, item)
		if err != nil {
			slog.Warn("watcher process item failed", "watcher_id", w.ID, "item_id", item.ID, "error", err)
			continue
		}
		result.Proposed++
		if proposal.ExecutionStatus == domain.ProposalExecuted {
			result.Executed++
		}
	}

	// Actualizar último resultado
	now := time.Now().UTC()
	w.LastRunAt = &now
	resultJSON, err := json.Marshal(result)
	if err != nil {
		slog.Error("watcher marshal result failed", "watcher_id", w.ID, "error", err)
		resultJSON = []byte(`{}`)
	}
	w.LastResult = resultJSON
	if _, err := uc.repo.UpdateWatcher(ctx, w); err != nil {
		slog.Error("watcher update last run failed", "watcher_id", w.ID, "error", err)
	}

	// Notificar al chat si hubo hallazgos
	if uc.notifier != nil && result.Found > 0 {
		msg := fmt.Sprintf("Alerta de %s: encontré %d items", w.Name, result.Found)
		if result.Executed > 0 {
			msg += fmt.Sprintf(", %d ya se ejecutaron automáticamente", result.Executed)
		}
		if pending := result.Proposed - result.Executed; pending > 0 {
			msg += fmt.Sprintf(", %d esperan tu aprobación", pending)
		}
		msg += "."
		if err := uc.notifier.NotifyAlert(ctx, w.OrgID, msg); err != nil {
			slog.Error("watcher chat notification failed", "watcher_id", w.ID, "error", err)
		}
	}

	return result, nil
}

func (uc *Usecases) queryProductCapability(ctx context.Context, w domain.Watcher) ([]domain.PymesItem, error) {
	switch w.WatcherType {
	case domain.WatcherStaleWorkOrders:
		var cfg domain.StaleWorkOrdersConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdDays <= 0 {
			cfg.ThresholdDays = 3
		}
		return uc.queryItems(ctx, w, "pymes.get_work_orders", map[string]any{
			"org_id":         w.OrgID,
			"threshold_days": cfg.ThresholdDays,
		})

	case domain.WatcherUnconfirmedAppointments:
		var cfg domain.UnconfirmedAppointmentsConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.HoursBeforeAppointment <= 0 {
			cfg.HoursBeforeAppointment = 24
		}
		return uc.queryItems(ctx, w, "pymes.get_appointments", map[string]any{
			"org_id":                   w.OrgID,
			"hours_before_appointment": cfg.HoursBeforeAppointment,
		})

	case domain.WatcherLowStock:
		var cfg domain.LowStockConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdUnits <= 0 {
			cfg.ThresholdUnits = 5
		}
		return uc.queryItems(ctx, w, "pymes.get_low_stock", map[string]any{
			"org_id":          w.OrgID,
			"threshold_units": cfg.ThresholdUnits,
		})

	case domain.WatcherInactiveCustomers:
		var cfg domain.InactiveCustomersConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdMonths <= 0 {
			cfg.ThresholdMonths = 6
		}
		return uc.queryItems(ctx, w, "pymes.get_customers", map[string]any{
			"org_id":           w.OrgID,
			"threshold_months": cfg.ThresholdMonths,
		})

	case domain.WatcherRevenueDrop:
		comparison, err := uc.queryRevenueComparison(ctx, w)
		if err != nil {
			return nil, fmt.Errorf("get revenue comparison: %w", err)
		}
		var cfg domain.RevenueDropConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdPercent <= 0 {
			cfg.ThresholdPercent = 20
		}
		if comparison.DropPercent >= cfg.ThresholdPercent {
			meta, err := json.Marshal(comparison)
			if err != nil {
				return nil, fmt.Errorf("marshal revenue comparison: %w", err)
			}
			return []domain.PymesItem{{
				ID:       "revenue_alert",
				Type:     "revenue",
				Name:     fmt.Sprintf("Caida de %.1f%% en facturacion", comparison.DropPercent),
				Metadata: meta,
			}}, nil
		}
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown watcher type: %s", w.WatcherType)
	}
}

func (uc *Usecases) queryItems(ctx context.Context, w domain.Watcher, operation string, payload map[string]any) ([]domain.PymesItem, error) {
	result, err := uc.queryCapability(ctx, w, operation, payload)
	if err != nil {
		return nil, err
	}
	var items []domain.PymesItem
	if err := json.Unmarshal(result.ResultJSON, &items); err != nil {
		return nil, fmt.Errorf("parse capability items: %w", err)
	}
	return items, nil
}

func (uc *Usecases) queryRevenueComparison(ctx context.Context, w domain.Watcher) (*domain.RevenueComparison, error) {
	result, err := uc.queryCapability(ctx, w, "pymes.get_revenue_comparison", map[string]any{"org_id": w.OrgID})
	if err != nil {
		return nil, err
	}
	var comparison domain.RevenueComparison
	if err := json.Unmarshal(result.ResultJSON, &comparison); err != nil {
		return nil, fmt.Errorf("parse revenue comparison: %w", err)
	}
	return &comparison, nil
}

func (uc *Usecases) queryCapability(ctx context.Context, w domain.Watcher, operation string, payload map[string]any) (connectordomain.ExecutionResult, error) {
	if uc.executor == nil {
		return connectordomain.ExecutionResult{}, fmt.Errorf("connector executor not configured")
	}
	connectorID, err := uc.findConnectorByKind(ctx, "pymes", w.OrgID)
	if err != nil {
		return connectordomain.ExecutionResult{}, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["org_id"] = w.OrgID
	raw, err := json.Marshal(payload)
	if err != nil {
		return connectordomain.ExecutionResult{}, fmt.Errorf("marshal watcher query payload: %w", err)
	}
	return uc.executor.Execute(ctx, connectordomain.ExecutionSpec{
		ConnectorID:      connectorID,
		OrgID:            w.OrgID,
		ActorID:          "nexus_companion",
		ProductSurface:   "pymes",
		AuthScopes:       []string{"companion:connectors:execute"},
		RunID:            "watcher:" + w.ID.String(),
		ToolInvocationID: "watcher-query:" + operation + ":" + w.ID.String(),
		Operation:        operation,
		Payload:          raw,
	})
}

func (uc *Usecases) processItem(ctx context.Context, w domain.Watcher, item domain.PymesItem) (domain.Proposal, error) {
	actionType := actionTypeForWatcher(w.WatcherType)
	itemParams := map[string]string{
		"item_id":   item.ID,
		"item_type": item.Type,
		"item_name": item.Name,
		"phone":     item.Phone,
		"party_id":  item.PartyID,
	}
	params, err := json.Marshal(itemParams)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("marshal proposal params: %w", err)
	}

	proposal := domain.Proposal{
		WatcherID:      w.ID,
		OrgID:          w.OrgID,
		ActionType:     actionType,
		TargetResource: item.ID,
		Params:         params,
		Reason:         fmt.Sprintf("Watcher %s detectó: %s", w.Name, item.Name),
	}

	created, err := uc.repo.CreateProposal(ctx, proposal)
	if err != nil {
		return proposal, fmt.Errorf("create proposal: %w", err)
	}
	proposal = created

	execSpec, binding, bindingHash, err := uc.buildWatcherExecutionSpec(ctx, w, item, proposal.ID, nil)
	if err != nil {
		now := time.Now().UTC()
		proposal.ExecutionStatus = domain.ProposalFailed
		proposal.ResolvedAt = &now
		proposal.ExecutionResult = marshalSyncErrorResult("build_connector_intent_failed", err)
		_ = uc.repo.UpdateProposal(ctx, proposal)
		return proposal, fmt.Errorf("build connector intent: %w", err)
	}

	// Consultar Nexus
	idempotencyKey := fmt.Sprintf("companion-watcher-%s-%s", w.ID, proposal.ID)
	nexusParams := map[string]any{
		"org_id":               w.OrgID,
		"proposal_id":          proposal.ID.String(),
		"watcher_id":           w.ID.String(),
		"proposed_action_type": actionType,
		"item":                 itemParams,
		"action_binding":       binding,
		"binding_hash":         bindingHash,
	}
	nexusResp, err := uc.nexus.SubmitRequest(ctx, idempotencyKey, nexusclient.SubmitRequestBody{
		RequesterType:  "service",
		RequesterID:    "nexus_companion",
		RequesterName:  "Nexus Companion Watcher",
		ActionType:     "companion.propose",
		TargetSystem:   fmt.Sprint(binding["target_system"]),
		TargetResource: fmt.Sprint(binding["target_resource"]),
		Params:         nexusParams,
		Reason:         proposal.Reason,
	})
	if err != nil {
		slog.Error("watcher nexus submit failed", "proposal_id", proposal.ID, "error", err)
		// Persistir el fallo en el proposal creado: si no, queda como pending
		// con nexus_request_id NULL — invisible para SyncPendingProposals y
		// difícil de reconciliar a mano. Marcamos failed con reason para que
		// un dashboard/listado muestre el orphan.
		now := time.Now().UTC()
		proposal.ExecutionStatus = domain.ProposalFailed
		proposal.ResolvedAt = &now
		proposal.ExecutionResult = marshalSyncErrorResult("submit_nexus_failed", err)
		if upErr := uc.repo.UpdateProposal(ctx, proposal); upErr != nil {
			slog.Error("watcher mark submit-failed proposal failed", "proposal_id", proposal.ID, "error", upErr)
		}
		return proposal, fmt.Errorf("submit nexus request: %w", err)
	}

	nexusID, _ := uuid.Parse(nexusResp.RequestID)
	if nexusID != uuid.Nil {
		proposal.NexusRequestID = &nexusID
		execSpec.NexusRequestID = &nexusID
	}

	decision := nexusResp.Decision
	proposal.NexusDecision = &decision

	switch {
	case decision == "allowed" || decision == "allow" || decision == "approved":
		// Ejecutar acción
		execResult, execErr := uc.executeAction(ctx, execSpec)
		now := time.Now().UTC()
		proposal.ResolvedAt = &now
		if execErr != nil {
			proposal.ExecutionStatus = domain.ProposalFailed
			errJSON, mErr := json.Marshal(map[string]string{"error": execErr.Error()})
			if mErr != nil {
				slog.Error("watcher marshal exec error failed", "proposal_id", proposal.ID, "error", mErr)
				errJSON = []byte(`{"error":"marshal_failed"}`)
			}
			proposal.ExecutionResult = errJSON
			uc.reportExecutionToNexus(ctx, proposal.NexusRequestID, execResult, false, execErr.Error())
		} else {
			proposal.ExecutionStatus = domain.ProposalExecuted
			proposal.ExecutionResult = watcherExecutionResultJSON(execResult, "inline")
			uc.reportExecutionToNexus(ctx, proposal.NexusRequestID, execResult, true, "")
		}

	case decision == "denied" || decision == "deny" || decision == "rejected":
		now := time.Now().UTC()
		proposal.ExecutionStatus = domain.ProposalSkipped
		proposal.ResolvedAt = &now

	default:
		// require_approval — queda pendiente
		proposal.ExecutionStatus = domain.ProposalPending
	}

	if err := uc.repo.UpdateProposal(ctx, proposal); err != nil {
		slog.Error("watcher update proposal failed", "proposal_id", proposal.ID, "error", err)
	}

	return proposal, nil
}

func (uc *Usecases) executeAction(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
	if uc.executor == nil {
		return connectordomain.ExecutionResult{}, fmt.Errorf("connector executor not configured")
	}
	return uc.executor.Execute(ctx, spec)
}

func (uc *Usecases) buildWatcherExecutionSpec(ctx context.Context, w domain.Watcher, item domain.PymesItem, proposalID uuid.UUID, nexusID *uuid.UUID) (connectordomain.ExecutionSpec, map[string]any, string, error) {
	if item.PartyID == "" && item.Phone == "" {
		return connectordomain.ExecutionSpec{}, nil, "", fmt.Errorf("no contact info for item %s", item.ID)
	}
	if uc.executor == nil {
		return connectordomain.ExecutionSpec{}, nil, "", fmt.Errorf("connector executor not configured")
	}
	connectorID, err := uc.findConnectorByKind(ctx, "pymes", w.OrgID)
	if err != nil {
		return connectordomain.ExecutionSpec{}, nil, "", err
	}
	payload, err := json.Marshal(map[string]any{
		"org_id":   w.OrgID,
		"party_id": item.PartyID,
		"body":     watcherMessage(w.WatcherType, item),
	})
	if err != nil {
		return connectordomain.ExecutionSpec{}, nil, "", fmt.Errorf("marshal watcher connector payload: %w", err)
	}
	spec := connectordomain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          w.OrgID,
		ActorID:        "nexus_companion",
		ProductSurface: "pymes",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "pymes.send_whatsapp_text",
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("watcher-execute-%s", proposalID.String()),
		NexusRequestID: nexusID,
	}
	binding, bindingHash, err := uc.executor.BuildActionBinding(ctx, spec)
	if err != nil {
		return connectordomain.ExecutionSpec{}, nil, "", err
	}
	return spec, binding, bindingHash, nil
}

func (uc *Usecases) findConnectorByKind(ctx context.Context, kind, orgID string) (uuid.UUID, error) {
	conns, err := uc.executor.ListConnectors(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("list connectors: %w", err)
	}
	for _, c := range conns {
		if c.Kind != kind || !c.Enabled {
			continue
		}
		if c.OrgID != "" && c.OrgID == orgID {
			return c.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("connector kind %s not configured for org %s", kind, orgID)
}

func watcherMessage(kind domain.WatcherType, item domain.PymesItem) string {
	switch kind {
	case domain.WatcherStaleWorkOrders:
		return "Hola! Te informamos que tu orden de trabajo esta en proceso. Lamentamos la demora y estamos trabajando en ello."
	case domain.WatcherUnconfirmedAppointments:
		return "Hola! Te recordamos que tenes un turno agendado. Por favor, confirma tu asistencia."
	case domain.WatcherInactiveCustomers:
		return "Hola! Hace tiempo que no nos visitas. Te esperamos!"
	case domain.WatcherLowStock, domain.WatcherRevenueDrop:
		return fmt.Sprintf("Alerta: %s", item.Name)
	default:
		return fmt.Sprintf("Hola! Te contactamos desde el negocio: %s", item.Name)
	}
}

func watcherExecutionResultJSON(result connectordomain.ExecutionResult, via string) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"status":                 result.Status,
		"via":                    via,
		"connector_execution_id": result.ID.String(),
		"external_ref":           result.ExternalRef,
	})
	if err != nil {
		return json.RawMessage(`{"status":"unknown"}`)
	}
	return raw
}

func (uc *Usecases) reportExecutionToNexus(ctx context.Context, nexusID *uuid.UUID, result connectordomain.ExecutionResult, success bool, errorMessage string) {
	if uc.nexus == nil || nexusID == nil || *nexusID == uuid.Nil {
		return
	}
	payload := map[string]any{
		"connector_execution_id": result.ID.String(),
		"connector_id":           result.ConnectorID.String(),
		"operation":              result.Operation,
		"external_ref":           result.ExternalRef,
		"org_id":                 result.OrgID,
		"actor_id":               result.ActorID,
	}
	status, err := uc.nexus.ReportResult(ctx, nexusID.String(), success, payload, result.DurationMS, errorMessage)
	if err != nil || status >= 400 {
		slog.Warn("watcher report execution to nexus failed",
			"nexus_request_id", nexusID.String(),
			"status", status,
			"error", err)
	}
}

// RunAllEnabled ejecuta todos los watchers habilitados de una organización.
func (uc *Usecases) RunAllEnabled(ctx context.Context, orgID string) error {
	watchers, err := uc.repo.ListWatchers(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list watchers: %w", err)
	}
	for _, w := range watchers {
		if !w.Enabled {
			continue
		}
		if _, err := uc.RunWatcher(ctx, w.ID); err != nil {
			slog.Error("run watcher failed", "watcher_id", w.ID, "error", err)
		}
	}
	return nil
}

// RunWatcherLoop ejecuta watchers periódicamente en background para todas las orgs.
func (uc *Usecases) RunWatcherLoop(ctx context.Context, interval time.Duration, batchSize int) {
	worker.RunPeriodic(ctx, interval, "watcher-loop", func(tickCtx context.Context) {
		orgIDs, err := uc.repo.ListEnabledOrgIDs(tickCtx)
		if err != nil {
			slog.Error("watcher loop: list org ids failed", "error", err)
			return
		}
		for _, orgID := range orgIDs {
			if err := uc.RunAllEnabled(tickCtx, orgID); err != nil {
				slog.Error("watcher loop: run org failed", "org_id", orgID, "error", err)
			}
		}
	})
}

// RunPendingProposalSyncLoop reconcilia periódicamente proposals que quedaron
// esperando decisión final en Nexus.
func (uc *Usecases) RunPendingProposalSyncLoop(ctx context.Context, interval time.Duration, batchSize int) {
	worker.RunPeriodic(ctx, interval, "watcher-proposal-sync-loop", func(tickCtx context.Context) {
		orgIDs, err := uc.repo.ListEnabledOrgIDs(tickCtx)
		if err != nil {
			slog.Error("watcher proposal sync: list org ids failed", "error", err)
			return
		}
		for _, orgID := range orgIDs {
			uc.SyncPendingProposals(tickCtx, orgID, batchSize)
		}
	})
}

// SyncPendingProposals reconcilia propuestas que quedaron en require_approval:
// pollea Nexus por su decisión final y, si fue aprobada, gatilla la ejecución
// de la acción que originalmente había propuesto el watcher. Si fue rechazada,
// marca skipped. C14 — antes solo loguea "execution not implemented".
func (uc *Usecases) SyncPendingProposals(ctx context.Context, orgID string, limit int) {
	proposals, err := uc.repo.PendingProposals(ctx, orgID)
	if err != nil {
		slog.Error("sync pending proposals failed", "error", err)
		return
	}
	for i, p := range proposals {
		if i >= limit {
			break
		}
		if p.NexusRequestID == nil {
			continue
		}
		summary, statusCode, err := uc.nexus.GetRequest(ctx, p.NexusRequestID.String())
		if err != nil || statusCode == 404 {
			continue
		}
		status := summary.Status
		if status != "approved" && status != "allowed" && status != "rejected" && status != "denied" {
			continue
		}

		decision := summary.Decision
		p.NexusDecision = &decision
		now := time.Now().UTC()
		p.ResolvedAt = &now

		if status == "rejected" || status == "denied" {
			p.ExecutionStatus = domain.ProposalSkipped
			if err := uc.repo.UpdateProposal(ctx, p); err != nil {
				slog.Error("sync update proposal failed", "proposal_id", p.ID, "error", err)
			}
			continue
		}

		// approved/allowed: reconstituir contexto y ejecutar.
		w, gErr := uc.repo.GetWatcher(ctx, p.WatcherID)
		if gErr != nil {
			slog.Error("sync get watcher failed", "proposal_id", p.ID, "watcher_id", p.WatcherID, "error", gErr)
			p.ExecutionStatus = domain.ProposalFailed
			p.ExecutionResult = marshalSyncErrorResult("get_watcher_failed", gErr)
			if err := uc.repo.UpdateProposal(ctx, p); err != nil {
				slog.Error("sync update proposal failed", "proposal_id", p.ID, "error", err)
			}
			continue
		}

		item, iErr := itemFromProposalParams(p.Params)
		if iErr != nil {
			slog.Error("sync rebuild item failed", "proposal_id", p.ID, "error", iErr)
			p.ExecutionStatus = domain.ProposalFailed
			p.ExecutionResult = marshalSyncErrorResult("rebuild_item_failed", iErr)
			if err := uc.repo.UpdateProposal(ctx, p); err != nil {
				slog.Error("sync update proposal failed", "proposal_id", p.ID, "error", err)
			}
			continue
		}

		execSpec, _, _, specErr := uc.buildWatcherExecutionSpec(ctx, w, item, p.ID, p.NexusRequestID)
		if specErr != nil {
			slog.Error("sync build connector intent failed", "proposal_id", p.ID, "error", specErr)
			p.ExecutionStatus = domain.ProposalFailed
			p.ExecutionResult = marshalSyncErrorResult("build_connector_intent_failed", specErr)
		} else if execResult, execErr := uc.executeAction(ctx, execSpec); execErr != nil {
			slog.Error("sync execute approved proposal failed", "proposal_id", p.ID, "error", execErr)
			p.ExecutionStatus = domain.ProposalFailed
			p.ExecutionResult = marshalSyncErrorResult("execution_failed", execErr)
			uc.reportExecutionToNexus(ctx, p.NexusRequestID, execResult, false, execErr.Error())
		} else {
			p.ExecutionStatus = domain.ProposalExecuted
			p.ExecutionResult = watcherExecutionResultJSON(execResult, "sync_loop")
			uc.reportExecutionToNexus(ctx, p.NexusRequestID, execResult, true, "")
		}
		if err := uc.repo.UpdateProposal(ctx, p); err != nil {
			slog.Error("sync update proposal failed", "proposal_id", p.ID, "error", err)
		}
	}
}

// itemFromProposalParams reconstruye el PymesItem original a partir del JSON
// que el watcher persistió en proposal.Params al crear la propuesta. Si el
// schema no coincide, devuelve error para que la sync lo marque como failed
// con un motivo claro en vez de ejecutar con datos parciales.
func itemFromProposalParams(params json.RawMessage) (domain.PymesItem, error) {
	if len(params) == 0 {
		return domain.PymesItem{}, fmt.Errorf("proposal params empty")
	}
	var raw struct {
		ItemID   string `json:"item_id"`
		ItemType string `json:"item_type"`
		ItemName string `json:"item_name"`
		Phone    string `json:"phone"`
		PartyID  string `json:"party_id"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return domain.PymesItem{}, fmt.Errorf("unmarshal proposal params: %w", err)
	}
	if raw.ItemID == "" {
		return domain.PymesItem{}, fmt.Errorf("proposal params missing item_id")
	}
	return domain.PymesItem{
		ID:      raw.ItemID,
		Type:    raw.ItemType,
		Name:    raw.ItemName,
		Phone:   raw.Phone,
		PartyID: raw.PartyID,
	}, nil
}

func marshalSyncErrorResult(reason string, err error) json.RawMessage {
	return marshalOrEmpty("sync_error_result", map[string]string{
		"status": "failed",
		"reason": reason,
		"error":  err.Error(),
	})
}

// marshalOrEmpty serializa v y devuelve "{}" loguenado el error si falla.
func marshalOrEmpty(label string, v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("watchers marshal payload failed", "label", label, "error", err)
		return json.RawMessage(`{}`)
	}
	return b
}
