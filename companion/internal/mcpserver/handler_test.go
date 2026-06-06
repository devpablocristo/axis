package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/mcpgovernance"
	"github.com/devpablocristo/companion/internal/products"
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
