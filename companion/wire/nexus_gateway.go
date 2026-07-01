package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/nexusclient"
)

// nexusGateway es el cliente HTTP de companion hacia el servicio Nexus.
// Conserva el cliente compartido de platform porque el contrato HTTP no cambió.
type nexusGateway struct {
	client  *nexusclient.Client
	baseURL string
	apiKey  string
	http    *http.Client
}

type nexusError struct {
	Kind       string
	StatusCode int
	Body       string
}

func (e nexusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("nexus %s: status %d", e.Kind, e.StatusCode)
	}
	return fmt.Sprintf("nexus %s: status %d body %s", e.Kind, e.StatusCode, e.Body)
}

func newNexusGateway(baseURL, apiKey string) *nexusGateway {
	return &nexusGateway{
		client:  nexusclient.NewClient(baseURL, apiKey),
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *nexusGateway) SubmitRequest(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
	return g.client.SubmitRequest(ctx, idempotencyKey, body)
}

func (g *nexusGateway) GetRequest(ctx context.Context, id string) (nexusclient.RequestSummary, int, error) {
	return g.client.GetRequest(ctx, id)
}

func (g *nexusGateway) ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error) {
	payload := map[string]any{
		"success":     success,
		"result":      result,
		"duration_ms": durationMS,
	}
	if strings.TrimSpace(errorMessage) != "" {
		payload["error_message"] = errorMessage
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal nexus result: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/requests/"+id+"/result", bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("build nexus result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post nexus result: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (g *nexusGateway) CreateAttestation(ctx context.Context, id string, payload map[string]any) (int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal nexus attestation: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/requests/"+id+"/attest", bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("build nexus attestation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post nexus attestation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, buildNexusError(resp)
	}
	return resp.StatusCode, nil
}

func (g *nexusGateway) GetEvidencePack(ctx context.Context, id string) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/v1/requests/"+id+"/evidence", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build nexus evidence request: %w", err)
	}
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("get nexus evidence: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, resp.StatusCode, buildNexusError(resp)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode nexus evidence: %w", err)
	}
	return out, resp.StatusCode, nil
}

func buildNexusError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return nexusError{
		Kind:       classifyNexusStatus(resp.StatusCode),
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(raw)),
	}
}

func classifyNexusStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusConflict:
		return "conflict"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "validation"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "unavailable"
	default:
		if status >= 500 {
			return "unavailable"
		}
		return "unexpected"
	}
}
