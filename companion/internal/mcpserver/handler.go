package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/mcpgovernance"
	"github.com/devpablocristo/companion/internal/ops"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/securityevals"
	"github.com/devpablocristo/companion/internal/tasks"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	ProtocolVersion = "2025-11-25"

	scopeCrossOrg = "companion:cross_org"

	maxMCPRequestBytes   = 128 * 1024
	maxMCPArgumentsBytes = 64 * 1024
	maxMCPResultBytes    = 256 * 1024
)

var (
	ErrValidation      = errors.New("mcp validation failed")
	ErrForbidden       = errors.New("mcp forbidden")
	ErrNotImplemented  = errors.New("mcp tool executor not implemented")
	ErrExecutionFailed = errors.New("mcp tool execution failed")
	ErrPayloadTooLarge = errors.New("mcp payload too large")
)

type Authorizer interface {
	Authorize(ctx context.Context, in mcpgovernance.DecisionInput) (mcpgovernance.Decision, error)
}

type ProductUsecases interface {
	ListProducts(ctx context.Context) ([]products.Product, error)
	GetProduct(ctx context.Context, productSurface string) (products.Product, error)
	ResolveInstallation(ctx context.Context, orgID, productSurface string) (products.Installation, error)
}

type CapabilityUsecases interface {
	CheckConformance(ctx context.Context, manifest capabilities.Manifest) (map[string]bool, []string)
	ImportManifest(ctx context.Context, manifest capabilities.Manifest, importedBy string) (capabilities.ManifestRecord, error)
}

type OpsUsecases interface {
	GetConsole(ctx context.Context, q ops.Query) (ops.Console, error)
	ListAlerts(ctx context.Context, q ops.Query) ([]ops.Alert, error)
	ListSLOs(ctx context.Context, q ops.Query) ([]ops.ProductSLO, error)
}

type CostSummaries interface {
	GetCostSummary(ctx context.Context, orgID, productSurface, period string, limit int) (runtime.CostSummary, error)
}

type TraceReplays interface {
	GetRunReplay(ctx context.Context, runID uuid.UUID) (runtime.RunReplay, error)
}

type EvalRunner interface {
	RunSuite(ctx context.Context, orgID, productSurface, suiteID, createdBy string) (securityevals.Report, error)
}

type TaskCreator interface {
	Create(ctx context.Context, in tasks.CreateTaskInput) (taskdomain.Task, error)
}

type NexusRequestLister interface {
	ListRequestsForOrg(ctx context.Context, query, orgID string) (int, []byte, error)
}

type Deps struct {
	Registry      *mcpgovernance.Registry
	Authorizer    Authorizer
	Products      ProductUsecases
	Capabilities  CapabilityUsecases
	Ops           OpsUsecases
	Costs         CostSummaries
	Traces        TraceReplays
	Evals         EvalRunner
	Tasks         TaskCreator
	Nexus         NexusRequestLister
	RateLimiter   productlimits.Limiter
	Observability runtime.ObservabilityRecorder
}

type Handler struct {
	registry *mcpgovernance.Registry
	authz    Authorizer
	executor *Executor
	limiter  productlimits.Limiter
	recorder runtime.ObservabilityRecorder
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		registry: deps.Registry,
		authz:    deps.Authorizer,
		executor: &Executor{deps: deps},
		limiter:  deps.RateLimiter,
		recorder: deps.Observability,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /mcp", h.rpc)
	mux.HandleFunc("GET /v1/mcp/tools", h.listToolsREST)
	mux.HandleFunc("POST /v1/mcp/tools/call", h.callToolREST)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type callRequest struct {
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments,omitempty"`
	IdempotencyKey   string         `json:"idempotency_key,omitempty"`
	RunID            string         `json:"run_id,omitempty"`
	ToolInvocationID string         `json:"tool_invocation_id,omitempty"`
	Reason           string         `json:"reason,omitempty"`
}

type toolCallResponse struct {
	Status          string                 `json:"status"`
	ToolName        string                 `json:"tool_name"`
	RequestID       string                 `json:"request_id,omitempty"`
	NexusStatus     string                 `json:"nexus_status,omitempty"`
	NexusDecision   string                 `json:"nexus_decision,omitempty"`
	DecisionReason  string                 `json:"decision_reason,omitempty"`
	PendingApproval bool                   `json:"pending_approval,omitempty"`
	Denied          bool                   `json:"denied,omitempty"`
	Result          any                    `json:"result,omitempty"`
	Error           string                 `json:"error,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func (h *Handler) rpc(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMCPRequestBytes)).Decode(&req); err != nil {
		if isRequestBodyTooLarge(err) {
			writeRPCError(w, nil, -32600, "MCP request exceeds size limit", map[string]any{
				"status":    http.StatusRequestEntityTooLarge,
				"max_bytes": maxMCPRequestBytes,
			})
			return
		}
		writeRPCError(w, nil, -32700, "Parse error", nil)
		return
	}
	if strings.TrimSpace(req.JSONRPC) != "2.0" {
		writeRPCError(w, req.ID, -32600, "Invalid JSON-RPC request", nil)
		return
	}

	switch strings.TrimSpace(req.Method) {
	case "initialize":
		if !h.requireAccessRPC(w, req.ID, r) {
			return
		}
		writeRPCResult(w, req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":        "axis-companion",
				"title":       "Axis Companion MCP",
				"version":     "1.0.0",
				"description": "Governed operational tools for Axis.",
			},
			"instructions": "All tool calls are authorized by Nexus before execution.",
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
	case "ping":
		if !h.requireAccessRPC(w, req.ID, r) {
			return
		}
		writeRPCResult(w, req.ID, map[string]any{})
	case "tools/list":
		if !h.requireAccessRPC(w, req.ID, r) {
			return
		}
		writeRPCResult(w, req.ID, map[string]any{"tools": h.toolViews()})
	case "tools/call":
		if !h.requireAccessRPC(w, req.ID, r) {
			return
		}
		var call callRequest
		if err := json.Unmarshal(req.Params, &call); err != nil {
			writeRPCError(w, req.ID, -32602, "Invalid tools/call params", nil)
			return
		}
		out, status, err := h.callTool(r, call)
		if err != nil && status >= 400 && status != http.StatusForbidden {
			writeRPCError(w, req.ID, -32602, err.Error(), map[string]any{"status": status})
			return
		}
		writeRPCResult(w, req.ID, toolResult(out, err != nil || out.Denied))
	default:
		writeRPCError(w, req.ID, -32601, "Method not found", nil)
	}
}

func (h *Handler) listToolsREST(w http.ResponseWriter, r *http.Request) {
	if !h.requireAccessREST(w, r) {
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"tools": h.toolViews()})
}

func (h *Handler) callToolREST(w http.ResponseWriter, r *http.Request) {
	if !h.requireAccessREST(w, r) {
		return
	}
	var call callRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMCPRequestBytes)).Decode(&call); err != nil {
		if isRequestBodyTooLarge(err) {
			httpjson.WriteFlatError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "mcp request exceeds size limit")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	out, status, err := h.callTool(r, call)
	if err != nil && status >= 400 {
		httpjson.WriteJSON(w, status, out)
		return
	}
	httpjson.WriteJSON(w, status, out)
}

func (h *Handler) callTool(r *http.Request, call callRequest) (out toolCallResponse, status int, err error) {
	call.Name = strings.TrimSpace(call.Name)
	if call.Name == "" {
		err = fmt.Errorf("%w: tool name is required", ErrValidation)
		return toolCallResponse{Status: "error", Error: err.Error()}, http.StatusBadRequest, err
	}
	if call.Arguments == nil {
		call.Arguments = map[string]any{}
	}
	invocation, err := invocationContext(r, call.Arguments)
	if err != nil {
		status := statusForError(err)
		return toolCallResponse{Status: "error", ToolName: call.Name, Error: err.Error()}, status, err
	}
	started := time.Now().UTC()
	out = toolCallResponse{ToolName: call.Name}
	status = http.StatusInternalServerError
	defer func() {
		h.recordToolCall(r.Context(), invocation, call, out, status, err, started)
	}()

	if err = enforceJSONSize(call.Arguments, maxMCPArgumentsBytes, "arguments"); err != nil {
		out.Status = "payload_too_large"
		out.Error = err.Error()
		status = statusForError(err)
		return out, status, err
	}
	if err = h.enforceRateLimit(r.Context(), invocation); err != nil {
		out.Status = "rate_limited"
		out.Error = err.Error()
		out.Metadata = rateLimitMetadata(err)
		status = statusForError(err)
		return out, status, err
	}
	if h.authz == nil {
		err = fmt.Errorf("%w: mcp authorizer is not configured", ErrExecutionFailed)
		out.Status = "error"
		out.Error = err.Error()
		status = http.StatusInternalServerError
		return out, status, err
	}
	decision, err := h.authz.Authorize(r.Context(), mcpgovernance.DecisionInput{
		ToolName:         call.Name,
		Context:          invocation,
		Arguments:        call.Arguments,
		IdempotencyKey:   firstNonEmpty(call.IdempotencyKey, stringArg(call.Arguments, "idempotency_key")),
		RunID:            firstNonEmpty(call.RunID, stringArg(call.Arguments, "run_id")),
		ToolInvocationID: firstNonEmpty(call.ToolInvocationID, stringArg(call.Arguments, "tool_invocation_id")),
		Reason:           firstNonEmpty(call.Reason, stringArg(call.Arguments, "reason")),
	})
	if err != nil {
		out.Status = "error"
		out.Error = err.Error()
		status = statusForError(err)
		return out, status, err
	}
	base := toolCallResponse{
		ToolName:        call.Name,
		RequestID:       decision.RequestID,
		NexusStatus:     decision.Status,
		NexusDecision:   decision.Decision,
		DecisionReason:  decision.DecisionReason,
		PendingApproval: decision.PendingApproval,
		Denied:          decision.Denied,
	}
	out = base
	if decision.PendingApproval {
		out.Status = "pending_approval"
		status = http.StatusAccepted
		return out, status, nil
	}
	if decision.Denied {
		out.Status = "denied"
		err = ErrForbidden
		status = http.StatusForbidden
		return out, status, err
	}
	if !decision.CanExecute {
		err = fmt.Errorf("%w: nexus did not allow execution", ErrForbidden)
		out.Status = "blocked"
		out.Error = err.Error()
		status = http.StatusForbidden
		return out, status, err
	}
	if h.executor == nil {
		err = fmt.Errorf("%w: executor is not configured", ErrExecutionFailed)
		out.Status = "error"
		out.Error = err.Error()
		status = http.StatusInternalServerError
		return out, status, err
	}
	result, err := h.executor.Execute(r.Context(), call.Name, call.Arguments, invocation)
	if err != nil {
		out.Status = "execution_error"
		out.Error = err.Error()
		status = statusForError(err)
		return out, status, err
	}
	redactedResult, err := redactAndLimitPayload(result, maxMCPResultBytes, "result")
	if err != nil {
		out.Status = "payload_too_large"
		out.Error = err.Error()
		status = statusForError(err)
		return out, status, err
	}
	out.Status = "executed"
	out.Result = redactedResult
	status = http.StatusOK
	return out, status, nil
}

func invocationContext(r *http.Request, args map[string]any) (mcpgovernance.InvocationContext, error) {
	id := identityctx.FromRequest(r)
	orgID, ok := identityctx.EffectiveOrgID(r, stringArg(args, "org_id"), scopeCrossOrg)
	if !ok {
		return mcpgovernance.InvocationContext{}, fmt.Errorf("%w: org_id is not allowed for this principal", ErrForbidden)
	}
	if strings.TrimSpace(orgID) == "" {
		return mcpgovernance.InvocationContext{}, fmt.Errorf("%w: org_id is required", ErrValidation)
	}
	productSurface := firstNonEmpty(stringArg(args, "product_surface"), id.ProductSurface, identityctx.DefaultSurface)
	return mcpgovernance.InvocationContext{
		OrgID:              orgID,
		ProductSurface:     productSurface,
		ActorID:            id.EffectiveActorID(),
		ActorType:          id.ActorType,
		OnBehalfOf:         id.OnBehalfOf,
		ServicePrincipal:   id.ServicePrincipal,
		Scopes:             append([]string(nil), id.Scopes...),
		CompanionPrincipal: id.CompanionPrincipal,
	}, nil
}

func (h *Handler) toolViews() []mcpTool {
	if h == nil || h.registry == nil {
		return nil
	}
	tools := h.registry.List()
	out := make([]mcpTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, mcpTool{
			Name:        tool.Name,
			Title:       titleForTool(tool.Name),
			Description: tool.Description,
			InputSchema: inputSchemaForTool(tool.Name),
			Annotations: map[string]any{
				"risk_level":        tool.RiskLevel,
				"side_effect_type":  tool.SideEffectType,
				"approval_required": tool.ApprovalRequired,
				"required_scopes":   tool.RequiredScopes,
			},
		})
	}
	return out
}

func (h *Handler) requireAccessREST(w http.ResponseWriter, r *http.Request) bool {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "mcp endpoints require authenticated context")
		return false
	}
	if identityctx.HasScope(r, mcpgovernance.ScopeMCPExecute) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing mcp execute scope")
	return false
}

func (h *Handler) requireAccessRPC(w http.ResponseWriter, id json.RawMessage, r *http.Request) bool {
	if identityctx.HasNoAuthContext(r) {
		writeRPCError(w, id, -32001, "MCP endpoints require authenticated context", nil)
		return false
	}
	if identityctx.HasScope(r, mcpgovernance.ScopeMCPExecute) {
		return true
	}
	writeRPCError(w, id, -32001, "Missing MCP execute scope", nil)
	return false
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	httpjson.WriteJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: idOrNull(id), Result: result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string, data any) {
	httpjson.WriteJSON(w, http.StatusOK, rpcResponse{
		JSONRPC: "2.0",
		ID:      idOrNull(id),
		Error:   &rpcError{Code: code, Message: message, Data: data},
	})
}

func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage(`null`)
	}
	return id
}

func toolResult(out toolCallResponse, isError bool) map[string]any {
	raw, err := json.Marshal(out)
	if err != nil {
		raw = []byte(`{"status":"error","error":"marshal tool result failed"}`)
		isError = true
	}
	return map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": string(raw),
		}},
		"structuredContent": out,
		"isError":           isError,
	}
}

func (h *Handler) enforceRateLimit(ctx context.Context, invocation mcpgovernance.InvocationContext) error {
	return productlimits.Enforce(ctx, h.limiter, productlimits.Key{
		OrgID:          invocation.OrgID,
		ProductSurface: invocation.ProductSurface,
		Area:           productlimits.AreaMCP,
	}, productlimits.DefaultLimit(productlimits.AreaMCP))
}

func (h *Handler) recordToolCall(ctx context.Context, invocation mcpgovernance.InvocationContext, call callRequest, out toolCallResponse, status int, callErr error, started time.Time) {
	if h == nil || h.recorder == nil {
		return
	}
	toolName := firstNonEmpty(out.ToolName, call.Name)
	runID := firstNonEmpty(call.RunID, stringArg(call.Arguments, "run_id"))
	payload := map[string]any{
		"tool_name":           toolName,
		"status":              out.Status,
		"http_status":         status,
		"request_id":          out.RequestID,
		"nexus_status":        out.NexusStatus,
		"nexus_decision":      out.NexusDecision,
		"pending_approval":    out.PendingApproval,
		"denied":              out.Denied,
		"actor_id":            invocation.ActorID,
		"actor_type":          invocation.ActorType,
		"service_principal":   invocation.ServicePrincipal,
		"on_behalf_of":        invocation.OnBehalfOf,
		"run_id":              runID,
		"tool_invocation_id":  firstNonEmpty(call.ToolInvocationID, stringArg(call.Arguments, "tool_invocation_id")),
		"idempotency_key_set": firstNonEmpty(call.IdempotencyKey, stringArg(call.Arguments, "idempotency_key")) != "",
		"duration_ms":         time.Since(started).Milliseconds(),
	}
	if callErr != nil {
		payload["error"] = callErr.Error()
	}
	if out.Metadata != nil {
		payload["metadata"] = out.Metadata
	}
	raw, err := marshalRedactedPayload(payload)
	if err != nil {
		raw = json.RawMessage(`{"redaction_error":"mcp observability payload could not be marshaled"}`)
	}
	event := runtime.ObservabilityEvent{
		OrgID:          invocation.OrgID,
		ProductSurface: invocation.ProductSurface,
		RunID:          uuidPtr(runID),
		CapabilityID:   toolName,
		EventType:      "mcp",
		EventName:      "mcp_tool_call",
		Severity:       severityForMCPCall(status, callErr, out),
		TraceID:        runID,
		Payload:        raw,
		Redacted:       true,
		OccurredAt:     time.Now().UTC(),
	}
	_ = h.recorder.RecordObservabilityEvent(context.WithoutCancel(ctx), event)
}

func enforceJSONSize(value any, maxBytes int, label string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%w: %s must be JSON-serializable: %v", ErrValidation, label, err)
	}
	if len(raw) > maxBytes {
		return fmt.Errorf("%w: %s exceeds %d bytes", ErrPayloadTooLarge, label, maxBytes)
	}
	return nil
}

func redactAndLimitPayload(value any, maxBytes int, label string) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be JSON-serializable: %v", ErrExecutionFailed, label, err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("%w: %s must be valid JSON: %v", ErrExecutionFailed, label, err)
	}
	redacted := redactValue(decoded)
	redactedRaw, err := json.Marshal(redacted)
	if err != nil {
		return nil, fmt.Errorf("%w: %s redaction failed: %v", ErrExecutionFailed, label, err)
	}
	if len(redactedRaw) > maxBytes {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrPayloadTooLarge, label, maxBytes)
	}
	return redacted, nil
}

func marshalRedactedPayload(value any) (json.RawMessage, error) {
	redacted, err := redactAndLimitPayload(value, maxMCPArgumentsBytes, "observability_payload")
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(redacted)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "***"
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactValue(item))
		}
		return out
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return "***"
		}
		return redactValue(decoded)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func rateLimitMetadata(err error) map[string]interface{} {
	var rateErr productlimits.RateLimitError
	if !errors.As(err, &rateErr) {
		return nil
	}
	return map[string]interface{}{
		"org_id":          rateErr.Key.OrgID,
		"product_surface": rateErr.Key.ProductSurface,
		"area":            rateErr.Key.Area,
		"limit":           rateErr.Limit.Max,
		"window_ms":       rateErr.Limit.Window.Milliseconds(),
		"retry_after_ms":  rateErr.RetryAfter.Milliseconds(),
	}
}

func severityForMCPCall(status int, callErr error, out toolCallResponse) string {
	if status >= http.StatusInternalServerError {
		return "error"
	}
	if callErr != nil || out.Denied || status == http.StatusTooManyRequests || status == http.StatusRequestEntityTooLarge {
		return "warn"
	}
	return "info"
}

func uuidPtr(value string) *uuid.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || parsed == uuid.Nil {
		return nil
	}
	return &parsed
}

func isRequestBodyTooLarge(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "request body too large")
}

type Executor struct {
	deps Deps
}

func (e *Executor) Execute(ctx context.Context, name string, args map[string]any, identity mcpgovernance.InvocationContext) (any, error) {
	switch strings.TrimSpace(name) {
	case "axis.products.list":
		if e.deps.Products == nil {
			return nil, fmt.Errorf("%w: products usecase missing", ErrNotImplemented)
		}
		items, err := e.deps.Products.ListProducts(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"products": items}, nil
	case "axis.products.get":
		if e.deps.Products == nil {
			return nil, fmt.Errorf("%w: products usecase missing", ErrNotImplemented)
		}
		productSurface, err := requiredProductSurface(args, identity)
		if err != nil {
			return nil, err
		}
		return e.deps.Products.GetProduct(ctx, productSurface)
	case "axis.installations.resolve":
		if e.deps.Products == nil {
			return nil, fmt.Errorf("%w: products usecase missing", ErrNotImplemented)
		}
		productSurface, err := requiredProductSurface(args, identity)
		if err != nil {
			return nil, err
		}
		return e.deps.Products.ResolveInstallation(ctx, identity.OrgID, productSurface)
	case "axis.capabilities.validate":
		if e.deps.Capabilities == nil {
			return nil, fmt.Errorf("%w: capabilities usecase missing", ErrNotImplemented)
		}
		manifest, err := manifestFromArgs(args)
		if err != nil {
			return nil, err
		}
		checks, errs := e.deps.Capabilities.CheckConformance(ctx, manifest)
		status := capabilities.ConformanceStatusPassed
		if len(errs) > 0 {
			status = capabilities.ConformanceStatusFailed
		}
		return map[string]any{"status": status, "checks": checks, "errors": errs}, nil
	case "axis.capabilities.import":
		if e.deps.Capabilities == nil {
			return nil, fmt.Errorf("%w: capabilities usecase missing", ErrNotImplemented)
		}
		manifest, err := manifestFromArgs(args)
		if err != nil {
			return nil, err
		}
		return e.deps.Capabilities.ImportManifest(ctx, manifest, actorFor(identity))
	case "axis.traces.replay":
		if e.deps.Traces == nil {
			return nil, fmt.Errorf("%w: trace replay repository missing", ErrNotImplemented)
		}
		runID, err := uuid.Parse(requiredString(args, "run_id"))
		if err != nil || runID == uuid.Nil {
			return nil, fmt.Errorf("%w: run_id must be a valid uuid", ErrValidation)
		}
		replay, err := e.deps.Traces.GetRunReplay(ctx, runID)
		if err != nil {
			return nil, err
		}
		if replay.Trace.OrgID != identity.OrgID {
			return nil, fmt.Errorf("%w: run trace belongs to a different org", ErrForbidden)
		}
		if identity.ProductSurface != identityctx.DefaultSurface && replay.Trace.ProductSurface != identity.ProductSurface {
			return nil, fmt.Errorf("%w: run trace belongs to a different product_surface", ErrForbidden)
		}
		return replay, nil
	case "axis.costs.summary":
		if e.deps.Costs == nil {
			return nil, fmt.Errorf("%w: cost ledger missing", ErrNotImplemented)
		}
		limit, err := intArg(args, "limit", 100)
		if err != nil {
			return nil, err
		}
		return e.deps.Costs.GetCostSummary(ctx, identity.OrgID, identity.ProductSurface, stringArg(args, "period"), limit)
	case "axis.evals.run":
		if e.deps.Evals == nil {
			return nil, fmt.Errorf("%w: eval runner missing", ErrNotImplemented)
		}
		return e.deps.Evals.RunSuite(ctx, identity.OrgID, identity.ProductSurface, stringArg(args, "suite"), actorFor(identity))
	case "axis.tasks.create":
		if e.deps.Tasks == nil {
			return nil, fmt.Errorf("%w: task creator missing", ErrNotImplemented)
		}
		title := requiredString(args, "title")
		if title == "" {
			return nil, fmt.Errorf("%w: title is required", ErrValidation)
		}
		contextJSON := json.RawMessage(`{}`)
		if raw, ok := args["context"]; ok {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return nil, fmt.Errorf("%w: context must be JSON-serializable", ErrValidation)
			}
			contextJSON = encoded
		}
		return e.deps.Tasks.Create(ctx, tasks.CreateTaskInput{
			OrgID:       identity.OrgID,
			Title:       title,
			Goal:        stringArg(args, "goal"),
			Priority:    stringArg(args, "priority"),
			CreatedBy:   actorFor(identity),
			AssignedTo:  stringArg(args, "assigned_to"),
			Channel:     firstNonEmpty(stringArg(args, "channel"), "mcp"),
			Summary:     stringArg(args, "summary"),
			ContextJSON: contextJSON,
		})
	case "axis.nexus.requests.list":
		if e.deps.Nexus == nil {
			return nil, fmt.Errorf("%w: nexus request lister missing", ErrNotImplemented)
		}
		query, err := nexusListQuery(args)
		if err != nil {
			return nil, err
		}
		status, raw, err := e.deps.Nexus.ListRequestsForOrg(ctx, query, identity.OrgID)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("%w: nexus list requests status %d body %s", ErrExecutionFailed, status, string(raw))
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("%w: decode nexus list response: %v", ErrExecutionFailed, err)
		}
		return decoded, nil
	case "axis.ops.console":
		if e.deps.Ops == nil {
			return nil, fmt.Errorf("%w: ops usecase missing", ErrNotImplemented)
		}
		q, err := opsQuery(args, identity)
		if err != nil {
			return nil, err
		}
		return e.deps.Ops.GetConsole(ctx, q)
	case "axis.ops.alerts":
		if e.deps.Ops == nil {
			return nil, fmt.Errorf("%w: ops usecase missing", ErrNotImplemented)
		}
		q, err := opsQuery(args, identity)
		if err != nil {
			return nil, err
		}
		alerts, err := e.deps.Ops.ListAlerts(ctx, q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"alerts": alerts}, nil
	case "axis.ops.slos":
		if e.deps.Ops == nil {
			return nil, fmt.Errorf("%w: ops usecase missing", ErrNotImplemented)
		}
		q, err := opsQuery(args, identity)
		if err != nil {
			return nil, err
		}
		slos, err := e.deps.Ops.ListSLOs(ctx, q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"slos": slos}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrNotImplemented, name)
	}
}

func inputSchemaForTool(name string) map[string]any {
	base := map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"org_id":          map[string]any{"type": "string", "description": "Axis customer org context."},
			"product_surface": map[string]any{"type": "string", "description": "Axis product surface context."},
		},
	}
	props := base["properties"].(map[string]any)
	switch name {
	case "axis.products.get", "axis.installations.resolve":
		props["product_surface"] = map[string]any{"type": "string"}
		base["required"] = []string{"product_surface"}
	case "axis.capabilities.validate", "axis.capabilities.import":
		props["manifest"] = map[string]any{"type": "object"}
		base["required"] = []string{"manifest"}
	case "axis.traces.replay":
		props["run_id"] = map[string]any{"type": "string"}
		base["required"] = []string{"run_id"}
	case "axis.costs.summary", "axis.ops.console", "axis.ops.alerts", "axis.ops.slos":
		props["period"] = map[string]any{"type": "string"}
		props["limit"] = map[string]any{"type": "integer", "minimum": 1}
	case "axis.evals.run":
		props["suite"] = map[string]any{"type": "string"}
	case "axis.tasks.create":
		props["title"] = map[string]any{"type": "string"}
		props["goal"] = map[string]any{"type": "string"}
		props["priority"] = map[string]any{"type": "string"}
		props["assigned_to"] = map[string]any{"type": "string"}
		props["summary"] = map[string]any{"type": "string"}
		props["context"] = map[string]any{"type": "object"}
		base["required"] = []string{"title"}
	case "axis.nexus.requests.list":
		props["status"] = map[string]any{"type": "string"}
		props["action_type"] = map[string]any{"type": "string"}
		props["limit"] = map[string]any{"type": "integer", "minimum": 1}
	}
	return base
}

func titleForTool(name string) string {
	name = strings.TrimPrefix(name, "axis.")
	parts := strings.Fields(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(name))
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func manifestFromArgs(args map[string]any) (capabilities.Manifest, error) {
	raw := args
	if nested, ok := args["manifest"]; ok {
		rawMap, ok := nested.(map[string]any)
		if !ok {
			return capabilities.Manifest{}, fmt.Errorf("%w: manifest must be an object", ErrValidation)
		}
		raw = rawMap
	}
	var manifest capabilities.Manifest
	if err := decodeValue(raw, &manifest); err != nil {
		return capabilities.Manifest{}, fmt.Errorf("%w: invalid manifest: %v", ErrValidation, err)
	}
	return manifest, nil
}

func opsQuery(args map[string]any, identity mcpgovernance.InvocationContext) (ops.Query, error) {
	limit, err := intArg(args, "limit", 100)
	if err != nil {
		return ops.Query{}, err
	}
	return ops.Query{
		OrgID:          identity.OrgID,
		ProductSurface: identity.ProductSurface,
		Period:         stringArg(args, "period"),
		Limit:          limit,
	}, nil
}

func nexusListQuery(args map[string]any) (string, error) {
	values := url.Values{}
	for _, key := range []string{"status", "action_type"} {
		if value := stringArg(args, key); value != "" {
			values.Set(key, value)
		}
	}
	limit, err := intArg(args, "limit", 100)
	if err != nil {
		return "", err
	}
	values.Set("limit", strconv.Itoa(limit))
	return values.Encode(), nil
}

func requiredProductSurface(args map[string]any, identity mcpgovernance.InvocationContext) (string, error) {
	productSurface := firstNonEmpty(stringArg(args, "product_surface"), identity.ProductSurface)
	if strings.TrimSpace(productSurface) == "" {
		return "", fmt.Errorf("%w: product_surface is required", ErrValidation)
	}
	return productSurface, nil
}

func requiredString(args map[string]any, key string) string {
	return strings.TrimSpace(stringArg(args, key))
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch value := args[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func intArg(args map[string]any, key string, fallback int) (int, error) {
	raw, ok := args[key]
	if !ok || raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "" {
		return fallback, nil
	}
	switch value := raw.(type) {
	case float64:
		if value <= 0 {
			return 0, fmt.Errorf("%w: %s must be positive", ErrValidation, key)
		}
		return int(value), nil
	case int:
		if value <= 0 {
			return 0, fmt.Errorf("%w: %s must be positive", ErrValidation, key)
		}
		return value, nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("%w: %s must be a positive integer", ErrValidation, key)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%w: %s must be a positive integer", ErrValidation, key)
	}
}

func decodeValue(value any, out any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func actorFor(identity mcpgovernance.InvocationContext) string {
	return firstNonEmpty(identity.ActorID, identity.OnBehalfOf, identity.CompanionPrincipal, identityctx.CompanionPrincipal)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func statusForError(err error) int {
	switch {
	case productlimits.IsRateLimited(err):
		return http.StatusTooManyRequests
	case errors.Is(err, ErrPayloadTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrForbidden), errors.Is(err, mcpgovernance.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrValidation), errors.Is(err, mcpgovernance.ErrValidation), errors.Is(err, products.ErrValidation), errors.Is(err, capabilities.ErrInvalidManifest):
		return http.StatusBadRequest
	case errors.Is(err, mcpgovernance.ErrToolNotFound):
		return http.StatusBadRequest
	case errors.Is(err, products.ErrProductNotFound), errors.Is(err, products.ErrInstallationNotFound), errors.Is(err, capabilities.ErrManifestNotFound), errors.Is(err, runtime.ErrTraceNotFound):
		return http.StatusNotFound
	case errors.Is(err, products.ErrProductDisabled), errors.Is(err, products.ErrInstallationDisabled), errors.Is(err, products.ErrInstallationRequired):
		return http.StatusForbidden
	case errors.Is(err, ErrNotImplemented):
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}
