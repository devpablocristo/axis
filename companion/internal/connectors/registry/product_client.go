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

	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/secrets"
)

// Envelope genérico capability_execution.v1. Es el contrato que cualquier
// producto puede implementar para recibir ejecuciones de capabilities desde
// Companion sin que Axis necesite código vertical por operación (a diferencia
// del switch hardcoded de PontiConnector).
const (
	ProductExecutionSchemaVersion = "capability_execution.v1"

	// ProductConnectorModeEnvelopeV1 es el valor de `config.connector_mode`
	// de una product installation que habilita el connector genérico.
	ProductConnectorModeEnvelopeV1 = "envelope.v1"

	productConnectorModeConfigKey  = "connector_mode"
	productDiscoveryPathConfigKey  = "discovery_path"
	productExecutePathConfigKey    = "execute_path"
	productRequiredScopesConfigKey = "required_scopes"

	defaultProductDiscoveryPath = "/api/v1/capabilities"
	defaultProductExecutePath   = "/api/v1/capability-executions"
)

// ProductInstallationSource resuelve instalaciones de producto y sus
// secretos. Lo implementa *products.Usecases (reuso del resolver canónico de
// internal/products); el client NO lee credenciales de env vars globales.
type ProductInstallationSource interface {
	ResolveInstallation(ctx context.Context, orgID, productSurface string) (products.Installation, error)
	ResolveInstallationSecret(ctx context.Context, orgID, productSurface string) (secrets.Secret, error)
	ListInstallationsByProduct(ctx context.Context, productSurface string) ([]products.Installation, error)
}

// ProductClient es el HTTP client genérico del ProductConnector. A diferencia
// de PontiClient (base URL + API key globales por env), resuelve base_url y
// auth por call desde la product installation activa de org_id +
// product_surface (fail-closed sin instalación activa).
type ProductClient struct {
	productSurface string
	installations  ProductInstallationSource
	HTTP           *http.Client
}

// NewProductClient construye el client genérico para un product_surface.
func NewProductClient(productSurface string, installations ProductInstallationSource) *ProductClient {
	return &ProductClient{
		productSurface: strings.TrimSpace(strings.ToLower(productSurface)),
		installations:  installations,
		HTTP:           &http.Client{Timeout: 10 * time.Second},
	}
}

// ProductSurface devuelve el producto que sirve este client.
func (c *ProductClient) ProductSurface() string { return c.productSurface }

// productExecutionEnvelope es el body POST {base_url}{execute_path}.
type productExecutionEnvelope struct {
	SchemaVersion  string               `json:"schema_version"`
	Operation      string               `json:"operation"`
	ExecutorRef    string               `json:"executor_ref,omitempty"`
	Payload        json.RawMessage      `json:"payload"`
	Workspace      map[string]any       `json:"workspace,omitempty"`
	IdempotencyKey string               `json:"idempotency_key,omitempty"`
	TaskID         string               `json:"task_id,omitempty"`
	RunID          string               `json:"run_id,omitempty"`
	NexusRequestID string               `json:"nexus_request_id,omitempty"`
	Actor          productEnvelopeActor `json:"actor"`
	OrgID          string               `json:"org_id"`
}

type productEnvelopeActor struct {
	ActorID        string `json:"actor_id"`
	ActorType      string `json:"actor_type,omitempty"`
	OnBehalfOf     string `json:"on_behalf_of,omitempty"`
	ProductSurface string `json:"product_surface,omitempty"`
}

// productExecutionResponse es la respuesta esperada del producto.
type productExecutionResponse struct {
	Status      string          `json:"status"`
	ExternalRef string          `json:"external_ref"`
	Result      json.RawMessage `json:"result"`
	Evidence    map[string]any  `json:"evidence"`
	Error       string          `json:"error"`
}

// IsEnvelopeInstallation indica si una installation habilita el connector
// genérico (config.connector_mode == envelope.v1).
func IsEnvelopeInstallation(installation products.Installation) bool {
	mode, _ := installation.Config[productConnectorModeConfigKey].(string)
	return strings.TrimSpace(strings.ToLower(mode)) == ProductConnectorModeEnvelopeV1
}

func installationConfigPath(installation products.Installation, key, fallback string) string {
	value, _ := installation.Config[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

// installationTenantID es el tenant header que el producto espera: su id
// nativo cuando está mapeado, el org_id de Axis como fallback.
func installationTenantID(installation products.Installation) string {
	if installation.ExternalTenantID != "" {
		return installation.ExternalTenantID
	}
	return installation.OrgID
}

func installationRequiredScopes(installation products.Installation) []string {
	raw, ok := installation.Config[productRequiredScopesConfigKey]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return cleanStringList(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return cleanStringList(out)
	default:
		return nil
	}
}

// DiscoverManifest descubre el catálogo de capabilities del producto vía
// GET {base_url}{config.discovery_path}. Igual que la metadata de Ponti, el
// manifest no es tenant-scoped: usamos la primera instalación envelope
// habilitada del producto como fuente de discovery.
func (c *ProductClient) DiscoverManifest(ctx context.Context) (ai.CapabilityManifest, error) {
	installation, err := c.discoveryInstallation(ctx)
	if err != nil {
		return ai.CapabilityManifest{}, err
	}
	var resp struct {
		Items []ai.CapabilityManifest `json:"items"`
	}
	path := installationConfigPath(installation, productDiscoveryPathConfigKey, defaultProductDiscoveryPath)
	if err := c.doGet(ctx, installation, path, &resp); err != nil {
		return ai.CapabilityManifest{}, fmt.Errorf("%s capabilities: %w", c.productSurface, err)
	}
	var merged ai.CapabilityManifest
	for _, m := range resp.Items {
		if !strings.EqualFold(strings.TrimSpace(m.Product), c.productSurface) {
			continue
		}
		if merged.ID == "" {
			merged = m
			merged.ID = c.productSurface
			merged.Tools = nil
			merged.Agents = nil
		}
		merged.Tools = append(merged.Tools, m.Tools...)
		merged.Agents = append(merged.Agents, m.Agents...)
	}
	if merged.ID == "" || len(merged.Tools) == 0 {
		return ai.CapabilityManifest{}, fmt.Errorf("product %s capabilities not present in capabilities response", c.productSurface)
	}
	return merged, nil
}

// ExecuteEnvelope postea el envelope capability_execution.v1 al producto.
// Resuelve la instalación activa del org (fail-closed) y propaga
// X-Nexus-Request-ID igual que PontiClient.doPostWithNexus.
func (c *ProductClient) ExecuteEnvelope(ctx context.Context, orgID string, envelope productExecutionEnvelope) (productExecutionResponse, error) {
	installation, err := c.installations.ResolveInstallation(ctx, orgID, c.productSurface)
	if err != nil {
		return productExecutionResponse{}, fmt.Errorf("resolve %s installation for org %s: %w", c.productSurface, orgID, err)
	}
	if !IsEnvelopeInstallation(installation) {
		return productExecutionResponse{}, fmt.Errorf("installation for org %s product %s is not connector_mode=%s", orgID, c.productSurface, ProductConnectorModeEnvelopeV1)
	}
	if len(envelope.Payload) == 0 {
		envelope.Payload = json.RawMessage(`{}`)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return productExecutionResponse{}, fmt.Errorf("marshal execution envelope: %w", err)
	}
	path := installationConfigPath(installation, productExecutePathConfigKey, defaultProductExecutePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, installation.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return productExecutionResponse{}, fmt.Errorf("build request: %w", err)
	}
	if err := c.authorize(ctx, installation, req); err != nil {
		return productExecutionResponse{}, err
	}
	if tenantID := installationTenantID(installation); tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	if scopes := installationRequiredScopes(installation); len(scopes) > 0 {
		req.Header.Set("X-Axis-Scopes", strings.Join(scopes, " "))
	}
	if nexusRequestID := strings.TrimSpace(envelope.NexusRequestID); nexusRequestID != "" {
		req.Header.Set("X-Nexus-Request-ID", nexusRequestID)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return productExecutionResponse{}, fmt.Errorf("%s http: %w", c.productSurface, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return productExecutionResponse{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return productExecutionResponse{}, fmt.Errorf("%s http %d: %s", c.productSurface, resp.StatusCode, string(raw))
	}
	var decoded productExecutionResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return productExecutionResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return decoded, nil
}

// discoveryInstallation elige la instalación que sirve de fuente del manifest.
func (c *ProductClient) discoveryInstallation(ctx context.Context) (products.Installation, error) {
	installations, err := c.installations.ListInstallationsByProduct(ctx, c.productSurface)
	if err != nil {
		return products.Installation{}, fmt.Errorf("list %s installations: %w", c.productSurface, err)
	}
	for _, installation := range installations {
		if installation.Enabled && installation.BaseURL != "" && IsEnvelopeInstallation(installation) {
			return installation, nil
		}
	}
	return products.Installation{}, fmt.Errorf("no enabled %s=%s installation for product %s", productConnectorModeConfigKey, ProductConnectorModeEnvelopeV1, c.productSurface)
}

// authorize agrega el bearer token resuelto desde el secret_ref de la
// instalación (esquema env: en dev; vault/secretmanager en adapters
// productivos). auth_mode=none queda sin header.
func (c *ProductClient) authorize(ctx context.Context, installation products.Installation, req *http.Request) error {
	if installation.AuthMode == products.AuthModeNone || installation.SecretRef == "" {
		return nil
	}
	secret, err := c.installations.ResolveInstallationSecret(ctx, installation.OrgID, c.productSurface)
	if err != nil {
		return fmt.Errorf("resolve %s installation secret: %w", c.productSurface, err)
	}
	req.Header.Set("Authorization", "Bearer "+secret.Value())
	return nil
}

func cleanStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (c *ProductClient) doGet(ctx context.Context, installation products.Installation, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, installation.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if err := c.authorize(ctx, installation, req); err != nil {
		return err
	}
	if tenantID := installationTenantID(installation); tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s http: %w", c.productSurface, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s http %d: %s", c.productSurface, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
