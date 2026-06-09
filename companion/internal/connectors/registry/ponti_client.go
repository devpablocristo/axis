package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// DiscoverManifest llama GET /api/v1/capabilities y devuelve el manifest
// canónico que publica Ponti. Si Ponti expone múltiples paquetes, filtra
// por ID == "ponti.insights" (el único que Companion consume hoy).
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
	for _, m := range resp.Items {
		if m.ID == "ponti.insights" {
			return m, nil
		}
	}
	return ai.CapabilityManifest{}, fmt.Errorf("ponti.insights manifest not present in capabilities response")
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
