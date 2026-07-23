package productintegrations

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

	"github.com/google/uuid"
)

type ServiceClient interface {
	Prepare(context.Context, ServicePrepareInput) (ServiceSnapshot, error)
	Validate(context.Context, ServiceMutationInput) (ServiceSnapshot, error)
	Activate(context.Context, ServiceMutationInput) error
	Readiness(context.Context, ServiceReadinessInput) (ServiceReadiness, error)
}

type ServicePrepareInput struct {
	Service             string
	BaseURL             string
	OrgID               uuid.UUID
	ProductID           uuid.UUID
	ProductSurface      string
	Actor               string
	Role                string
	SourceIntegrationID uuid.UUID
	SourceVersionID     uuid.UUID
	SourceRevision      int64
	ContractHash        string
	Section             ServiceSection
}

type ServiceMutationInput struct {
	Service        string
	BaseURL        string
	OrgID          uuid.UUID
	ProductID      uuid.UUID
	ProductSurface string
	Actor          string
	Role           string
	VersionID      uuid.UUID
}

type ServiceReadinessInput struct {
	Service        string
	BaseURL        string
	OrgID          uuid.UUID
	ProductID      uuid.UUID
	ProductSurface string
	Actor          string
	Role           string
}

type HTTPServiceClient struct {
	internalToken string
	client        *http.Client
}

type ParticipantHTTPError struct {
	StatusCode int
	Retryable  bool
}

func (e *ParticipantHTTPError) Error() string {
	return fmt.Sprintf("integration participant returned status %d", e.StatusCode)
}

func NewHTTPServiceClient(internalToken string, client *http.Client) *HTTPServiceClient {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &HTTPServiceClient{internalToken: strings.TrimSpace(internalToken), client: client}
}

func (c *HTTPServiceClient) Prepare(ctx context.Context, in ServicePrepareInput) (ServiceSnapshot, error) {
	section := any(in.Section)
	if in.Service == "nexus" {
		actionTypes := make([]map[string]string, 0, len(in.Section.ActionTypes))
		for _, key := range in.Section.ActionTypes {
			actionTypes = append(actionTypes, map[string]string{"key": key})
		}
		section = map[string]any{
			"schema_version": in.Section.SchemaVersion, "api_contracts": in.Section.APIContracts,
			"action_types": actionTypes, "governed_operations": in.Section.GovernedOperations,
			"access_modes": in.Section.AccessModes, "webhooks": in.Section.Webhooks,
		}
	}
	body := map[string]any{
		"source_integration_id": in.SourceIntegrationID, "source_version_id": in.SourceVersionID,
		"version": in.SourceRevision, "contract_hash": in.ContractHash,
		"product_surface": in.ProductSurface, "section": section,
	}
	target := serviceURL(in.BaseURL, "/v1/product-integrations/"+url.PathEscape(in.ProductID.String())+"/versions")
	var out struct {
		ID          uuid.UUID `json:"id"`
		ContentHash string    `json:"content_hash"`
	}
	if err := c.do(ctx, http.MethodPost, target, in.OrgID, in.ProductID, in.ProductSurface, in.Actor, in.Role, body, &out); err != nil {
		return ServiceSnapshot{}, err
	}
	return ServiceSnapshot{Service: in.Service, VersionID: out.ID, ContentHash: out.ContentHash}, nil
}

func (c *HTTPServiceClient) Validate(ctx context.Context, in ServiceMutationInput) (ServiceSnapshot, error) {
	target := serviceURL(in.BaseURL, "/v1/product-integration-versions/"+url.PathEscape(in.VersionID.String())+"/validate")
	var out struct {
		Valid       bool   `json:"valid"`
		ContentHash string `json:"content_hash"`
	}
	if err := c.do(ctx, http.MethodPost, target, in.OrgID, in.ProductID, in.ProductSurface, in.Actor, in.Role, nil, &out); err != nil {
		return ServiceSnapshot{}, err
	}
	return ServiceSnapshot{Service: in.Service, VersionID: in.VersionID, Valid: out.Valid, ContentHash: out.ContentHash}, nil
}

func (c *HTTPServiceClient) Activate(ctx context.Context, in ServiceMutationInput) error {
	target := serviceURL(in.BaseURL, "/v1/product-integration-versions/"+url.PathEscape(in.VersionID.String())+"/activate")
	return c.do(ctx, http.MethodPost, target, in.OrgID, in.ProductID, in.ProductSurface, in.Actor, in.Role, nil, nil)
}

func (c *HTTPServiceClient) Readiness(ctx context.Context, in ServiceReadinessInput) (ServiceReadiness, error) {
	target := serviceURL(in.BaseURL, "/v1/product-integrations/"+url.PathEscape(in.ProductID.String())+"/readiness")
	var out ServiceReadiness
	if err := c.do(ctx, http.MethodGet, target, in.OrgID, in.ProductID, in.ProductSurface, in.Actor, in.Role, nil, &out); err != nil {
		return ServiceReadiness{Service: in.Service, Status: "unavailable"}, err
	}
	if out.Service == "" {
		out.Service = in.Service
	}
	return out, nil
}

func (c *HTTPServiceClient) do(
	ctx context.Context,
	method, target string,
	orgID, productID uuid.UUID,
	surface, actor, role string,
	body any,
	out any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Axis-Internal-Token", c.internalToken)
	request.Header.Set("X-Org-ID", orgID.String())
	request.Header.Set("X-Product-ID", productID.String())
	request.Header.Set("X-Axis-Product-ID", productID.String())
	request.Header.Set("X-Product-Surface", surface)
	request.Header.Set("X-Actor-ID", actor)
	request.Header.Set("X-Axis-Principal-Type", "human")
	request.Header.Set("X-Axis-Access-Mode", "via_orchestrator")
	request.Header.Set("X-Axis-Org-Role", role)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &ParticipantHTTPError{
			StatusCode: response.StatusCode,
			Retryable: response.StatusCode == http.StatusRequestTimeout ||
				response.StatusCode == http.StatusTooManyRequests ||
				response.StatusCode >= http.StatusInternalServerError,
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func serviceURL(baseURL, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + path
}
