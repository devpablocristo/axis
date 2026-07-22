package mcpgovernance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRPCInitializeAndContextualToolsList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uc, _, capability, _, request := testUseCases(t, "read")
	uc.RegisterReadExecutor(capability.CapabilityKey, fakeReader{result: map[string]any{"items": []any{}}})
	router := gin.New()
	NewHandler(uc).MCPRoutes(router)

	initialize := rpcTestRequest(t, router, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, nil)
	if initialize.Code != http.StatusOK || !strings.Contains(initialize.Body.String(), ProtocolVersion) {
		t.Fatalf("unexpected initialize: %d %s", initialize.Code, initialize.Body.String())
	}

	headers := map[string]string{
		"X-Tenant-ID": request.TenantID, "X-Actor-ID": request.ActorID,
		"X-Axis-Tenant-Role":  request.ActorRole,
		"X-Axis-Virployee-ID": request.VirployeeID.String(), "X-Axis-Subject-ID": request.SubjectID.String(),
	}
	listed := rpcTestRequest(t, router, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`, headers)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), capability.CapabilityKey) || !strings.Contains(listed.Body.String(), "inputSchema") {
		t.Fatalf("unexpected tools/list: %d %s", listed.Code, listed.Body.String())
	}
}

func TestRPCToolsCallRequiresContext(t *testing.T) {
	uc, _, _, _, _ := testUseCases(t, "read")
	router := gin.New()
	NewHandler(uc).MCPRoutes(router)
	response := rpcTestRequest(t, router, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"calendar.events.read","arguments":{}}}`, nil)
	var decoded rpcResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Error == nil || decoded.Error.Code != -32602 {
		t.Fatalf("missing trusted context must fail: %s", response.Body.String())
	}
}

func TestRPCRejectsTrailingJSONAndOversizedArguments(t *testing.T) {
	uc, _, _, _, request := testUseCases(t, "read")
	router := gin.New()
	NewHandler(uc).MCPRoutes(router)
	trailing := rpcTestRequest(t, router, `{"jsonrpc":"2.0","id":1,"method":"initialize"}{}`, nil)
	if !strings.Contains(trailing.Body.String(), `"code":-32700`) {
		t.Fatalf("trailing JSON must be rejected: %s", trailing.Body.String())
	}
	headers := map[string]string{
		"X-Tenant-ID": request.TenantID, "X-Actor-ID": request.ActorID,
		"X-Axis-Tenant-Role": request.ActorRole, "X-Axis-Virployee-ID": request.VirployeeID.String(),
		"X-Axis-Subject-ID": request.SubjectID.String(),
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"calendar.events.read","arguments":{"value":"` + strings.Repeat("x", maxArgumentsBytes) + `"}}}`
	oversized := rpcTestRequest(t, router, body, headers)
	if !strings.Contains(oversized.Body.String(), "exceed size limit") {
		t.Fatalf("oversized arguments must be rejected: %s", oversized.Body.String())
	}
}

func rpcTestRequest(t *testing.T, handler http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
