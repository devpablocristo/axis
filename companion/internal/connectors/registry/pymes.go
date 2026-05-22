package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/watchers/pymesclient"
)

// PymesConnector conector a Pymes Core API.
type PymesConnector struct {
	client *pymesclient.Client
}

// NewPymesConnector crea un conector a Pymes.
func NewPymesConnector(client *pymesclient.Client) *PymesConnector {
	return &PymesConnector{client: client}
}

func (p *PymesConnector) ID() string   { return "pymes" }
func (p *PymesConnector) Kind() string { return "pymes" }

func (p *PymesConnector) Capabilities() []domain.Capability {
	return []domain.Capability{
		{
			Operation:             "pymes.send_whatsapp_text",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "medium",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "party_id", "body"},
			},
			EvidenceFields: []string{"sent", "external_ref", "party_id"},
		},
		{
			Operation:             "pymes.send_whatsapp_template",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "medium",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "party_id", "template_name"},
			},
			EvidenceFields: []string{"sent", "external_ref", "party_id", "template_name"},
		},
		{
			Operation:      "pymes.get_work_orders",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation:      "pymes.get_appointments",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation:      "pymes.get_low_stock",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation:      "pymes.get_customers",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation:      "pymes.get_revenue_comparison",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"current_month", "previous_month", "drop_percent"},
		},

		// -------------------------------------------------------------------
		// Migración pymes/ai → Companion (Sprint 1: capabilities ampliadas).
		// Naming dot-separated por convención del MIGRATION_INVENTORY.md.
		// -------------------------------------------------------------------

		{
			Operation:      "pymes.customers.search",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
				"properties": map[string]any{
					"org_id": map[string]any{"type": "string"},
					"query":  map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			EvidenceFields: []string{"items", "total", "has_more"},
		},
		{
			Operation:      "pymes.services.search",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
				"properties": map[string]any{
					"org_id": map[string]any{"type": "string"},
					"query":  map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			EvidenceFields: []string{"items", "total", "has_more"},
		},
		{
			Operation:      "pymes.inventory.search",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
				"properties": map[string]any{
					"org_id": map[string]any{"type": "string"},
					"query":  map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			EvidenceFields: []string{"items", "total", "has_more"},
		},
		{
			Operation:      "pymes.cashflow.summary",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
				"properties": map[string]any{
					"org_id": map[string]any{"type": "string"},
					"period": map[string]any{
						"type": "string",
						"enum": []string{"today", "week", "month", "quarter", "year"},
					},
				},
			},
			EvidenceFields: []string{"summary"},
		},
		{
			Operation:      "pymes.accounts.summary",
			Mode:           domain.CapabilityModeRead,
			ReadOnly:       true,
			RiskClass:      "low",
			RequiredScopes: []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"summary"},
		},
		{
			Operation:             "pymes.scheduling.book",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "medium",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			// Shape alineado con platform/features/scheduling/go CreateBookingRequest.
			// (POST /v1/scheduling/bookings). branch_id, service_id, start_at,
			// customer_name y customer_phone son requeridos por el backend.
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "branch_id", "service_id", "start_at", "customer_name", "customer_phone"},
				"properties": map[string]any{
					"org_id":         map[string]any{"type": "string"},
					"branch_id":      map[string]any{"type": "string"},
					"service_id":     map[string]any{"type": "string"},
					"start_at":       map[string]any{"type": "string", "format": "date-time"},
					"end_at":         map[string]any{"type": "string", "format": "date-time"},
					"customer_name":  map[string]any{"type": "string"},
					"customer_phone": map[string]any{"type": "string"},
					"customer_email": map[string]any{"type": "string"},
					"party_id":       map[string]any{"type": "string"},
					"resource_id":    map[string]any{"type": "string"},
					"notes":          map[string]any{"type": "string"},
				},
			},
			EvidenceFields: []string{"id", "start_at", "end_at", "service_id", "branch_id"},
		},
		{
			Operation:             "pymes.quotes.create",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "medium",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "party_id", "items"},
				"properties": map[string]any{
					"org_id":   map[string]any{"type": "string"},
					"party_id": map[string]any{"type": "string"},
					"items":    map[string]any{"type": "array", "minItems": 1},
					"notes":    map[string]any{"type": "string"},
				},
			},
			EvidenceFields: []string{"quote_id", "total", "party_id"},
		},
		{
			Operation:             "pymes.sales.create",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "high",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "party_id", "items"},
				"properties": map[string]any{
					"org_id":         map[string]any{"type": "string"},
					"party_id":       map[string]any{"type": "string"},
					"items":          map[string]any{"type": "array", "minItems": 1},
					"payment_method": map[string]any{"type": "string"},
				},
			},
			EvidenceFields: []string{"sale_id", "total", "party_id"},
		},
		{
			Operation:             "pymes.payments.link",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "high",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "sale_id", "amount"},
				"properties": map[string]any{
					"org_id":  map[string]any{"type": "string"},
					"sale_id": map[string]any{"type": "string"},
					"amount":  map[string]any{"type": "number", "exclusiveMinimum": 0},
					"method":  map[string]any{"type": "string"},
				},
			},
			EvidenceFields: []string{"payment_id", "sale_id", "amount"},
		},
		{
			Operation:             "pymes.procurement_requests.create",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RiskClass:             "medium",
			RequiresNexusApproval: true,
			RequiredScopes:        []string{"companion:connectors:execute"},
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "items"},
				"properties": map[string]any{
					"org_id":   map[string]any{"type": "string"},
					"items":    map[string]any{"type": "array", "minItems": 1},
					"priority": map[string]any{"type": "string"},
					"notes":    map[string]any{"type": "string"},
				},
			},
			EvidenceFields: []string{"request_id", "items_count"},
		},
	}
}

func (p *PymesConnector) Validate(spec domain.ExecutionSpec) error {
	if spec.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	return nil
}

func (p *PymesConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	start := time.Now()

	var params struct {
		OrgID           string            `json:"org_id"`
		PartyID         string            `json:"party_id"`
		Body            string            `json:"body"`
		TemplateName    string            `json:"template_name"`
		Params          map[string]string `json:"params"`
		ThresholdDays   int               `json:"threshold_days"`
		HoursBefore     int               `json:"hours_before_appointment"`
		ThresholdUnits  int               `json:"threshold_units"`
		ThresholdMonths int               `json:"threshold_months"`
		Query           string            `json:"query"`
		Limit           int               `json:"limit"`
		Period          string            `json:"period"`
	}
	if err := json.Unmarshal(spec.Payload, &params); err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("parse payload: %w", err)
	}

	var resultData any
	var execErr error

	switch spec.Operation {
	case "pymes.send_whatsapp_text":
		execErr = p.client.SendWhatsAppText(ctx, params.OrgID, params.PartyID, params.Body)
		resultData = map[string]string{"sent": "true"}

	case "pymes.send_whatsapp_template":
		execErr = p.client.SendWhatsAppTemplate(ctx, params.OrgID, params.PartyID, params.TemplateName, params.Params)
		resultData = map[string]string{"sent": "true"}

	case "pymes.get_work_orders":
		items, err := p.client.GetStaleWorkOrders(ctx, params.OrgID, params.ThresholdDays)
		execErr = err
		resultData = items

	case "pymes.get_appointments":
		hoursBefore := params.HoursBefore
		if hoursBefore <= 0 {
			hoursBefore = 24
		}
		items, err := p.client.GetUnconfirmedAppointments(ctx, params.OrgID, hoursBefore)
		execErr = err
		resultData = items

	case "pymes.get_low_stock":
		items, err := p.client.GetLowStockItems(ctx, params.OrgID, params.ThresholdUnits)
		execErr = err
		resultData = items

	case "pymes.get_customers":
		items, err := p.client.GetInactiveCustomers(ctx, params.OrgID, params.ThresholdMonths)
		execErr = err
		resultData = items

	case "pymes.get_revenue_comparison":
		comparison, err := p.client.GetRevenueComparison(ctx, params.OrgID)
		execErr = err
		resultData = comparison

	case "pymes.customers.search":
		paged, err := p.client.SearchCustomers(ctx, params.OrgID, params.Query, params.Limit)
		execErr = err
		resultData = paged

	case "pymes.services.search":
		paged, err := p.client.SearchServices(ctx, params.OrgID, params.Query, params.Limit)
		execErr = err
		resultData = paged

	case "pymes.inventory.search":
		paged, err := p.client.SearchInventory(ctx, params.OrgID, params.Query, params.Limit)
		execErr = err
		resultData = paged

	case "pymes.cashflow.summary":
		raw, err := p.client.GetCashflowSummary(ctx, params.OrgID, params.Period)
		execErr = err
		resultData = map[string]any{"summary": json.RawMessage(raw)}

	case "pymes.accounts.summary":
		raw, err := p.client.GetAccountsSummary(ctx, params.OrgID)
		execErr = err
		resultData = map[string]any{"summary": json.RawMessage(raw)}

	case "pymes.scheduling.book":
		raw, err := p.client.BookScheduling(ctx, params.OrgID, spec.Payload)
		execErr = err
		resultData = json.RawMessage(raw)

	case "pymes.quotes.create":
		raw, err := p.client.CreateQuote(ctx, params.OrgID, spec.Payload)
		execErr = err
		resultData = json.RawMessage(raw)

	case "pymes.sales.create":
		raw, err := p.client.CreateSale(ctx, params.OrgID, spec.Payload)
		execErr = err
		resultData = json.RawMessage(raw)

	case "pymes.payments.link":
		raw, err := p.client.LinkPayment(ctx, params.OrgID, spec.Payload)
		execErr = err
		resultData = json.RawMessage(raw)

	case "pymes.procurement_requests.create":
		raw, err := p.client.CreateProcurementRequest(ctx, params.OrgID, spec.Payload)
		execErr = err
		resultData = json.RawMessage(raw)

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

	resultJSON, mErr := json.Marshal(resultData)
	if mErr != nil {
		// Marshal de un map[string]any controlado: solo puede fallar si el
		// connector mete tipos no-marshalables (channels, funcs). Logueamos
		// y degradamos en lugar de bloquear el reportback de la ejecución.
		slog.Error("pymes connector marshal result", "operation", spec.Operation, "error", mErr)
		resultJSON = []byte(`{}`)
	}

	return domain.ExecutionResult{
		ID:             uuid.New(),
		ConnectorID:    spec.ConnectorID,
		OrgID:          spec.OrgID,
		ActorID:        spec.ActorID,
		Operation:      spec.Operation,
		Status:         status,
		ExternalRef:    fmt.Sprintf("pymes-%s", spec.Operation),
		Payload:        spec.Payload,
		ResultJSON:     json.RawMessage(resultJSON),
		ErrorMessage:   errMsg,
		Retryable:      execErr != nil,
		DurationMS:     duration,
		IdempotencyKey: spec.IdempotencyKey,
		TaskID:         spec.TaskID,
		NexusRequestID: spec.NexusRequestID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}
