// Package companionhttp implements the product-edge outbound ports over the
// current Companion HTTP API. Product-facing handlers never depend on its URL
// shape or transport DTOs.
package companionhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/productedge"
)

type Adapter struct {
	baseURL       string
	internalToken string
	client        *http.Client
}

func New(baseURL, internalToken string, client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &Adapter{
		baseURL:       strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		internalToken: strings.TrimSpace(internalToken),
		client:        client,
	}
}

func (a *Adapter) StartAssist(ctx context.Context, invocation productedge.InvocationContext, input productedge.AssistInput) (productedge.AssistRun, error) {
	body := map[string]any{
		"input_json":            input.Input,
		"idempotency_key":       input.IdempotencyKey,
		"assist_type":           input.AssistType,
		"product_surface":       invocation.ProductSurface,
		"capability_id":         input.CapabilityID,
		"capability_key":        input.CapabilityKey,
		"subject_id":            input.SubjectID,
		"repository_generation": input.RepositoryGeneration,
		"case_id":               input.CaseID,
		"assignment_id":         input.AssignmentID,
	}
	var out productedge.AssistRun
	err := a.doJSON(
		ctx,
		invocation,
		http.MethodPost,
		"/v1/virployees/"+url.PathEscape(input.VirployeeID)+"/assist-runs",
		body,
		&out,
	)
	return out, err
}

func (a *Adapter) GetAssistRun(ctx context.Context, invocation productedge.InvocationContext, virployeeID, runID string) (productedge.AssistRun, error) {
	var out productedge.AssistRun
	err := a.doJSON(
		ctx,
		invocation,
		http.MethodGet,
		"/v1/virployees/"+url.PathEscape(virployeeID)+"/assist-runs/"+url.PathEscape(runID),
		nil,
		&out,
	)
	return out, err
}

func (a *Adapter) ResolveRouting(ctx context.Context, invocation productedge.InvocationContext, input productedge.RoutingInput) (productedge.RoutingResolution, error) {
	body := map[string]string{
		"pool_id":        input.PoolID,
		"subject_id":     strings.TrimSpace(input.SubjectID),
		"capability_id":  strings.TrimSpace(input.CapabilityID),
		"capability_key": strings.TrimSpace(input.CapabilityKey),
	}
	var out productedge.RoutingResolution
	err := a.doJSON(ctx, invocation, http.MethodPost, "/v1/virployee-routing:resolve", body, &out)
	return out, err
}

func (a *Adapter) PublishProductEvent(ctx context.Context, invocation productedge.InvocationContext, event productedge.ProductEvent) (productedge.Response, error) {
	// The public Axis contract keeps `version`; Companion's current transport
	// calls the same value `event_version`. This translation stays in the
	// adapter, not in the application handler.
	body := map[string]any{
		"event_id":      event.EventID,
		"event_type":    event.EventType,
		"event_version": event.Version,
		"payload":       event.Payload,
	}
	if strings.TrimSpace(event.VirployeeID) != "" {
		body["virployee_id"] = event.VirployeeID
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return productedge.Response{}, err
	}
	request, err := a.request(ctx, invocation, http.MethodPost, "/v1/product-events", encoded)
	if err != nil {
		return productedge.Response{}, err
	}
	request.Header.Set("Idempotency-Key", event.EventID)
	response, err := a.client.Do(request)
	if err != nil {
		return productedge.Response{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return productedge.Response{}, err
	}
	return productedge.Response{
		StatusCode:  response.StatusCode,
		ContentType: response.Header.Get("Content-Type"),
		RetryAfter:  response.Header.Get("Retry-After"),
		Body:        raw,
	}, nil
}

func (a *Adapter) doJSON(
	ctx context.Context,
	invocation productedge.InvocationContext,
	method, path string,
	body any,
	out any,
) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	request, err := a.request(ctx, invocation, method, path, encoded)
	if err != nil {
		return err
	}
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &productedge.DownstreamError{
			StatusCode: response.StatusCode,
			RetryAfter: response.Header.Get("Retry-After"),
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode Companion response: %w", err)
	}
	return nil
}

func (a *Adapter) request(
	ctx context.Context,
	invocation productedge.InvocationContext,
	method, path string,
	body []byte,
) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Axis-Internal-Token", a.internalToken)
	request.Header.Set("X-Org-ID", invocation.OrgID)
	request.Header.Set("X-Actor-ID", invocation.PrincipalID)
	request.Header.Set("X-Axis-Principal-Type", invocation.PrincipalType)
	request.Header.Set("X-Product-ID", invocation.ProductID)
	request.Header.Set("X-Axis-Product-ID", invocation.ProductID)
	request.Header.Set("X-Product-Surface", invocation.ProductSurface)
	request.Header.Set("X-Axis-Forwarded-By", "bff-v2")
	request.Header.Set("X-Axis-Access-Mode", invocation.AccessMode)
	if len(invocation.Scopes) > 0 {
		request.Header.Set("X-Axis-Scopes", strings.Join(invocation.Scopes, " "))
	}
	if invocation.IntegrationID != "" {
		request.Header.Set("X-Axis-Integration-ID", invocation.IntegrationID)
		request.Header.Set("X-Axis-Integration-Version", strconv.FormatInt(invocation.IntegrationRevision, 10))
		request.Header.Set("X-Axis-Integration-Hash", invocation.IntegrationHash)
	}
	return request, nil
}

var (
	_ productedge.StartAssist         = (*Adapter)(nil)
	_ productedge.GetAssistRun        = (*Adapter)(nil)
	_ productedge.PublishProductEvent = (*Adapter)(nil)
	_ productedge.ResolveRouting      = (*Adapter)(nil)
)
