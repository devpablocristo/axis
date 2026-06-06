package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/mcpgovernance"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/runtime"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
)

type fakeAuthorizer struct {
	calls    int
	input    mcpgovernance.DecisionInput
	decision mcpgovernance.Decision
	err      error
}

func (f *fakeAuthorizer) Authorize(_ context.Context, in mcpgovernance.DecisionInput) (mcpgovernance.Decision, error) {
	f.calls++
	f.input = in
	return f.decision, f.err
}

type fakeProducts struct {
	listCalls int
	products  []products.Product
}

func (f *fakeProducts) ListProducts(context.Context) ([]products.Product, error) {
	f.listCalls++
	return f.products, nil
}

func (f *fakeProducts) GetProduct(context.Context, string) (products.Product, error) {
	return products.Product{}, products.ErrProductNotFound
}

func (f *fakeProducts) ResolveInstallation(context.Context, string, string) (products.Installation, error) {
	return products.Installation{}, products.ErrInstallationNotFound
}

type fakeLimiter struct {
	calls    int
	key      productlimits.Key
	limit    productlimits.Limit
	decision productlimits.Decision
	err      error
}

func (f *fakeLimiter) Allow(_ context.Context, key productlimits.Key, limit productlimits.Limit) (productlimits.Decision, error) {
	f.calls++
	f.key = key
	f.limit = limit
	if f.err != nil {
		return productlimits.Decision{}, f.err
	}
	if !f.decision.Allowed && f.decision.RetryAfter == 0 && f.decision.ResetAt.IsZero() && f.decision.Remaining == 0 {
		return productlimits.Decision{Allowed: true}, nil
	}
	return f.decision, nil
}

type fakeRecorder struct {
	events []runtime.ObservabilityEvent
	err    error
}

func (f *fakeRecorder) RecordObservabilityEvent(_ context.Context, event runtime.ObservabilityEvent) error {
	f.events = append(f.events, event)
	return f.err
}

func TestRPCToolsListRequiresMCPExecuteScope(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(Deps{Registry: reg})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req = withPrincipal(req, nil)
	res := httptest.NewRecorder()

	handler.rpc(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected rpc status 200, got %d body=%s", res.Code, res.Body.String())
	}
	var out rpcResponse
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == nil || !strings.Contains(out.Error.Message, "MCP execute scope") {
		t.Fatalf("expected missing scope rpc error, got %+v", out.Error)
	}
}

func TestRPCToolsCallExecutesAfterNexusAllow(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	authz := &fakeAuthorizer{decision: mcpgovernance.Decision{
		RequestID:       "req-1",
		Status:          "allowed",
		Decision:        "allow",
		DecisionReason:  "test allow",
		CanExecute:      true,
		PendingApproval: false,
	}}
	productStore := &fakeProducts{products: []products.Product{{
		ProductSurface: "ponti",
		DisplayName:    "Ponti",
		Status:         products.ProductStatusActive,
	}}}
	handler := NewHandler(Deps{Registry: reg, Authorizer: authz, Products: productStore})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":"call-1",
		"method":"tools/call",
		"params":{"name":"axis.products.list","arguments":{"product_surface":"companion"}}
	}`))
	req = withPrincipal(req, []string{mcpgovernance.ScopeMCPExecute, "companion:products:read"})
	res := httptest.NewRecorder()

	handler.rpc(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", res.Code, res.Body.String())
	}
	if authz.calls != 1 {
		t.Fatalf("expected one authz call, got %d", authz.calls)
	}
	if authz.input.Context.OrgID != "org-a" || authz.input.Context.ProductSurface != "companion" {
		t.Fatalf("unexpected invocation context: %+v", authz.input.Context)
	}
	if productStore.listCalls != 1 {
		t.Fatalf("expected products list execution, got %d", productStore.listCalls)
	}
	var raw map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	result := raw["result"].(map[string]any)
	if result["isError"].(bool) {
		t.Fatalf("expected non-error tool result: %+v", result)
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["status"] != "executed" {
		t.Fatalf("expected executed status, got %+v", structured)
	}
}

func TestRPCToolsCallRateLimitedBeforeNexus(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	authz := &fakeAuthorizer{}
	productStore := &fakeProducts{products: []products.Product{{ProductSurface: "ponti"}}}
	limiter := &fakeLimiter{decision: productlimits.Decision{Allowed: false, RetryAfter: time.Second}}
	recorder := &fakeRecorder{}
	handler := NewHandler(Deps{
		Registry:      reg,
		Authorizer:    authz,
		Products:      productStore,
		RateLimiter:   limiter,
		Observability: recorder,
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":"call-rate-limit",
		"method":"tools/call",
		"params":{"name":"axis.products.list","arguments":{"product_surface":"companion"}}
	}`))
	req = withPrincipal(req, []string{mcpgovernance.ScopeMCPExecute, "companion:products:read"})
	res := httptest.NewRecorder()

	handler.rpc(res, req)

	if authz.calls != 0 {
		t.Fatalf("rate-limited MCP call must not reach Nexus, authz calls=%d", authz.calls)
	}
	if productStore.listCalls != 0 {
		t.Fatalf("rate-limited MCP call must not execute tool, product calls=%d", productStore.listCalls)
	}
	if limiter.calls != 1 || limiter.key.Area != productlimits.AreaMCP || limiter.key.OrgID != "org-a" || limiter.key.ProductSurface != "companion" {
		t.Fatalf("unexpected limiter call: calls=%d key=%+v", limiter.calls, limiter.key)
	}
	var out rpcResponse
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == nil || !strings.Contains(strings.ToLower(out.Error.Message), "rate limit") {
		t.Fatalf("expected rate limit rpc error, got %+v", out.Error)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected one observability event, got %d", len(recorder.events))
	}
	if recorder.events[0].EventType != "mcp" || recorder.events[0].EventName != "mcp_tool_call" || recorder.events[0].Severity != "warn" {
		t.Fatalf("unexpected observability event: %+v", recorder.events[0])
	}
}

func TestRPCToolsCallRecordsObservabilityEvent(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	authz := &fakeAuthorizer{decision: mcpgovernance.Decision{
		RequestID:  "req-observe",
		Status:     "allowed",
		Decision:   "allow",
		CanExecute: true,
	}}
	productStore := &fakeProducts{products: []products.Product{{ProductSurface: "ponti"}}}
	recorder := &fakeRecorder{}
	handler := NewHandler(Deps{Registry: reg, Authorizer: authz, Products: productStore, Observability: recorder})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":"call-observe",
		"method":"tools/call",
		"params":{"name":"axis.products.list","arguments":{"product_surface":"companion"}}
	}`))
	req = withPrincipal(req, []string{mcpgovernance.ScopeMCPExecute, "companion:products:read"})
	res := httptest.NewRecorder()

	handler.rpc(res, req)

	if len(recorder.events) != 1 {
		t.Fatalf("expected one observability event, got %d", len(recorder.events))
	}
	event := recorder.events[0]
	if event.OrgID != "org-a" || event.ProductSurface != "companion" || event.CapabilityID != "axis.products.list" {
		t.Fatalf("unexpected event scope: %+v", event)
	}
	if event.EventType != "mcp" || event.EventName != "mcp_tool_call" || event.Severity != "info" || !event.Redacted {
		t.Fatalf("unexpected event metadata: %+v", event)
	}
}

func TestRPCToolsCallRedactsResultSecrets(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	authz := &fakeAuthorizer{decision: mcpgovernance.Decision{
		RequestID:  "req-secret",
		Status:     "allowed",
		Decision:   "allow",
		CanExecute: true,
	}}
	productStore := &fakeProducts{products: []products.Product{{
		ProductSurface: "secret-product",
		DisplayName:    "Secret Product",
		Status:         products.ProductStatusActive,
		Metadata: map[string]any{
			"api_key": "plain-secret",
			"nested": map[string]any{
				"client_secret": "nested-secret",
				"safe":          "ok",
			},
		},
	}}}
	handler := NewHandler(Deps{Registry: reg, Authorizer: authz, Products: productStore})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":"call-secret",
		"method":"tools/call",
		"params":{"name":"axis.products.list","arguments":{"product_surface":"companion"}}
	}`))
	req = withPrincipal(req, []string{mcpgovernance.ScopeMCPExecute, "companion:products:read"})
	res := httptest.NewRecorder()

	handler.rpc(res, req)

	var raw map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	result := raw["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	toolResult := structured["result"].(map[string]any)
	items := toolResult["products"].([]any)
	metadata := items[0].(map[string]any)["metadata"].(map[string]any)
	if metadata["api_key"] != "***" {
		t.Fatalf("expected api_key redacted, got %+v", metadata)
	}
	nested := metadata["nested"].(map[string]any)
	if nested["client_secret"] != "***" || nested["safe"] != "ok" {
		t.Fatalf("expected nested secret redacted and safe value preserved, got %+v", nested)
	}
}

func TestRESTToolsCallRejectsOversizedArgumentsBeforeNexus(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	authz := &fakeAuthorizer{}
	productStore := &fakeProducts{products: []products.Product{{ProductSurface: "ponti"}}}
	handler := NewHandler(Deps{Registry: reg, Authorizer: authz, Products: productStore})
	body, err := json.Marshal(map[string]any{
		"name": "axis.products.list",
		"arguments": map[string]any{
			"product_surface": "companion",
			"blob":            strings.Repeat("x", maxMCPArgumentsBytes),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/tools/call", strings.NewReader(string(body)))
	req = withPrincipal(req, []string{mcpgovernance.ScopeMCPExecute, "companion:products:read"})
	res := httptest.NewRecorder()

	handler.callToolREST(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", res.Code, res.Body.String())
	}
	if authz.calls != 0 {
		t.Fatalf("oversized arguments must not reach Nexus, authz calls=%d", authz.calls)
	}
	if productStore.listCalls != 0 {
		t.Fatalf("oversized arguments must not execute tool, product calls=%d", productStore.listCalls)
	}
}

func TestRPCToolsCallPendingApprovalDoesNotExecute(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	authz := &fakeAuthorizer{decision: mcpgovernance.Decision{
		RequestID:       "req-2",
		Status:          "pending_approval",
		Decision:        "require_approval",
		PendingApproval: true,
	}}
	productStore := &fakeProducts{products: []products.Product{{ProductSurface: "ponti"}}}
	handler := NewHandler(Deps{Registry: reg, Authorizer: authz, Products: productStore})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"id":"call-2",
		"method":"tools/call",
		"params":{"name":"axis.products.list","arguments":{"product_surface":"companion"}}
	}`))
	req = withPrincipal(req, []string{mcpgovernance.ScopeMCPExecute, "companion:products:read"})
	res := httptest.NewRecorder()

	handler.rpc(res, req)

	if productStore.listCalls != 0 {
		t.Fatalf("pending approval must not execute tool, calls=%d", productStore.listCalls)
	}
	var raw map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	result := raw["result"].(map[string]any)
	if result["isError"].(bool) {
		t.Fatalf("pending approval should be a non-error tool state: %+v", result)
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["status"] != "pending_approval" {
		t.Fatalf("expected pending_approval, got %+v", structured)
	}
}

func TestRESTToolsListRequiresScope(t *testing.T) {
	reg, err := mcpgovernance.NewDefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(Deps{Registry: reg})
	req := httptest.NewRequest(http.MethodGet, "/v1/mcp/tools", nil)
	req = withPrincipal(req, nil)
	res := httptest.NewRecorder()

	handler.listToolsREST(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d body=%s", res.Code, res.Body.String())
	}
}

func withPrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "org-a", Actor: "agent-a", Scopes: scopes, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
