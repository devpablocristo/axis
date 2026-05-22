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

	connectors "github.com/devpablocristo/companion/internal/connectors"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
)

// governanceGateway es el cliente HTTP de companion hacia el servicio Nexus Governance.
// Conserva el cliente compartido de platform porque el contrato HTTP no cambió.
type governanceGateway struct {
	client  *governanceclient.Client
	baseURL string
	apiKey  string
	http    *http.Client
}

type governanceError struct {
	Kind       string
	StatusCode int
	Body       string
}

func (e governanceError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("governance %s: status %d", e.Kind, e.StatusCode)
	}
	return fmt.Sprintf("governance %s: status %d body %s", e.Kind, e.StatusCode, e.Body)
}

func newGovernanceGateway(baseURL, apiKey string) *governanceGateway {
	return &governanceGateway{
		client:  governanceclient.NewClient(baseURL, apiKey),
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *governanceGateway) SubmitRequest(ctx context.Context, idempotencyKey string, body governanceclient.SubmitRequestBody) (governanceclient.SubmitResponse, error) {
	return g.client.SubmitRequest(ctx, idempotencyKey, body)
}

func (g *governanceGateway) GetRequest(ctx context.Context, id string) (governanceclient.RequestSummary, int, error) {
	return g.client.GetRequest(ctx, id)
}

func (g *governanceGateway) GetRequestMeta(ctx context.Context, id string) (connectors.GovernanceRequestMeta, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/v1/requests/"+id, nil)
	if err != nil {
		return connectors.GovernanceRequestMeta{}, 0, fmt.Errorf("build governance get request: %w", err)
	}
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return connectors.GovernanceRequestMeta{}, 0, fmt.Errorf("get governance request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return connectors.GovernanceRequestMeta{}, resp.StatusCode, nil
	}
	var body struct {
		Status        string         `json:"status"`
		OrgID         string         `json:"org_id"`
		BindingHash   string         `json:"binding_hash"`
		ActionBinding map[string]any `json:"action_binding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return connectors.GovernanceRequestMeta{}, resp.StatusCode, fmt.Errorf("decode governance get response: %w", err)
	}
	return connectors.GovernanceRequestMeta{
		Status:        body.Status,
		OrgID:         body.OrgID,
		BindingHash:   body.BindingHash,
		ActionBinding: body.ActionBinding,
	}, resp.StatusCode, nil
}

func (g *governanceGateway) ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error) {
	payload := map[string]any{
		"success":     success,
		"result":      result,
		"duration_ms": durationMS,
	}
	if resultID, _ := result["connector_execution_id"].(string); strings.TrimSpace(resultID) != "" {
		payload["result_id"] = strings.TrimSpace(resultID)
	}
	if strings.TrimSpace(errorMessage) != "" {
		payload["error_message"] = errorMessage
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal governance result: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/requests/"+id+"/result", bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("build governance result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post governance result: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (g *governanceGateway) CreateAttestation(ctx context.Context, id string, payload map[string]any) (int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal governance attestation: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/requests/"+id+"/attest", bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("build governance attestation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post governance attestation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, buildGovernanceError(resp)
	}
	return resp.StatusCode, nil
}

func (g *governanceGateway) GetEvidencePack(ctx context.Context, id string) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/v1/requests/"+id+"/evidence", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build governance evidence request: %w", err)
	}
	req.Header.Set("X-API-Key", g.apiKey)
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("get governance evidence: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, resp.StatusCode, buildGovernanceError(resp)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode governance evidence: %w", err)
	}
	return out, resp.StatusCode, nil
}

func buildGovernanceError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return governanceError{
		Kind:       classifyGovernanceStatus(resp.StatusCode),
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(raw)),
	}
}

func classifyGovernanceStatus(status int) string {
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
