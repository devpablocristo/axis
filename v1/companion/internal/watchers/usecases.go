// Package watchers implementa la observación proactiva del estado del negocio.
package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/jobs"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/platform/concurrency/go/worker"
	"github.com/devpablocristo/platform/errors/go/domainerr"
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

type ProductInstallationGuard interface {
	RequireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error
}

// Usecases contiene la lógica de negocio del módulo watchers.
type Usecases struct {
	repo              Repository
	nexus             NexusGateway
	notifier          ChatNotifier // nil = sin notificaciones al chat
	jobQueue          jobs.Repository
	installationGuard ProductInstallationGuard
	rateLimiter       productlimits.Limiter
}

// NewUsecases crea los usecases del módulo watchers.
func NewUsecases(repo Repository, nexus NexusGateway) *Usecases {
	return &Usecases{repo: repo, nexus: nexus}
}

// SetNotifier inyecta el notificador de chat. Opcional.
func (uc *Usecases) SetNotifier(n ChatNotifier) {
	uc.notifier = n
}

func (uc *Usecases) SetJobQueue(queue jobs.Repository) {
	uc.jobQueue = queue
}

func (uc *Usecases) SetProductInstallationGuard(guard ProductInstallationGuard) {
	uc.installationGuard = guard
}

func (uc *Usecases) SetRateLimiter(limiter productlimits.Limiter) {
	uc.rateLimiter = limiter
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

// RunWatcher ejecuta un watcher: consulta una capability, crea propuestas y
// evalua con Nexus. No ejecuta side effects externos desde un adapter
// generico; cualquier integracion futura debe vivir como outbound adapter
// especifico del dominio que corresponda.
func (uc *Usecases) RunWatcher(ctx context.Context, watcherID uuid.UUID) (*domain.WatcherResult, error) {
	w, err := uc.repo.GetWatcher(ctx, watcherID)
	if err != nil {
		return nil, fmt.Errorf("get watcher: %w", err)
	}
	if !w.Enabled {
		return nil, ErrWatcherDisabled
	}

	items, config, err := uc.queryProductCapability(ctx, w)
	if err != nil {
		slog.Error("watcher query capability failed", "watcher_id", w.ID, "error", err)
		return nil, fmt.Errorf("query product capability: %w", err)
	}

	result := &domain.WatcherResult{Found: len(items)}

	for _, item := range items {
		proposal, err := uc.processItem(ctx, w, config, item)
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

func (uc *Usecases) queryProductCapability(ctx context.Context, w domain.Watcher) ([]domain.WatcherItem, domain.CapabilityWatcherConfig, error) {
	config, err := resolveWatcherCapabilityConfig(w)
	if err != nil {
		return nil, config, err
	}
	if err := uc.requireActiveInstallation(ctx, w.OrgID, config.ProductSurface, "watcher_query"); err != nil {
		return nil, config, err
	}
	if err := productlimits.Enforce(ctx, uc.rateLimiter, productlimits.Key{
		OrgID:          w.OrgID,
		ProductSurface: config.ProductSurface,
		Area:           productlimits.AreaWatcher,
	}, productlimits.DefaultLimit(productlimits.AreaWatcher)); err != nil {
		return nil, config, err
	}
	result, err := uc.queryCapability(ctx, w, config)
	if err != nil {
		return nil, config, err
	}
	items, err := extractWatcherItems(result, config)
	if err != nil {
		return nil, config, err
	}
	return items, config, nil
}

func resolveWatcherCapabilityConfig(w domain.Watcher) (domain.CapabilityWatcherConfig, error) {
	if w.WatcherType == domain.WatcherCapability {
		var cfg domain.CapabilityWatcherConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return cfg, fmt.Errorf("parse capability watcher config: %w", err)
		}
		cfg.ProductSurface = strings.TrimSpace(cfg.ProductSurface)
		cfg.QueryOperation = strings.TrimSpace(cfg.QueryOperation)
		cfg.ActionOperation = strings.TrimSpace(cfg.ActionOperation)
		cfg.ActionType = strings.TrimSpace(cfg.ActionType)
		if cfg.ProductSurface == "" || cfg.QueryOperation == "" || cfg.ActionType == "" {
			return cfg, fmt.Errorf("product_surface, query_operation and action_type are required")
		}
		if !cfg.ProposalOnly && cfg.ActionOperation == "" {
			return cfg, fmt.Errorf("action_operation is required unless proposal_only is true")
		}
		return cfg, nil
	}
	return pymesCompatWatcherConfig(w)
}

func pymesCompatWatcherConfig(w domain.Watcher) (domain.CapabilityWatcherConfig, error) {
	cfg := domain.CapabilityWatcherConfig{
		ProductSurface:  "pymes",
		ResultItemsPath: "",
		ActionOperation: "pymes.send_whatsapp_text",
		ActionType:      actionTypeForWatcher(w.WatcherType),
		ActionPayloadTemplate: map[string]any{
			"org_id":   "${org_id}",
			"party_id": "${party_id}",
			"body":     "${watcher_message}",
		},
	}
	switch w.WatcherType {
	case domain.WatcherStaleWorkOrders:
		var compat domain.StaleWorkOrdersConfig
		if err := json.Unmarshal(w.Config, &compat); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		if compat.ThresholdDays <= 0 {
			compat.ThresholdDays = 3
		}
		cfg.QueryOperation = "pymes.get_work_orders"
		cfg.QueryPayload = map[string]any{"threshold_days": compat.ThresholdDays}
	case domain.WatcherUnconfirmedAppointments:
		var compat domain.UnconfirmedAppointmentsConfig
		if err := json.Unmarshal(w.Config, &compat); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		if compat.HoursBeforeAppointment <= 0 {
			compat.HoursBeforeAppointment = 24
		}
		cfg.QueryOperation = "pymes.get_appointments"
		cfg.QueryPayload = map[string]any{"hours_before_appointment": compat.HoursBeforeAppointment}
	case domain.WatcherLowStock:
		var compat domain.LowStockConfig
		if err := json.Unmarshal(w.Config, &compat); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		if compat.ThresholdUnits <= 0 {
			compat.ThresholdUnits = 5
		}
		cfg.QueryOperation = "pymes.get_low_stock"
		cfg.QueryPayload = map[string]any{"threshold_units": compat.ThresholdUnits}
	case domain.WatcherInactiveCustomers:
		var compat domain.InactiveCustomersConfig
		if err := json.Unmarshal(w.Config, &compat); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		if compat.ThresholdMonths <= 0 {
			compat.ThresholdMonths = 6
		}
		cfg.QueryOperation = "pymes.get_customers"
		cfg.QueryPayload = map[string]any{"threshold_months": compat.ThresholdMonths}
	case domain.WatcherRevenueDrop:
		var compat domain.RevenueDropConfig
		if err := json.Unmarshal(w.Config, &compat); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		if compat.ThresholdPercent <= 0 {
			compat.ThresholdPercent = 20
		}
		cfg.QueryOperation = "pymes.get_revenue_comparison"
		cfg.Condition = domain.WatcherCondition{Path: "drop_percent", Operator: "gte", Value: compat.ThresholdPercent}
	default:
		return cfg, fmt.Errorf("unknown watcher type: %s", w.WatcherType)
	}
	return cfg, nil
}

func (uc *Usecases) queryCapability(context.Context, domain.Watcher, domain.CapabilityWatcherConfig) (json.RawMessage, error) {
	return json.RawMessage(`[]`), nil
}

func extractWatcherItems(raw json.RawMessage, config domain.CapabilityWatcherConfig) ([]domain.WatcherItem, error) {
	var root any
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse capability result: %w", err)
	}
	selected, ok := valueAtPath(root, config.ResultItemsPath)
	if !ok {
		return nil, nil
	}
	values := valuesAsSlice(selected)
	items := make([]domain.WatcherItem, 0, len(values))
	for i, value := range values {
		if !conditionMatches(value, config.Condition) {
			continue
		}
		items = append(items, watcherItemFromValue(value, i))
	}
	return items, nil
}

func valuesAsSlice(value any) []any {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		return v
	default:
		return []any{v}
	}
}

func watcherItemFromValue(value any, index int) domain.WatcherItem {
	raw, _ := json.Marshal(value)
	item := domain.WatcherItem{Metadata: raw}
	m, _ := value.(map[string]any)
	item.ID = firstMapString(m, "id", "item_id", "subject_id", "external_id")
	if item.ID == "" {
		item.ID = fmt.Sprintf("item-%d", index+1)
	}
	item.Type = firstMapString(m, "type", "item_type", "subject_type", "fact_type")
	if item.Type == "" {
		item.Type = "item"
	}
	item.Name = firstMapString(m, "name", "title", "message", "label")
	if item.Name == "" {
		item.Name = item.ID
	}
	item.Status = firstMapString(m, "status")
	item.Phone = firstMapString(m, "phone", "phone_number")
	item.PartyID = firstMapString(m, "party_id", "customer_id", "contact_id")
	item.UpdatedAt = firstMapString(m, "updated_at")
	return item
}

func conditionMatches(value any, condition domain.WatcherCondition) bool {
	operator := strings.ToLower(strings.TrimSpace(condition.Operator))
	if operator == "" {
		return true
	}
	left, exists := valueAtPath(value, condition.Path)
	switch operator {
	case "exists":
		return exists
	case "not_exists":
		return !exists
	case "non_empty":
		return exists && strings.TrimSpace(fmt.Sprint(left)) != ""
	case "eq":
		return fmt.Sprint(left) == fmt.Sprint(condition.Value)
	case "ne":
		return fmt.Sprint(left) != fmt.Sprint(condition.Value)
	case "gt", "gte", "lt", "lte":
		lv, lok := numericValue(left)
		rv, rok := numericValue(condition.Value)
		if !lok || !rok {
			return false
		}
		switch operator {
		case "gt":
			return lv > rv
		case "gte":
			return lv >= rv
		case "lt":
			return lv < rv
		case "lte":
			return lv <= rv
		}
	default:
		return false
	}
	return false
}

func (uc *Usecases) processItem(ctx context.Context, w domain.Watcher, config domain.CapabilityWatcherConfig, item domain.WatcherItem) (domain.Proposal, error) {
	actionType := config.ActionType
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
	if config.ProposalOnly {
		proposal.ExecutionStatus = domain.ProposalPending
		if err := uc.repo.UpdateProposal(ctx, proposal); err != nil {
			slog.Error("watcher update proposal-only proposal failed", "proposal_id", proposal.ID, "error", err)
		}
		return proposal, nil
	}

	binding, err := buildWatcherActionBinding(w, config, item, proposal.ID)
	if err != nil {
		now := time.Now().UTC()
		proposal.ExecutionStatus = domain.ProposalFailed
		proposal.ResolvedAt = &now
		proposal.ExecutionResult = marshalSyncErrorResult("build_action_intent_failed", err)
		_ = uc.repo.UpdateProposal(ctx, proposal)
		return proposal, fmt.Errorf("build action intent: %w", err)
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
	}
	nexusResp, err := uc.nexus.SubmitRequest(ctx, idempotencyKey, nexusclient.SubmitRequestBody{
		RequesterType:  "service",
		RequesterID:    identityctx.CompanionPrincipal,
		RequesterName:  "Companion Watcher",
		ActionType:     "companion.propose",
		TargetSystem:   fmt.Sprint(binding["target_system"]),
		TargetResource: fmt.Sprint(binding["target_resource"]),
		ActionBinding:  binding,
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
	}

	decision := nexusResp.Decision
	proposal.NexusDecision = &decision

	switch {
	case decision == "allowed" || decision == "allow" || decision == "approved":
		now := time.Now().UTC()
		proposal.ResolvedAt = &now
		proposal.ExecutionStatus = domain.ProposalSkipped
		proposal.ExecutionResult = marshalOrEmpty("watcher_no_outbound_adapter_result", map[string]string{
			"status": "skipped",
			"reason": "no_outbound_adapter_configured",
		})

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

func buildWatcherActionBinding(w domain.Watcher, config domain.CapabilityWatcherConfig, item domain.WatcherItem, proposalID uuid.UUID) (map[string]any, error) {
	payloadMap := renderActionPayloadTemplate(config.ActionPayloadTemplate, w, item)
	if _, err := json.Marshal(payloadMap); err != nil {
		return nil, fmt.Errorf("marshal watcher action payload: %w", err)
	}
	return map[string]any{
		"schema_version":  "watcher_action.v1",
		"org_id":          w.OrgID,
		"watcher_id":      w.ID.String(),
		"proposal_id":     proposalID.String(),
		"product_surface": config.ProductSurface,
		"capability_id":   config.ActionOperation,
		"operation":       config.ActionOperation,
		"target_system":   "axis",
		"target_resource": item.ID,
		"action_type":     config.ActionType,
		"payload":         payloadMap,
	}, nil
}

func (uc *Usecases) requireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error {
	if uc.installationGuard == nil {
		return nil
	}
	if err := uc.installationGuard.RequireActiveInstallation(ctx, orgID, productSurface, reason); err != nil {
		if errors.Is(err, products.ErrValidation) {
			return domainerr.Validation(err.Error())
		}
		return domainerr.Forbidden(err.Error())
	}
	return nil
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

func renderActionPayloadTemplate(template map[string]any, w domain.Watcher, item domain.WatcherItem) map[string]any {
	if len(template) == 0 {
		return map[string]any{
			"org_id":   w.OrgID,
			"party_id": item.PartyID,
			"body":     watcherMessage(w.WatcherType, item),
		}
	}
	out := make(map[string]any, len(template)+1)
	for key, value := range template {
		out[key] = renderTemplateValue(value, w, item)
	}
	if _, ok := out["org_id"]; !ok {
		out["org_id"] = w.OrgID
	}
	return out
}

func renderTemplateValue(value any, w domain.Watcher, item domain.WatcherItem) any {
	switch v := value.(type) {
	case string:
		replacements := map[string]string{
			"${org_id}":          w.OrgID,
			"${watcher_id}":      w.ID.String(),
			"${watcher_name}":    w.Name,
			"${watcher_message}": watcherMessage(w.WatcherType, item),
			"${id}":              item.ID,
			"${item_id}":         item.ID,
			"${type}":            item.Type,
			"${item_type}":       item.Type,
			"${name}":            item.Name,
			"${item_name}":       item.Name,
			"${status}":          item.Status,
			"${phone}":           item.Phone,
			"${party_id}":        item.PartyID,
			"${updated_at}":      item.UpdatedAt,
			"${item_json}":       string(item.Metadata),
		}
		for token, replacement := range replacements {
			v = strings.ReplaceAll(v, token, replacement)
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, itemValue := range v {
			out[key] = renderTemplateValue(itemValue, w, item)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, itemValue := range v {
			out = append(out, renderTemplateValue(itemValue, w, item))
		}
		return out
	default:
		return value
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func valueAtPath(value any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" || path == "$" || path == "." {
		return value, true
	}
	current := value
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func firstMapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if m == nil {
			return ""
		}
		if value, ok := m[key]; ok {
			if out := strings.TrimSpace(fmt.Sprint(value)); out != "" && out != "<nil>" {
				return out
			}
		}
	}
	return ""
}

func numericValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		return 0, false
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
		if uc.jobQueue != nil {
			count, err := uc.EnqueueWatcherRuns(tickCtx, batchSize)
			if err != nil {
				slog.Error("watcher loop: enqueue watcher jobs failed", "error", err)
				return
			}
			slog.Info("watcher loop: watcher jobs enqueued", "count", count)
			return
		}
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
		if uc.jobQueue != nil {
			count, err := uc.EnqueuePendingProposalSyncs(tickCtx, batchSize)
			if err != nil {
				slog.Error("watcher proposal sync: enqueue sync jobs failed", "error", err)
				return
			}
			slog.Info("watcher proposal sync: jobs enqueued", "count", count)
			return
		}
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

const (
	JobKindWatcherRun          = "watcher.run"
	JobKindWatcherProposalSync = "watcher.proposals.sync"
)

type watcherRunJobPayload struct {
	WatcherID      string `json:"watcher_id"`
	ProductSurface string `json:"product_surface,omitempty"`
}

type watcherProposalSyncJobPayload struct {
	OrgID string `json:"org_id"`
	Limit int    `json:"limit"`
}

func (uc *Usecases) EnqueueWatcherRuns(ctx context.Context, batchSize int) (int, error) {
	if uc.jobQueue == nil {
		return 0, fmt.Errorf("job queue not configured")
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	orgIDs, err := uc.repo.ListEnabledOrgIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list enabled org ids: %w", err)
	}
	count := 0
	for _, orgID := range orgIDs {
		if count >= batchSize {
			break
		}
		watchers, err := uc.repo.ListWatchers(ctx, orgID)
		if err != nil {
			return count, fmt.Errorf("list watchers for org %s: %w", orgID, err)
		}
		for _, w := range watchers {
			if count >= batchSize {
				break
			}
			if !w.Enabled {
				continue
			}
			productSurface := jobs.DefaultProductSurface
			if cfg, cfgErr := resolveWatcherCapabilityConfig(w); cfgErr == nil && strings.TrimSpace(cfg.ProductSurface) != "" {
				productSurface = strings.TrimSpace(cfg.ProductSurface)
			}
			payload, err := json.Marshal(watcherRunJobPayload{WatcherID: w.ID.String(), ProductSurface: productSurface})
			if err != nil {
				return count, fmt.Errorf("marshal watcher job payload: %w", err)
			}
			_, _, err = uc.jobQueue.Enqueue(ctx, jobs.EnqueueInput{
				OrgID:          w.OrgID,
				ProductSurface: productSurface,
				Kind:           JobKindWatcherRun,
				ShardKey:       w.OrgID + ":" + productSurface + ":" + w.ID.String(),
				DedupeKey:      JobKindWatcherRun + ":" + w.ID.String(),
				Payload:        payload,
				MaxAttempts:    3,
				Timeout:        5 * time.Minute,
			})
			if err != nil {
				return count, fmt.Errorf("enqueue watcher job: %w", err)
			}
			count++
		}
	}
	return count, nil
}

func (uc *Usecases) EnqueuePendingProposalSyncs(ctx context.Context, batchSize int) (int, error) {
	if uc.jobQueue == nil {
		return 0, fmt.Errorf("job queue not configured")
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	orgIDs, err := uc.repo.ListEnabledOrgIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list enabled org ids: %w", err)
	}
	count := 0
	for _, orgID := range orgIDs {
		payload, err := json.Marshal(watcherProposalSyncJobPayload{OrgID: orgID, Limit: batchSize})
		if err != nil {
			return count, fmt.Errorf("marshal watcher proposal sync payload: %w", err)
		}
		_, _, err = uc.jobQueue.Enqueue(ctx, jobs.EnqueueInput{
			OrgID:       orgID,
			Kind:        JobKindWatcherProposalSync,
			ShardKey:    orgID,
			DedupeKey:   JobKindWatcherProposalSync + ":" + orgID,
			Payload:     payload,
			MaxAttempts: 3,
			Timeout:     2 * time.Minute,
		})
		if err != nil {
			return count, fmt.Errorf("enqueue watcher proposal sync job: %w", err)
		}
		count++
	}
	return count, nil
}

func (uc *Usecases) RegisterJobHandlers(worker *jobs.Worker) {
	if worker == nil {
		return
	}
	worker.Register(JobKindWatcherRun, uc.handleWatcherRunJob)
	worker.Register(JobKindWatcherProposalSync, uc.handleWatcherProposalSyncJob)
}

func (uc *Usecases) handleWatcherRunJob(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload watcherRunJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return json.RawMessage(`{"reason":"invalid_payload"}`), jobs.Permanent(fmt.Errorf("parse watcher run job payload: %w", err))
	}
	watcherID, err := uuid.Parse(strings.TrimSpace(payload.WatcherID))
	if err != nil || watcherID == uuid.Nil {
		return json.RawMessage(`{"reason":"invalid_watcher_id"}`), jobs.Permanent(fmt.Errorf("invalid watcher_id %q", payload.WatcherID))
	}
	w, err := uc.repo.GetWatcher(ctx, watcherID)
	if err != nil {
		return json.RawMessage(`{"reason":"watcher_not_found"}`), jobs.Permanent(fmt.Errorf("get watcher: %w", err))
	}
	config, err := resolveWatcherCapabilityConfig(w)
	if err != nil {
		return json.RawMessage(`{"reason":"invalid_watcher_config"}`), jobs.Permanent(err)
	}
	if err := requireWatcherJobProductScope(job, payload.ProductSurface, config.ProductSurface); err != nil {
		return json.RawMessage(`{"reason":"product_surface_mismatch"}`), err
	}
	result, err := uc.RunWatcher(ctx, watcherID)
	if err != nil {
		if err == ErrWatcherDisabled {
			return json.RawMessage(`{"reason":"watcher_disabled"}`), jobs.Permanent(err)
		}
		return json.RawMessage(`{"reason":"run_watcher_failed"}`), err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return json.RawMessage(`{"reason":"marshal_result_failed"}`), err
	}
	return raw, nil
}

func requireWatcherJobProductScope(job jobs.Job, payloadProductSurface, configProductSurface string) error {
	expected := strings.TrimSpace(strings.ToLower(configProductSurface))
	if expected == "" {
		return jobs.Permanent(fmt.Errorf("watcher config product_surface is required"))
	}
	if payloadProductSurface = strings.TrimSpace(strings.ToLower(payloadProductSurface)); payloadProductSurface != "" && payloadProductSurface != expected {
		return jobs.Permanent(fmt.Errorf("watcher payload product_surface mismatch: payload=%s config=%s", payloadProductSurface, expected))
	}
	jobProductSurface := strings.TrimSpace(strings.ToLower(job.ProductSurface))
	if jobProductSurface == "" || jobProductSurface != expected {
		return jobs.Permanent(fmt.Errorf("watcher job product_surface mismatch: job=%s config=%s", jobProductSurface, expected))
	}
	return nil
}

func (uc *Usecases) handleWatcherProposalSyncJob(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload watcherProposalSyncJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return json.RawMessage(`{"reason":"invalid_payload"}`), jobs.Permanent(fmt.Errorf("parse watcher sync job payload: %w", err))
	}
	orgID := strings.TrimSpace(payload.OrgID)
	if orgID == "" {
		orgID = job.OrgID
	}
	if orgID == "" {
		return json.RawMessage(`{"reason":"missing_org_id"}`), jobs.Permanent(fmt.Errorf("org_id is required"))
	}
	limit := payload.Limit
	if limit <= 0 {
		limit = 50
	}
	uc.SyncPendingProposals(ctx, orgID, limit)
	return json.RawMessage(`{"status":"synced"}`), nil
}

// SyncPendingProposals reconcilia propuestas que quedaron en require_approval:
// pollea Nexus por su decisión final y marca el resultado. No ejecuta acciones
// externas desde un executor generico.
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

		p.ExecutionStatus = domain.ProposalSkipped
		p.ExecutionResult = marshalOrEmpty("watcher_no_outbound_adapter_result", map[string]string{
			"status": "skipped",
			"reason": "no_outbound_adapter_configured",
		})
		if err := uc.repo.UpdateProposal(ctx, p); err != nil {
			slog.Error("sync update proposal failed", "proposal_id", p.ID, "error", err)
		}
	}
}

// itemFromProposalParams reconstruye el item original a partir del JSON que el
// watcher persistió en proposal.Params al crear la propuesta.
func itemFromProposalParams(params json.RawMessage) (domain.WatcherItem, error) {
	if len(params) == 0 {
		return domain.WatcherItem{}, fmt.Errorf("proposal params empty")
	}
	var raw struct {
		ItemID   string `json:"item_id"`
		ItemType string `json:"item_type"`
		ItemName string `json:"item_name"`
		Phone    string `json:"phone"`
		PartyID  string `json:"party_id"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return domain.WatcherItem{}, fmt.Errorf("unmarshal proposal params: %w", err)
	}
	if raw.ItemID == "" {
		return domain.WatcherItem{}, fmt.Errorf("proposal params missing item_id")
	}
	return domain.WatcherItem{
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
