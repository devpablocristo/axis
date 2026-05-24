package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/agents"
	"github.com/devpablocristo/companion/internal/identityctx"
)

const (
	CompanionPrincipal    = identityctx.CompanionPrincipal
	DefaultProductSurface = identityctx.DefaultSurface
)

type AutonomyLevel string

const (
	AutonomyA0 AutonomyLevel = "A0"
	AutonomyA1 AutonomyLevel = "A1"
	AutonomyA2 AutonomyLevel = "A2"
	AutonomyA3 AutonomyLevel = "A3"
	AutonomyA4 AutonomyLevel = "A4"
	AutonomyA5 AutonomyLevel = "A5"
)

type IdentityChain struct {
	InitiatingUser      string   `json:"initiating_user,omitempty"`
	Tenant              string   `json:"tenant,omitempty"`
	CustomerOrgID       string   `json:"customer_org_id,omitempty"`
	HumanUserID         string   `json:"human_user_id,omitempty"`
	ActorType           string   `json:"actor_type,omitempty"`
	ProductSurface      string   `json:"product_surface,omitempty"`
	TaskID              string   `json:"task_id,omitempty"`
	AuthScopes          []string `json:"auth_scopes,omitempty"`
	CompanionPrincipal  string   `json:"companion_principal"`
	OnBehalfOf          string   `json:"on_behalf_of,omitempty"`
	ServicePrincipal    bool     `json:"service_principal,omitempty"`
	CapabilityPrincipal string   `json:"capability_principal,omitempty"`
	ApprovalActor       string   `json:"approval_actor,omitempty"`
}

type RunTrace struct {
	RunID           string           `json:"run_id"`
	IdentityChain   IdentityChain    `json:"identity_chain"`
	Intent          string           `json:"intent"`
	ProductSurface  string           `json:"product_surface"`
	AutonomyLevel   AutonomyLevel    `json:"autonomy_level"`
	PromptVersion   string           `json:"prompt_version,omitempty"`
	Model           string           `json:"model,omitempty"`
	Usage           RunUsage         `json:"usage,omitempty"`
	GuardrailEvents []GuardrailEvent `json:"guardrail_events,omitempty"`
	ToolCalls       []ToolTrace      `json:"tool_calls,omitempty"`
	StartedAt       time.Time        `json:"started_at"`
	CompletedAt     time.Time        `json:"completed_at,omitempty"`
}

type RunUsage struct {
	LLMCalls              int `json:"llm_calls"`
	InputChars            int `json:"input_chars"`
	OutputChars           int `json:"output_chars"`
	EstimatedInputTokens  int `json:"estimated_input_tokens"`
	EstimatedOutputTokens int `json:"estimated_output_tokens"`
	EstimatedTotalTokens  int `json:"estimated_total_tokens"`
	ToolCalls             int `json:"tool_calls"`
	ToolErrors            int `json:"tool_errors"`
}

type ToolTrace struct {
	Name           string `json:"name"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	Allowed        bool   `json:"allowed"`
	DecisionReason string `json:"decision_reason,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	Error          string `json:"error,omitempty"`
}

type GuardrailEvent struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason"`
}

type AgentRoute struct {
	Intent       string        `json:"intent"`
	Product      string        `json:"product"`
	Autonomy     AutonomyLevel `json:"autonomy"`
	AllowedTools []string      `json:"allowed_tools"`
	Profile      AgentProfile  `json:"profile"`
}

// AgentProfile representa el perfil efectivo del empleado IA para una corrida.
// Es deliberadamente chico: suficiente para auditar autonomía y tools sin
// introducir persistencia ni un registry pesado todavía.
type AgentProfile struct {
	ID                  string        `json:"id"`
	ProductSurface      string        `json:"product_surface"`
	MaxAutonomy         AutonomyLevel `json:"max_autonomy"`
	AllowedTools        []string      `json:"allowed_tools"`
	AllowedCapabilities []string      `json:"allowed_capabilities,omitempty"`
	MemoryPolicy        any           `json:"memory_policy,omitempty"`
	RequiredScopes      []string      `json:"required_scopes,omitempty"`
	Enabled             bool          `json:"enabled"`
	Version             string        `json:"version"`
}

func BuildIdentityChain(userID, orgID, productSurface string, scopes ...string) IdentityChain {
	productSurface = strings.TrimSpace(productSurface)
	if productSurface == "" {
		productSurface = DefaultProductSurface
	}
	return IdentityChain{
		InitiatingUser:     strings.TrimSpace(userID),
		Tenant:             strings.TrimSpace(orgID),
		CustomerOrgID:      strings.TrimSpace(orgID),
		HumanUserID:        strings.TrimSpace(userID),
		ProductSurface:     productSurface,
		AuthScopes:         cleanScopes(scopes),
		CompanionPrincipal: CompanionPrincipal,
	}
}

func BuildIdentityChainFromContext(id identityctx.IdentityContext) IdentityChain {
	id = id.WithProductSurface(id.ProductSurface)
	initiatingUser := id.EffectiveActorID()
	return IdentityChain{
		InitiatingUser:     initiatingUser,
		Tenant:             id.CustomerOrgID,
		CustomerOrgID:      id.CustomerOrgID,
		HumanUserID:        id.HumanUserID,
		ActorType:          id.ActorType,
		ProductSurface:     id.ProductSurface,
		AuthScopes:         cleanScopes(id.Scopes),
		CompanionPrincipal: id.CompanionPrincipal,
		OnBehalfOf:         id.OnBehalfOf,
		ServicePrincipal:   id.ServicePrincipal,
	}
}

// RouteAgent clasifica intent y devuelve la ruta del agente. defaultAutonomy
// puede ser "" (vacío), en cuyo caso se asume A2.
func RouteAgent(message, productSurface string, toolkit *ToolKit, identity IdentityChain, defaultAutonomy AutonomyLevel) AgentRoute {
	intent := classifyIntent(message)
	autonomy := defaultAutonomy
	if autonomy == "" {
		autonomy = AutonomyA2
	}
	var availableTools []string
	if toolkit != nil {
		schemas := toolkit.SchemasFor(identity, intent)
		availableTools = make([]string, 0, len(schemas))
		for _, schema := range schemas {
			name := strings.TrimSpace(schema.Name)
			if name == "" {
				continue
			}
			availableTools = append(availableTools, name)
		}
	}
	if strings.TrimSpace(productSurface) == "" {
		productSurface = DefaultProductSurface
	}
	profile := agents.DefaultRegistry().Resolve(productSurface, intent, string(autonomy), identity.AuthScopes, availableTools)
	autonomy = AutonomyLevel(profile.MaxAutonomy)
	return AgentRoute{
		Intent:       intent,
		Product:      productSurface,
		Autonomy:     autonomy,
		AllowedTools: append([]string(nil), profile.AllowedTools...),
		Profile: AgentProfile{
			ID:                  profile.ID,
			ProductSurface:      profile.ProductSurface,
			MaxAutonomy:         autonomy,
			AllowedTools:        append([]string(nil), profile.AllowedTools...),
			AllowedCapabilities: append([]string(nil), profile.AllowedCapabilities...),
			MemoryPolicy:        profile.MemoryPolicy,
			RequiredScopes:      append([]string(nil), profile.RequiredScopes...),
			Enabled:             profile.Enabled,
			Version:             profile.Version,
		},
	}
}

func cleanScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func classifyIntent(message string) string {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "aprobar"), strings.Contains(text, "rechazar"), strings.Contains(text, "approval"):
		return "nexus.nexus"
	case strings.Contains(text, "record"), strings.Contains(text, "memor"):
		return "memory"
	case strings.Contains(text, "alerta"), strings.Contains(text, "watcher"):
		return "operations.watchers"
	case strings.Contains(text, "política"), strings.Contains(text, "policy"):
		return "nexus.policy"
	default:
		return "general.assist"
	}
}

func CheckPromptInjection(input string) *GuardrailEvent {
	normalized := strings.ToLower(input)
	suspicious := []string{
		"ignore previous instructions",
		"ignora las instrucciones anteriores",
		"olvida tus instrucciones",
		"reveal system prompt",
		"muestra el prompt",
		"exfiltrate",
	}
	for _, token := range suspicious {
		if strings.Contains(normalized, token) {
			return &GuardrailEvent{Type: "prompt_injection", Target: "message", Reason: "input contains instruction override pattern"}
		}
	}
	return nil
}

func ValidateToolPolicy(toolName string, args json.RawMessage, identity IdentityChain, route AgentRoute, toolkit *ToolKit) *GuardrailEvent {
	if event := CheckPromptInjection(string(args)); event != nil {
		event.Target = "tool_args:" + toolName
		return event
	}
	if !routeAllowsTool(route, toolName) {
		return &GuardrailEvent{Type: "tool_policy", Target: "tool:" + toolName, Reason: "tool is not allowed for the current agent route"}
	}
	if toolkit != nil && !toolkit.CanUseTool(toolName, identity) {
		return &GuardrailEvent{Type: "tool_policy", Target: "tool:" + toolName, Reason: "tool requires customer org, user, or scopes not present in this request"}
	}
	return nil
}

func routeAllowsTool(route AgentRoute, toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	for _, allowed := range route.AllowedTools {
		allowed = strings.TrimSpace(allowed)
		if allowed == toolName {
			return true
		}
		if strings.HasSuffix(allowed, "*") && strings.HasPrefix(toolName, strings.TrimSuffix(allowed, "*")) {
			return true
		}
	}
	return false
}

func filterSchemasForRoute(schemas []ToolSchema, route AgentRoute) []ToolSchema {
	out := make([]ToolSchema, 0, len(schemas))
	for _, schema := range schemas {
		if routeAllowsTool(route, schema.Name) {
			out = append(out, schema)
		}
	}
	return out
}

func runtimeSummary(identity IdentityChain, route AgentRoute) string {
	return fmt.Sprintf(`- Identidad: %s.
- Customer org: %s.
- Usuario iniciador: %s.
- On behalf of: %s.
- Superficie: %s.
- Task actual: %s.
- Intención clasificada: %s.
- Autonomía máxima efectiva: %s.
- Si hay task actual y el trabajo requiere varios pasos, mantené actualizado el plan durable con las tools de planner.
- Regla dura: podés decidir, recomendar y proponer; no ejecutes writes sensibles ni approvals como acción autónoma.
- Toda tool debe respetar customer org, permisos, trazas y guardrails.`,
		identity.CompanionPrincipal,
		emptyAsUnknown(identity.CustomerOrgID),
		emptyAsUnknown(identity.InitiatingUser),
		emptyAsUnknown(identity.OnBehalfOf),
		route.Product,
		emptyAsUnknown(identity.TaskID),
		route.Intent,
		route.Autonomy,
	)
}

func emptyAsUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
