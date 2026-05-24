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
	ListRuntimePolicyAudit(ctx context.Context, orgID string, limit int) ([]RuntimePolicyAuditEntry, error)
	GetRuntimeUsage(ctx context.Context, orgID, period string) (TenantRuntimeUsage, error)
	AddRuntimeUsage(ctx context.Context, orgID, period string, usage RunUsage) error
}

type TenantRuntimePolicy struct {
	OrgID                  string                  `json:"org_id"`
	Enabled                bool                    `json:"enabled"`
	KillSwitch             bool                    `json:"kill_switch"`
	MaxAutonomy            AutonomyLevel           `json:"max_autonomy"`
	AllowedProductSurfaces []string                `json:"allowed_product_surfaces,omitempty"`
	AllowedModels          []string                `json:"allowed_models,omitempty"`
	MonthlyTokenBudget     int64                   `json:"monthly_token_budget,omitempty"`
	MonthlyToolCallBudget  int64                   `json:"monthly_tool_call_budget,omitempty"`
	SettingsVersion        int64                   `json:"settings_version,omitempty"`
	ControlPlane           OrgControlPlaneSettings `json:"control_plane,omitempty"`
	Metadata               map[string]any          `json:"metadata,omitempty"`
	CreatedAt              time.Time               `json:"created_at,omitempty"`
	UpdatedAt              time.Time               `json:"updated_at,omitempty"`
}

type OrgControlPlaneSettings struct {
	EnabledAgents          []string                 `json:"enabled_agents,omitempty"`
	AllowedProfiles        []string                 `json:"allowed_profiles,omitempty"`
	AllowedTools           []string                 `json:"allowed_tools,omitempty"`
	DeniedTools            []string                 `json:"denied_tools,omitempty"`
	AllowedCapabilities    []string                 `json:"allowed_capabilities,omitempty"`
	DeniedCapabilities     []string                 `json:"denied_capabilities,omitempty"`
	AllowedConnectors      []string                 `json:"allowed_connectors,omitempty"`
	DeniedConnectors       []string                 `json:"denied_connectors,omitempty"`
	AllowedModels          []string                 `json:"allowed_models,omitempty"`
	MonthlyCostBudgetCents int64                    `json:"monthly_cost_budget_cents,omitempty"`
	MaxRiskClass           string                   `json:"max_risk_class,omitempty"`
	ApprovalThresholds     map[string]string        `json:"approval_thresholds,omitempty"`
	Retention              OrgRetentionPolicy       `json:"retention,omitempty"`
	Memory                 OrgMemoryPolicy          `json:"memory,omitempty"`
	AgentKillSwitches      map[string]bool          `json:"agent_kill_switches,omitempty"`
	ToolKillSwitches       map[string]bool          `json:"tool_kill_switches,omitempty"`
	ConnectorKillSwitches  map[string]bool          `json:"connector_kill_switches,omitempty"`
	DataIsolation          OrgDataIsolationPolicy   `json:"data_isolation,omitempty"`
	Observability          OrgObservabilitySettings `json:"observability,omitempty"`
	Metadata               map[string]any           `json:"metadata,omitempty"`
}

type OrgRetentionPolicy struct {
	RunTraceDays     int `json:"run_trace_days,omitempty"`
	ToolEvidenceDays int `json:"tool_evidence_days,omitempty"`
	MemoryDays       int `json:"memory_days,omitempty"`
}

type OrgMemoryPolicy struct {
	RetentionDays        int      `json:"retention_days,omitempty"`
	CompactionAfterDays  int      `json:"compaction_after_days,omitempty"`
	RequireProvenance    bool     `json:"require_provenance"`
	MinConfidence        float64  `json:"min_confidence,omitempty"`
	AllowedMemoryKinds   []string `json:"allowed_memory_kinds,omitempty"`
	BlockedMemorySources []string `json:"blocked_memory_sources,omitempty"`
}

type OrgDataIsolationPolicy struct {
	Mode                    string `json:"mode,omitempty"`
	RequireOrgScopedMemory  bool   `json:"require_org_scoped_memory"`
	RequireOrgScopedTools   bool   `json:"require_org_scoped_tools"`
	ForbidCrossOrgRetrieval bool   `json:"forbid_cross_org_retrieval"`
}

type OrgObservabilitySettings struct {
	TraceLevel          string `json:"trace_level,omitempty"`
	RedactionMode       string `json:"redaction_mode,omitempty"`
	ReplayEnabled       bool   `json:"replay_enabled"`
	CapturePrompts      bool   `json:"capture_prompts"`
	CaptureToolPayloads bool   `json:"capture_tool_payloads"`
}

type RuntimePolicyAuditEntry struct {
	ID              string              `json:"id,omitempty"`
	OrgID           string              `json:"org_id"`
	SettingsVersion int64               `json:"settings_version"`
	ChangedBy       string              `json:"changed_by,omitempty"`
	Reason          string              `json:"reason,omitempty"`
	Policy          TenantRuntimePolicy `json:"policy"`
	CreatedAt       time.Time           `json:"created_at,omitempty"`
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
		OrgID:           strings.TrimSpace(orgID),
		Enabled:         true,
		MaxAutonomy:     AutonomyA2,
		SettingsVersion: 0,
		ControlPlane:    defaultOrgControlPlaneSettings(),
		Metadata:        map[string]any{},
	}
}

func normalizeRuntimePolicy(policy TenantRuntimePolicy) TenantRuntimePolicy {
	policy.OrgID = strings.TrimSpace(policy.OrgID)
	policy.AllowedProductSurfaces = normalizeStringList(policy.AllowedProductSurfaces)
	policy.AllowedModels = normalizeStringList(policy.AllowedModels)
	policy.ControlPlane = normalizeOrgControlPlaneSettings(policy.ControlPlane)
	if policy.MaxAutonomy == "" {
		policy.MaxAutonomy = AutonomyA2
	}
	if policy.Metadata == nil {
		policy.Metadata = map[string]any{}
	}
	return policy
}

func defaultOrgControlPlaneSettings() OrgControlPlaneSettings {
	return normalizeOrgControlPlaneSettings(OrgControlPlaneSettings{
		MaxRiskClass: "high",
		DataIsolation: OrgDataIsolationPolicy{
			Mode:                    "strict_org",
			RequireOrgScopedMemory:  true,
			RequireOrgScopedTools:   true,
			ForbidCrossOrgRetrieval: true,
		},
		Observability: OrgObservabilitySettings{
			TraceLevel:          "standard",
			RedactionMode:       "strict",
			ReplayEnabled:       true,
			CapturePrompts:      false,
			CaptureToolPayloads: false,
		},
		Memory: OrgMemoryPolicy{
			RequireProvenance: true,
			MinConfidence:     0.5,
		},
	})
}

func normalizeOrgControlPlaneSettings(settings OrgControlPlaneSettings) OrgControlPlaneSettings {
	settings.EnabledAgents = normalizeStringList(settings.EnabledAgents)
	settings.AllowedProfiles = normalizeStringList(settings.AllowedProfiles)
	settings.AllowedTools = normalizeStringList(settings.AllowedTools)
	settings.DeniedTools = normalizeStringList(settings.DeniedTools)
	settings.AllowedCapabilities = normalizeStringList(settings.AllowedCapabilities)
	settings.DeniedCapabilities = normalizeStringList(settings.DeniedCapabilities)
	settings.AllowedConnectors = normalizeStringList(settings.AllowedConnectors)
	settings.DeniedConnectors = normalizeStringList(settings.DeniedConnectors)
	settings.AllowedModels = normalizeStringList(settings.AllowedModels)
	settings.MaxRiskClass = strings.TrimSpace(settings.MaxRiskClass)
	if settings.MaxRiskClass == "" {
		settings.MaxRiskClass = "high"
	}
	settings.ApprovalThresholds = normalizeStringMap(settings.ApprovalThresholds)
	settings.AgentKillSwitches = normalizeBoolMap(settings.AgentKillSwitches)
	settings.ToolKillSwitches = normalizeBoolMap(settings.ToolKillSwitches)
	settings.ConnectorKillSwitches = normalizeBoolMap(settings.ConnectorKillSwitches)
	if settings.DataIsolation.Mode == "" {
		settings.DataIsolation.Mode = "strict_org"
	}
	if !settings.DataIsolation.RequireOrgScopedMemory && !settings.DataIsolation.RequireOrgScopedTools && !settings.DataIsolation.ForbidCrossOrgRetrieval {
		settings.DataIsolation.RequireOrgScopedMemory = true
		settings.DataIsolation.RequireOrgScopedTools = true
		settings.DataIsolation.ForbidCrossOrgRetrieval = true
	}
	if settings.Observability.TraceLevel == "" {
		settings.Observability.TraceLevel = "standard"
	}
	if settings.Observability.RedactionMode == "" {
		settings.Observability.RedactionMode = "strict"
	}
	if settings.Memory.MinConfidence == 0 {
		settings.Memory.MinConfidence = 0.5
	}
	settings.Memory.AllowedMemoryKinds = normalizeStringList(settings.Memory.AllowedMemoryKinds)
	settings.Memory.BlockedMemorySources = normalizeStringList(settings.Memory.BlockedMemorySources)
	if settings.Metadata == nil {
		settings.Metadata = map[string]any{}
	}
	return settings
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
	if len(policy.ControlPlane.AllowedModels) > 0 && !stringListAllows(policy.ControlPlane.AllowedModels, model) {
		out.Event = &GuardrailEvent{Type: "org_control_plane", Target: "model", Reason: fmt.Sprintf("model %q is not allowed by customer org control plane", model)}
		out.Reply = "El modelo configurado para Companion no está permitido por la configuración de esta organización."
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
	if event := applyControlPlaneToRoute(policy.ControlPlane, &route); event != nil {
		out.Event = event
		out.Reply = "No puedo operar con el perfil o las herramientas solicitadas bajo la configuración actual de esta organización."
		return out
	}
	out.Route = route
	return out
}

func applyControlPlaneToRoute(settings OrgControlPlaneSettings, route *AgentRoute) *GuardrailEvent {
	settings = normalizeOrgControlPlaneSettings(settings)
	profileID := strings.TrimSpace(route.Profile.ID)
	if profileID != "" {
		if settings.AgentKillSwitches[profileID] {
			return &GuardrailEvent{Type: "org_control_plane", Target: "agent:" + profileID, Reason: "agent kill switch is active"}
		}
		if len(settings.EnabledAgents) > 0 && !stringListAllows(settings.EnabledAgents, profileID) {
			return &GuardrailEvent{Type: "org_control_plane", Target: "agent:" + profileID, Reason: "agent is not enabled for this customer org"}
		}
		if len(settings.AllowedProfiles) > 0 && !stringListAllows(settings.AllowedProfiles, profileID) {
			return &GuardrailEvent{Type: "org_control_plane", Target: "profile:" + profileID, Reason: "profile is not allowed for this customer org"}
		}
	}
	route.AllowedTools = filterAllowedToolsForControlPlane(route.AllowedTools, settings)
	route.Profile.AllowedTools = append([]string(nil), route.AllowedTools...)
	if route.Profile.AllowedCapabilities != nil {
		route.Profile.AllowedCapabilities = filterStringSet(route.Profile.AllowedCapabilities, settings.AllowedCapabilities, settings.DeniedCapabilities, nil)
	}
	return nil
}

func filterAllowedToolsForControlPlane(tools []string, settings OrgControlPlaneSettings) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		if settings.ToolKillSwitches[tool] {
			continue
		}
		if len(settings.AllowedTools) > 0 && !stringListAllows(settings.AllowedTools, tool) {
			continue
		}
		if stringListAllows(settings.DeniedTools, tool) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func filterStringSet(values, allowed, denied []string, kill map[string]bool) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if kill != nil && kill[value] {
			continue
		}
		if len(allowed) > 0 && !stringListAllows(allowed, value) {
			continue
		}
		if stringListAllows(denied, value) {
			continue
		}
		out = append(out, value)
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

func normalizeStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeBoolMap(values map[string]bool) map[string]bool {
	out := make(map[string]bool, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return map[string]bool{}
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
