package mcpgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/nexusclient"
)

var (
	ErrValidation             = errors.New("mcp governance validation failed")
	ErrForbidden              = errors.New("mcp governance forbidden")
	ErrNexusDecision          = errors.New("mcp nexus decision failed")
	ErrApprovalPolicyRequired = errors.New("mcp approval policy required")
)

type NexusSubmitter interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error)
}

type InvocationContext struct {
	OrgID              string   `json:"org_id"`
	ProductSurface     string   `json:"product_surface"`
	ActorID            string   `json:"actor_id"`
	ActorType          string   `json:"actor_type,omitempty"`
	OnBehalfOf         string   `json:"on_behalf_of,omitempty"`
	ServicePrincipal   bool     `json:"service_principal"`
	Scopes             []string `json:"scopes"`
	CompanionPrincipal string   `json:"companion_principal,omitempty"`
}

type DecisionInput struct {
	ToolName         string
	Context          InvocationContext
	Arguments        map[string]any
	IdempotencyKey   string
	RunID            string
	ToolInvocationID string
	Reason           string
}

type Decision struct {
	Tool            ToolDefinition `json:"tool"`
	RequestID       string         `json:"request_id,omitempty"`
	Decision        string         `json:"decision"`
	Status          string         `json:"status"`
	RiskLevel       string         `json:"risk_level,omitempty"`
	DecisionReason  string         `json:"decision_reason,omitempty"`
	CanExecute      bool           `json:"can_execute"`
	PendingApproval bool           `json:"pending_approval"`
	Denied          bool           `json:"denied"`
}

type Gateway struct {
	registry *Registry
	nexus    NexusSubmitter
}

func NewGateway(registry *Registry, nexus NexusSubmitter) *Gateway {
	return &Gateway{registry: registry, nexus: nexus}
}

func (g *Gateway) Authorize(ctx context.Context, in DecisionInput) (Decision, error) {
	if g == nil || g.registry == nil {
		return Decision{}, fmt.Errorf("%w: tool registry is not configured", ErrValidation)
	}
	if g.nexus == nil {
		return Decision{}, fmt.Errorf("%w: nexus submitter is not configured", ErrValidation)
	}
	tool, ok := g.registry.Get(in.ToolName)
	if !ok {
		return Decision{}, fmt.Errorf("%w: %s", ErrToolNotFound, normalizeToolName(in.ToolName))
	}
	identity := normalizeInvocationContext(in.Context)
	if err := validateInvocationContext(identity); err != nil {
		return Decision{}, err
	}
	if missing := missingScopes(identity.Scopes, tool.RequiredScopes); len(missing) > 0 {
		return Decision{}, fmt.Errorf("%w: missing scopes %s", ErrForbidden, strings.Join(missing, ","))
	}

	arguments := sanitizePayload(in.Arguments)
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("axis-mcp:%s", uuid.NewString())
	}
	payloadHash, err := hashPayload(arguments)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: invalid arguments payload: %v", ErrValidation, err)
	}
	runID := firstNonEmpty(in.RunID, uuid.NewString())
	toolInvocationID := firstNonEmpty(in.ToolInvocationID, uuid.NewString())
	binding := actionBinding(tool, identity, arguments, idempotencyKey, runID, toolInvocationID, payloadHash)
	resp, err := g.nexus.SubmitRequest(ctx, idempotencyKey, nexusclient.SubmitRequestBody{
		RequesterType:  firstNonEmpty(identity.ActorType, requesterType(identity)),
		RequesterID:    firstNonEmpty(identity.CompanionPrincipal, identity.ActorID),
		RequesterName:  firstNonEmpty(identity.OnBehalfOf, identity.ActorID),
		ActionType:     tool.NexusActionType,
		TargetSystem:   TargetSystemAxisMCP,
		TargetResource: tool.Name,
		ActionBinding:  binding,
		Params: map[string]any{
			"org_id":              identity.OrgID,
			"product_surface":     identity.ProductSurface,
			"operation":           tool.Name,
			"tool_name":           tool.Name,
			"payload":             arguments,
			"action_binding":      binding,
			"actor_id":            identity.ActorID,
			"actor_type":          firstNonEmpty(identity.ActorType, requesterType(identity)),
			"on_behalf_of":        identity.OnBehalfOf,
			"service_principal":   identity.ServicePrincipal,
			"required_scopes":     tool.RequiredScopes,
			"risk_level":          tool.RiskLevel,
			"side_effect_type":    tool.SideEffectType,
			"approval_required":   tool.ApprovalRequired,
			"target_system":       TargetSystemAxisMCP,
			"target_resource":     tool.Name,
			"companion_principal": identity.CompanionPrincipal,
		},
		Reason: firstNonEmpty(in.Reason, fmt.Sprintf("MCP invocation of %s", tool.Name)),
		Context: fmt.Sprintf(
			"org_id=%s product_surface=%s tool=%s side_effect_type=%s approval_required=%t",
			identity.OrgID,
			identity.ProductSurface,
			tool.Name,
			tool.SideEffectType,
			tool.ApprovalRequired,
		),
	})
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrNexusDecision, err)
	}
	return classifyDecision(tool, resp)
}

func normalizeInvocationContext(ctx InvocationContext) InvocationContext {
	ctx.OrgID = strings.TrimSpace(ctx.OrgID)
	ctx.ProductSurface = strings.ToLower(strings.TrimSpace(ctx.ProductSurface))
	ctx.ActorID = strings.TrimSpace(ctx.ActorID)
	ctx.ActorType = strings.ToLower(strings.TrimSpace(ctx.ActorType))
	ctx.OnBehalfOf = strings.TrimSpace(ctx.OnBehalfOf)
	ctx.CompanionPrincipal = strings.TrimSpace(ctx.CompanionPrincipal)
	ctx.Scopes = cleanList(ctx.Scopes)
	return ctx
}

func validateInvocationContext(ctx InvocationContext) error {
	if ctx.OrgID == "" {
		return fmt.Errorf("%w: org_id is required", ErrValidation)
	}
	if ctx.ProductSurface == "" {
		return fmt.Errorf("%w: product_surface is required", ErrValidation)
	}
	if ctx.ActorID == "" && ctx.CompanionPrincipal == "" {
		return fmt.Errorf("%w: actor_id or companion_principal is required", ErrValidation)
	}
	return nil
}

func missingScopes(actual, required []string) []string {
	owned := make(map[string]bool, len(actual))
	for _, scope := range actual {
		owned[scope] = true
	}
	missing := make([]string, 0)
	for _, scope := range required {
		if !owned[scope] {
			missing = append(missing, scope)
		}
	}
	return missing
}

func requesterType(ctx InvocationContext) string {
	if ctx.ServicePrincipal {
		return "service"
	}
	if ctx.ActorType != "" {
		return ctx.ActorType
	}
	return "agent"
}

func actionBinding(tool ToolDefinition, identity InvocationContext, arguments map[string]any, idempotencyKey, runID, toolInvocationID, payloadHash string) map[string]any {
	actorType := requesterType(identity)
	return map[string]any{
		"schema_version":     nexusclient.ToolIntentSchemaVersion,
		"org_id":             identity.OrgID,
		"actor_id":           firstNonEmpty(identity.CompanionPrincipal, identity.ActorID),
		"actor_type":         actorType,
		"product_surface":    identity.ProductSurface,
		"run_id":             runID,
		"tool_invocation_id": toolInvocationID,
		"capability_id":      tool.Name,
		"operation":          tool.Name,
		"target_system":      TargetSystemAxisMCP,
		"target_resource":    tool.Name,
		"payload_hash":       payloadHash,
		"idempotency_key":    idempotencyKey,
		"tool_name":          tool.Name,
		"on_behalf_of":       identity.OnBehalfOf,
		"risk_level":         tool.RiskLevel,
		"side_effect_type":   tool.SideEffectType,
		"approval_required":  tool.ApprovalRequired,
		"required_scopes":    tool.RequiredScopes,
		"redaction_applied":  true,
		"redacted_arguments": arguments,
	}
}

func classifyDecision(tool ToolDefinition, resp nexusclient.SubmitResponse) (Decision, error) {
	status := strings.ToLower(strings.TrimSpace(resp.Status))
	decision := strings.ToLower(strings.TrimSpace(resp.Decision))
	if status == "" {
		status = statusFromDecision(decision)
	}
	out := Decision{
		Tool:           tool,
		RequestID:      resp.RequestID,
		Decision:       decision,
		Status:         status,
		RiskLevel:      resp.RiskLevel,
		DecisionReason: resp.DecisionReason,
	}
	switch status {
	case nexusclient.StatusAllowed:
		if tool.ApprovalRequired {
			return Decision{}, fmt.Errorf("%w: %s returned allowed for approval-required tool %s", ErrApprovalPolicyRequired, TargetSystemAxisMCP, tool.Name)
		}
		out.CanExecute = true
		return out, nil
	case nexusclient.StatusApproved, nexusclient.StatusExecuted:
		out.CanExecute = true
		return out, nil
	case nexusclient.StatusPendingApproval, nexusclient.StatusPending:
		out.PendingApproval = true
		return out, nil
	case nexusclient.StatusDenied, nexusclient.StatusRejected, nexusclient.StatusExpired, nexusclient.StatusCancelled:
		out.Denied = true
		return out, nil
	default:
		return Decision{}, fmt.Errorf("%w: unsupported nexus status %q for tool %s", ErrNexusDecision, status, tool.Name)
	}
}

func statusFromDecision(decision string) string {
	switch decision {
	case nexusclient.DecisionAllow:
		return nexusclient.StatusAllowed
	case nexusclient.DecisionDeny:
		return nexusclient.StatusDenied
	case nexusclient.DecisionRequireApproval:
		return nexusclient.StatusPendingApproval
	default:
		return ""
	}
}

func sanitizePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		if isSensitiveKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = sanitizeValue(value)
	}
	return out
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizePayload(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeValue(item)
		}
		return out
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

func hashPayload(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
