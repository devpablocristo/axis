package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	ai "github.com/devpablocristo/platform/kernels/ai/go"
)

// PontiClient es el HTTP client mínimo para invocar capabilities read-only
// de Ponti. Espeja el patrón de internal/watchers/pymesclient pero queda en
// este package porque es exclusivo del connector y muy pequeño.
//
// Nota de auth: en fase 1 usa un API key estático (PONTI_API_KEY env var)
// como service account. Per la decisión D.1 del plan, la propagación de JWT
// del usuario originador (delegated_user) queda para fase 2 — requiere
// threadear el token desde el handler HTTP hasta acá vía context, cambio de
// scope mayor que no aporta a la prueba end-to-end de fase 1.
type PontiClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

type pontiWorkspacePayload struct {
	CustomerID *int64 `json:"customer_id,omitempty"`
	ProjectID  *int64 `json:"project_id,omitempty"`
	CampaignID *int64 `json:"campaign_id,omitempty"`
	FieldID    *int64 `json:"field_id,omitempty"`
}

type pontiOperationPayload struct {
	Limit           int                    `json:"limit"`
	IncludeResolved bool                   `json:"include_resolved"`
	InsightID       string                 `json:"insight_id"`
	CutoffDate      string                 `json:"cutoff_date"`
	Status          string                 `json:"status"`
	SupplyID        *int64                 `json:"supply_id,omitempty"`
	IsDigital       *bool                  `json:"is_digital,omitempty"`
	Mode            string                 `json:"mode"`
	CropID          *int64                 `json:"crop_id,omitempty"`
	ProjectID       *int64                 `json:"project_id,omitempty"`
	CustomerID      *int64                 `json:"customer_id,omitempty"`
	CampaignID      *int64                 `json:"campaign_id,omitempty"`
	FieldID         *int64                 `json:"field_id,omitempty"`
	Workspace       pontiWorkspacePayload  `json:"workspace,omitempty"`
	Extra           map[string]interface{} `json:"-"`
}

// NewPontiClient construye el client. Si BaseURL queda vacío el connector
// nunca se registra (ver wire/setup.go).
func NewPontiClient(baseURL, apiKey string) *PontiClient {
	return &PontiClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *PontiClient) doGet(ctx context.Context, path string, orgID string, out any) error {
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if orgID != "" {
		// Ponti extrae org_id del JWT en producción. En pruebas/dev el header
		// X-Tenant-Id se usa como override del middleware de auth.
		req.Header.Set("X-Tenant-Id", orgID)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ponti http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ponti http %d: %s", resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *PontiClient) doPost(ctx context.Context, path string, orgID string, payload json.RawMessage, out any) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if orgID != "" {
		// Ponti extrae org_id del JWT en producción. En pruebas/dev el header
		// X-Tenant-Id se usa como override del middleware de auth.
		req.Header.Set("X-Tenant-Id", orgID)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ponti http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ponti http %d: %s", resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// DiscoverManifest llama GET /api/v1/capabilities y devuelve un manifest
// canónico fusionado para product=ponti. Ponti publica varios paquetes
// (insights, operational, actions); Companion los consume como un único
// catálogo de tools del connector.
//
// La metadata de capabilities no es tenant-scoped (mismas tools para todos
// los tenants), pero el endpoint de Ponti requiere auth/tenant context.
// Mandamos un X-Tenant-Id sentinel ("companion-discovery") solo para
// pasar el middleware; Ponti devuelve la lista completa igual.
func (c *PontiClient) DiscoverManifest(ctx context.Context) (ai.CapabilityManifest, error) {
	var resp struct {
		Items []ai.CapabilityManifest `json:"items"`
	}
	if err := c.doGet(ctx, "/api/v1/capabilities", "companion-discovery", &resp); err != nil {
		return ai.CapabilityManifest{}, fmt.Errorf("ponti capabilities: %w", err)
	}
	var merged ai.CapabilityManifest
	for _, m := range resp.Items {
		if m.Product != "ponti" {
			continue
		}
		if merged.ID == "" {
			merged = m
			merged.ID = "ponti"
			merged.Name = "Ponti"
			merged.Description = "Merged Ponti product capabilities discovered from Ponti backend."
			merged.Tools = nil
		}
		merged.Tools = append(merged.Tools, m.Tools...)
		merged.Agents = append(merged.Agents, m.Agents...)
	}
	if merged.ID != "" && len(merged.Tools) > 0 {
		return merged, nil
	}
	for _, m := range resp.Items {
		if m.ID == "ponti.insights" && len(m.Tools) > 0 {
			return m, nil
		}
	}
	return ai.CapabilityManifest{}, fmt.Errorf("ponti product capabilities not present in capabilities response")
}

// ListInsights llama GET /api/v1/insights del tenant.
func (c *PontiClient) ListInsights(ctx context.Context, orgID string, limit int, includeResolved bool) (json.RawMessage, error) {
	path := "/api/v1/insights"
	q := []string{}
	if limit > 0 {
		q = append(q, fmt.Sprintf("limit=%d", limit))
	}
	if includeResolved {
		q = append(q, "include_resolved=true")
	}
	if len(q) > 0 {
		path += "?" + strings.Join(q, "&")
	}
	var raw json.RawMessage
	if err := c.doGet(ctx, path, orgID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// SummaryInsights llama GET /api/v1/insights/summary del tenant.
func (c *PontiClient) SummaryInsights(ctx context.Context, orgID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doGet(ctx, "/api/v1/insights/summary", orgID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ExplainInsight llama GET /api/v1/insights/{id}/explain del tenant.
func (c *PontiClient) ExplainInsight(ctx context.Context, orgID, insightID string) (json.RawMessage, error) {
	if strings.TrimSpace(insightID) == "" {
		return nil, fmt.Errorf("insight_id is required")
	}
	var raw json.RawMessage
	if err := c.doGet(ctx, "/api/v1/insights/"+insightID+"/explain", orgID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) PrepareInsightResolve(ctx context.Context, orgID string, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doPost(ctx, "/api/v1/ai/actions/insight-resolve/prepare", orgID, payload, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) PrepareWorkOrderDraft(ctx context.Context, orgID string, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doPost(ctx, "/api/v1/ai/actions/workorder-draft/prepare", orgID, payload, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) PrepareStockAdjustment(ctx context.Context, orgID string, payload json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doPost(ctx, "/api/v1/ai/actions/stock-adjustment/prepare", orgID, payload, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) DashboardSummary(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	return c.doGetRaw(ctx, "/api/v1/dashboard?"+workspaceQuery(payload).Encode(), orgID)
}

func (c *PontiClient) StockSummary(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	projectID := payload.effectiveProjectID()
	if projectID <= 0 {
		return nil, fmt.Errorf("workspace.project_id is required")
	}
	q := workspaceQuery(payload)
	if strings.TrimSpace(payload.CutoffDate) != "" {
		q.Set("cutoff_date", strings.TrimSpace(payload.CutoffDate))
	}
	return c.doGetRaw(ctx, fmt.Sprintf("/api/v1/projects/%d/stocks/summary?%s", projectID, q.Encode()), orgID)
}

func (c *PontiClient) WorkOrdersList(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	q := workspaceQuery(payload)
	if status := strings.TrimSpace(payload.Status); status != "" {
		q.Set("status", status)
	}
	if payload.SupplyID != nil && *payload.SupplyID > 0 {
		q.Set("supply_id", fmt.Sprintf("%d", *payload.SupplyID))
	}
	if payload.IsDigital != nil {
		q.Set("is_digital", fmt.Sprintf("%t", *payload.IsDigital))
	}
	limit := payload.limitOrDefault(25)
	q.Set("page", "1")
	q.Set("per_page", fmt.Sprintf("%d", limit))
	return c.doGetRaw(ctx, "/api/v1/work-orders?"+q.Encode(), orgID)
}

func (c *PontiClient) WorkOrdersMetrics(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	q := workspaceQuery(payload)
	if status := strings.TrimSpace(payload.Status); status != "" {
		q.Set("status", status)
	}
	if payload.SupplyID != nil && *payload.SupplyID > 0 {
		q.Set("supply_id", fmt.Sprintf("%d", *payload.SupplyID))
	}
	if payload.IsDigital != nil {
		q.Set("is_digital", fmt.Sprintf("%t", *payload.IsDigital))
	}
	return c.doGetRaw(ctx, "/api/v1/work-orders/metrics?"+q.Encode(), orgID)
}

func (c *PontiClient) LotsSummary(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	q := workspaceQuery(payload)
	if payload.CropID != nil && *payload.CropID > 0 {
		q.Set("crop_id", fmt.Sprintf("%d", *payload.CropID))
	}
	q.Set("page", "1")
	q.Set("per_page", fmt.Sprintf("%d", payload.limitOrDefault(50)))
	return c.doGetRaw(ctx, "/api/v1/lots?"+q.Encode(), orgID)
}

func (c *PontiClient) SuppliesSummary(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	q := workspaceQuery(payload)
	if mode := strings.TrimSpace(payload.Mode); mode != "" {
		q.Set("mode", mode)
	}
	q.Set("page", "1")
	q.Set("per_page", fmt.Sprintf("%d", payload.limitOrDefault(50)))
	return c.doGetRaw(ctx, "/api/v1/supplies?"+q.Encode(), orgID)
}

func (c *PontiClient) DataIntegritySummary(ctx context.Context, orgID string, payload pontiOperationPayload) (json.RawMessage, error) {
	return c.doGetRaw(ctx, "/api/v1/data-integrity/summary?"+workspaceQuery(payload).Encode(), orgID)
}

func (c *PontiClient) ReportSummary(ctx context.Context, orgID, reportType string, payload pontiOperationPayload) (json.RawMessage, error) {
	reportType = strings.Trim(reportType, "/")
	if reportType == "" {
		return nil, fmt.Errorf("report type is required")
	}
	return c.doGetRaw(ctx, "/api/v1/reports/"+reportType+"?"+workspaceQuery(payload).Encode(), orgID)
}

func (c *PontiClient) CreateWorkOrderDraft(ctx context.Context, orgID string, payload json.RawMessage, nexusRequestID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doPostWithNexus(ctx, "/api/v1/work-order-drafts/digital", orgID, payload, nexusRequestID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) DraftInsightResolution(ctx context.Context, orgID string, payload json.RawMessage, nexusRequestID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doPostWithNexus(ctx, "/api/v1/ai/actions/insight-resolution/draft", orgID, payload, nexusRequestID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) DraftStockCount(ctx context.Context, orgID string, payload json.RawMessage, nexusRequestID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doPostWithNexus(ctx, "/api/v1/ai/actions/stock-count/draft", orgID, payload, nexusRequestID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) doGetRaw(ctx context.Context, path string, orgID string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doGet(ctx, path, orgID, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *PontiClient) doPostWithNexus(ctx context.Context, path string, orgID string, payload json.RawMessage, nexusRequestID string, out any) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if orgID != "" {
		req.Header.Set("X-Tenant-Id", orgID)
	}
	if strings.TrimSpace(nexusRequestID) != "" {
		req.Header.Set("X-Nexus-Request-ID", strings.TrimSpace(nexusRequestID))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ponti http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ponti http %d: %s", resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func workspaceQuery(payload pontiOperationPayload) url.Values {
	q := url.Values{}
	setInt64 := func(key string, value *int64) {
		if value != nil && *value > 0 {
			q.Set(key, fmt.Sprintf("%d", *value))
		}
	}
	setInt64("customer_id", firstInt64Ptr(payload.Workspace.CustomerID, payload.CustomerID))
	setInt64("project_id", firstInt64Ptr(payload.Workspace.ProjectID, payload.ProjectID))
	setInt64("campaign_id", firstInt64Ptr(payload.Workspace.CampaignID, payload.CampaignID))
	setInt64("field_id", firstInt64Ptr(payload.Workspace.FieldID, payload.FieldID))
	return q
}

func (p pontiOperationPayload) effectiveProjectID() int64 {
	if p.Workspace.ProjectID != nil && *p.Workspace.ProjectID > 0 {
		return *p.Workspace.ProjectID
	}
	if p.ProjectID != nil && *p.ProjectID > 0 {
		return *p.ProjectID
	}
	return 0
}

func (p pontiOperationPayload) limitOrDefault(defaultValue int) int {
	if p.Limit > 0 {
		if p.Limit > 200 {
			return 200
		}
		return p.Limit
	}
	return defaultValue
}

func firstInt64Ptr(values ...*int64) *int64 {
	for _, value := range values {
		if value != nil && *value > 0 {
			return value
		}
	}
	return nil
}
