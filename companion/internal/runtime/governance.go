package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrRuntimePolicyNotFound = errors.New("runtime policy not found")

type RuntimeControls interface {
	GetRuntimePolicy(ctx context.Context, orgID string) (TenantRuntimePolicy, error)
	UpsertRuntimePolicy(ctx context.Context, policy TenantRuntimePolicy) (TenantRuntimePolicy, error)
	GetRuntimeUsage(ctx context.Context, orgID, period string) (TenantRuntimeUsage, error)
	AddRuntimeUsage(ctx context.Context, orgID, period string, usage RunUsage) error
}

type TenantRuntimePolicy struct {
	OrgID                  string         `json:"org_id"`
	Enabled                bool           `json:"enabled"`
	KillSwitch             bool           `json:"kill_switch"`
	MaxAutonomy            AutonomyLevel  `json:"max_autonomy"`
	AllowedProductSurfaces []string       `json:"allowed_product_surfaces,omitempty"`
	AllowedModels          []string       `json:"allowed_models,omitempty"`
	MonthlyTokenBudget     int64          `json:"monthly_token_budget,omitempty"`
	MonthlyToolCallBudget  int64          `json:"monthly_tool_call_budget,omitempty"`
	Metadata               map[string]any `json:"metadata,omitempty"`
	CreatedAt              time.Time      `json:"created_at,omitempty"`
	UpdatedAt              time.Time      `json:"updated_at,omitempty"`
}

type TenantRuntimeUsage struct {
	OrgID                   string    `json:"org_id"`
	Period                  string    `json:"period"`
	EstimatedTokens         int64     `json:"estimated_tokens"`
	LLMCalls                int64     `json:"llm_calls"`
	ToolCalls               int64     `json:"tool_calls"`
	ToolErrors              int64     `json:"tool_errors"`
	LastRunAt               time.Time `json:"last_run_at,omitempty"`
	UpdatedAt               time.Time `json:"updated_at,omitempty"`
	MonthlyTokenBudget      int64     `json:"monthly_token_budget,omitempty"`
	MonthlyToolCallBudget   int64     `json:"monthly_tool_call_budget,omitempty"`
	TokenBudgetRemaining    int64     `json:"token_budget_remaining,omitempty"`
	ToolCallBudgetRemaining int64     `json:"tool_call_budget_remaining,omitempty"`
}

type runtimePolicyDecision struct {
	Route AgentRoute
	Event *GuardrailEvent
	Reply string
}

func defaultRuntimePolicy(orgID string) TenantRuntimePolicy {
	return TenantRuntimePolicy{
		OrgID:       strings.TrimSpace(orgID),
		Enabled:     true,
		MaxAutonomy: AutonomyA2,
		Metadata:    map[string]any{},
	}
}

func normalizeRuntimePolicy(policy TenantRuntimePolicy) TenantRuntimePolicy {
	policy.OrgID = strings.TrimSpace(policy.OrgID)
	policy.AllowedProductSurfaces = normalizeStringList(policy.AllowedProductSurfaces)
	policy.AllowedModels = normalizeStringList(policy.AllowedModels)
	if policy.MaxAutonomy == "" {
		policy.MaxAutonomy = AutonomyA2
	}
	if policy.Metadata == nil {
		policy.Metadata = map[string]any{}
	}
	return policy
}

func applyRuntimePolicy(policy TenantRuntimePolicy, usage TenantRuntimeUsage, route AgentRoute, model string) runtimePolicyDecision {
	policy = normalizeRuntimePolicy(policy)
	out := runtimePolicyDecision{Route: route}
	if !policy.Enabled || policy.KillSwitch {
		out.Event = &GuardrailEvent{Type: "tenant_runtime_policy", Target: "runtime", Reason: "companion runtime is disabled for this customer org"}
		out.Reply = "Companion está deshabilitado para esta organización. Pedile a un administrador que revise la política de runtime."
		return out
	}
	if len(policy.AllowedProductSurfaces) > 0 && !stringListAllows(policy.AllowedProductSurfaces, route.Product) {
		out.Event = &GuardrailEvent{Type: "tenant_runtime_policy", Target: "product_surface", Reason: fmt.Sprintf("product surface %q is not allowed for this customer org", route.Product)}
		out.Reply = "No puedo operar sobre esa superficie de producto para esta organización."
		return out
	}
	if len(policy.AllowedModels) > 0 && !stringListAllows(policy.AllowedModels, model) {
		out.Event = &GuardrailEvent{Type: "tenant_runtime_policy", Target: "model", Reason: fmt.Sprintf("model %q is not allowed for this customer org", model)}
		out.Reply = "El modelo configurado para Companion no está permitido por la política de esta organización."
		return out
	}
	if policy.MonthlyTokenBudget > 0 && usage.EstimatedTokens >= policy.MonthlyTokenBudget {
		out.Event = &GuardrailEvent{Type: "tenant_runtime_budget", Target: "tokens", Reason: "monthly token budget exhausted"}
		out.Reply = "La organización alcanzó el presupuesto mensual de tokens para Companion."
		return out
	}
	if policy.MonthlyToolCallBudget > 0 && usage.ToolCalls >= policy.MonthlyToolCallBudget {
		out.Event = &GuardrailEvent{Type: "tenant_runtime_budget", Target: "tools", Reason: "monthly tool call budget exhausted"}
		out.Reply = "La organización alcanzó el presupuesto mensual de ejecución de tools para Companion."
		return out
	}
	if autonomyRankRuntime(policy.MaxAutonomy) < autonomyRankRuntime(route.Autonomy) {
		route.Autonomy = policy.MaxAutonomy
		route.Profile.MaxAutonomy = policy.MaxAutonomy
		out.Route = route
		out.Event = &GuardrailEvent{Type: "tenant_runtime_policy", Target: "autonomy", Reason: fmt.Sprintf("autonomy capped to %s by tenant policy", policy.MaxAutonomy)}
	}
	return out
}

func runtimeUsagePeriod(t time.Time) string {
	return t.UTC().Format("2006-01")
}

func (u *RunUsage) AddInput(text string) {
	u.InputChars += len(text)
	u.EstimatedInputTokens = estimateTokens(u.InputChars)
	u.EstimatedTotalTokens = u.EstimatedInputTokens + u.EstimatedOutputTokens
}

func (u *RunUsage) AddOutput(text string) {
	u.OutputChars += len(text)
	u.EstimatedOutputTokens = estimateTokens(u.OutputChars)
	u.EstimatedTotalTokens = u.EstimatedInputTokens + u.EstimatedOutputTokens
}

func (u *RunUsage) AddLLMCall() {
	u.LLMCalls++
}

func (u *RunUsage) AddToolCall(result string) {
	u.ToolCalls++
	u.AddOutput(result)
	if strings.Contains(strings.ToLower(result), `"error"`) {
		u.ToolErrors++
	}
}

func estimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringListAllows(values []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, allowed := range values {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == value {
			return true
		}
		if strings.HasSuffix(allowed, "*") && strings.HasPrefix(value, strings.TrimSuffix(allowed, "*")) {
			return true
		}
	}
	return false
}

func autonomyRankRuntime(level AutonomyLevel) int {
	switch level {
	case AutonomyA0:
		return 0
	case AutonomyA1:
		return 1
	case AutonomyA2:
		return 2
	case AutonomyA3:
		return 3
	case AutonomyA4:
		return 4
	case AutonomyA5:
		return 5
	default:
		return 2
	}
}
