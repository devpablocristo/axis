package mcpgovernance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxRequestBytes   = 1 << 20
	maxArgumentsBytes = 256 << 10
	maxResultBytes    = 1 << 20
)

type UseCasesPort interface {
	GetPolicy(context.Context, string) (Policy, error)
	PutPolicy(context.Context, string, string, string, PutPolicyInput) (Policy, error)
	ListPolicyAudit(context.Context, string, string, int) ([]PolicyAudit, error)
	ListInvocations(context.Context, string, string, uuid.UUID, int) ([]InvocationAudit, error)
	ResolveContext(context.Context, ContextRequest) (InvocationContext, error)
	ListTools(context.Context, ContextRequest) ([]Tool, error)
	CallTool(context.Context, Invocation) (InvocationResult, error)
}

type Handler struct{ ucs UseCasesPort }

func NewHandler(ucs UseCasesPort) *Handler { return &Handler{ucs: ucs} }

func (h *Handler) MCPRoutes(router gin.IRouter) { router.POST("/mcp", h.RPC) }

func (h *Handler) AdminRoutes(router gin.IRouter) {
	router.GET("/runtime/mcp-policy", h.GetPolicy)
	router.PUT("/runtime/mcp-policy", h.PutPolicy)
	router.GET("/runtime/mcp-policy/audit", h.ListPolicyAudit)
	router.GET("/runtime/mcp-invocations", h.ListInvocations)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Meta      struct {
		IdempotencyKey string `json:"idempotency_key"`
	} `json:"_meta"`
}

func (h *Handler) RPC(c *gin.Context) {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes))
	decoder.UseNumber()
	var req rpcRequest
	if err := decoder.Decode(&req); err != nil {
		h.writeRPCError(c, nil, -32700, "Parse error", nil)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		h.writeRPCError(c, nil, -32700, "Parse error", nil)
		return
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		h.writeRPCError(c, rawID(req.ID), -32600, "Invalid Request", nil)
		return
	}
	switch req.Method {
	case "initialize":
		h.writeRPCResult(c, rawID(req.ID), map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "axis-companion-v2", "title": "Axis Companion MCP", "version": "2.0.0"},
		})
	case "tools/list":
		request, err := contextRequest(c)
		if err != nil {
			h.writeRPCError(c, rawID(req.ID), -32602, err.Error(), nil)
			return
		}
		tools, err := h.ucs.ListTools(c.Request.Context(), request)
		if err != nil {
			h.writeRPCDomainError(c, rawID(req.ID), err)
			return
		}
		h.writeRPCResult(c, rawID(req.ID), map[string]any{"tools": tools})
	case "tools/call":
		request, err := contextRequest(c)
		if err != nil {
			h.writeRPCError(c, rawID(req.ID), -32602, err.Error(), nil)
			return
		}
		var params callParams
		if len(req.Params) == 0 || json.Unmarshal(req.Params, &params) != nil || strings.TrimSpace(params.Name) == "" {
			h.writeRPCError(c, rawID(req.ID), -32602, "tool name and valid params are required", nil)
			return
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		if raw, _ := json.Marshal(params.Arguments); len(raw) > maxArgumentsBytes {
			h.writeRPCError(c, rawID(req.ID), -32602, "tool arguments exceed size limit", nil)
			return
		}
		resolved, err := h.ucs.ResolveContext(c.Request.Context(), request)
		if err != nil {
			h.writeRPCDomainError(c, rawID(req.ID), err)
			return
		}
		idempotencyKey := strings.TrimSpace(c.GetHeader("X-Idempotency-Key"))
		if idempotencyKey == "" {
			idempotencyKey = strings.TrimSpace(params.Meta.IdempotencyKey)
		}
		out, err := h.ucs.CallTool(c.Request.Context(), Invocation{
			Context: resolved, ToolName: params.Name, Arguments: params.Arguments, IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			h.writeRPCResult(c, rawID(req.ID), map[string]any{
				"content": []map[string]any{{"type": "text", "text": safeToolError(err)}},
				"isError": true,
			})
			return
		}
		text, _ := json.Marshal(out)
		h.writeRPCResult(c, rawID(req.ID), map[string]any{
			"content":           []map[string]any{{"type": "text", "text": string(text)}},
			"structuredContent": out,
			"isError":           false,
		})
	default:
		h.writeRPCError(c, rawID(req.ID), -32601, "Method not found", nil)
	}
}

func (h *Handler) GetPolicy(c *gin.Context) {
	if !ownerOrAdmin(c.GetHeader("X-Axis-Org-Role")) {
		ginmw.Respond(c, domainerr.Forbidden("MCP policy requires an owner or admin"))
		return
	}
	out, err := h.ucs.GetPolicy(c.Request.Context(), orgID(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) PutPolicy(c *gin.Context) {
	var input PutPolicyInput
	if err := ginmw.BindJSON(c, &input); err != nil {
		return
	}
	out, err := h.ucs.PutPolicy(c.Request.Context(), orgID(c), actorID(c), c.GetHeader("X-Axis-Org-Role"), input)
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) ListPolicyAudit(c *gin.Context) {
	out, err := h.ucs.ListPolicyAudit(c.Request.Context(), orgID(c), c.GetHeader("X-Axis-Org-Role"), queryLimit(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func (h *Handler) ListInvocations(c *gin.Context) {
	var virployeeID uuid.UUID
	if raw := strings.TrimSpace(c.Query("virployee_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			ginmw.Respond(c, domainerr.Validation("virployee_id must be a UUID"))
			return
		}
		virployeeID = parsed
	}
	out, err := h.ucs.ListInvocations(c.Request.Context(), orgID(c), c.GetHeader("X-Axis-Org-Role"), virployeeID, queryLimit(c))
	if err != nil {
		ginmw.Respond(c, err)
		return
	}
	ginmw.WriteJSON(c, http.StatusOK, out)
}

func contextRequest(c *gin.Context) (ContextRequest, error) {
	virployeeID, err := requiredHeaderUUID(c, "X-Axis-Virployee-ID")
	if err != nil {
		return ContextRequest{}, err
	}
	subjectID, err := requiredHeaderUUID(c, "X-Axis-Subject-ID")
	if err != nil {
		return ContextRequest{}, err
	}
	var caseID uuid.UUID
	if raw := strings.TrimSpace(c.GetHeader("X-Axis-Case-ID")); raw != "" {
		caseID, err = uuid.Parse(raw)
		if err != nil || caseID == uuid.Nil {
			return ContextRequest{}, errors.New("X-Axis-Case-ID must be a UUID")
		}
	}
	return ContextRequest{
		OrgID: orgID(c), ActorID: actorID(c), ActorRole: strings.TrimSpace(c.GetHeader("X-Axis-Org-Role")),
		VirployeeID: virployeeID, SubjectID: subjectID, CaseID: caseID,
		ProductSurface:       strings.ToLower(strings.TrimSpace(c.GetHeader("X-Axis-Product-Surface"))),
		RepositoryGeneration: strings.TrimSpace(c.GetHeader("X-Axis-Repository-Generation")),
	}, nil
}

func requiredHeaderUUID(c *gin.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(c.GetHeader(name)))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New(name + " must be a UUID")
	}
	return id, nil
}

func (h *Handler) writeRPCResult(c *gin.Context, id, result any) {
	payload := rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > maxResultBytes {
		h.writeRPCError(c, id, -32603, "result exceeds size limit", nil)
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

func (h *Handler) writeRPCError(c *gin.Context, id any, code int, message string, data any) {
	payload := rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message, Data: data}}
	raw, _ := json.Marshal(payload)
	c.Data(http.StatusOK, "application/json", raw)
}

func (h *Handler) writeRPCDomainError(c *gin.Context, id any, err error) {
	code := -32603
	if domainerr.IsValidation(err) {
		code = -32602
	}
	if domainerr.IsForbidden(err) {
		code = -32003
	}
	h.writeRPCError(c, id, code, safeToolError(err), nil)
}

func rawID(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func safeToolError(err error) string {
	switch {
	case domainerr.IsValidation(err), domainerr.IsForbidden(err), domainerr.IsConflict(err), domainerr.IsNotFound(err):
		return err.Error()
	default:
		return "tool invocation failed"
	}
}

func orgID(c *gin.Context) string   { return strings.TrimSpace(c.GetHeader("X-Org-ID")) }
func actorID(c *gin.Context) string { return strings.TrimSpace(c.GetHeader("X-Actor-ID")) }

func queryLimit(c *gin.Context) int {
	value, _ := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	return value
}
