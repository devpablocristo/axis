package mcpgovernance

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/nexusclient"
)

const (
	ScopeMCPExecute = "companion:mcp:execute"

	TargetSystemAxisMCP = "axis.mcp"
)

var (
	ErrInvalidToolDefinition = errors.New("invalid mcp tool definition")
	ErrToolNotFound          = errors.New("mcp tool not found")

	toolNameExpression = regexp.MustCompile(`^axis\.[a-z0-9][a-z0-9_.-]{1,127}$`)
)

type ToolDefinition struct {
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	RequiredScopes   []string `json:"required_scopes"`
	RiskLevel        string   `json:"risk_level"`
	SideEffectType   string   `json:"side_effect_type"`
	NexusActionType  string   `json:"nexus_action_type"`
	ApprovalRequired bool     `json:"approval_required"`
	Tags             []string `json:"tags,omitempty"`
}

type Registry struct {
	tools  []ToolDefinition
	byName map[string]ToolDefinition
}

func NewRegistry(tools []ToolDefinition) (*Registry, error) {
	reg := &Registry{
		tools:  make([]ToolDefinition, 0, len(tools)),
		byName: make(map[string]ToolDefinition, len(tools)),
	}
	for _, tool := range tools {
		tool = normalizeTool(tool)
		if err := validateTool(tool); err != nil {
			return nil, err
		}
		if _, exists := reg.byName[tool.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate tool %s", ErrInvalidToolDefinition, tool.Name)
		}
		reg.tools = append(reg.tools, tool)
		reg.byName[tool.Name] = tool
	}
	sort.Slice(reg.tools, func(i, j int) bool {
		return reg.tools[i].Name < reg.tools[j].Name
	})
	return reg, nil
}

func NewDefaultRegistry() (*Registry, error) {
	return NewRegistry(DefaultToolDefinitions())
}

func (r *Registry) List() []ToolDefinition {
	if r == nil {
		return nil
	}
	out := make([]ToolDefinition, len(r.tools))
	copy(out, r.tools)
	return out
}

func (r *Registry) Get(name string) (ToolDefinition, bool) {
	if r == nil {
		return ToolDefinition{}, false
	}
	tool, ok := r.byName[normalizeToolName(name)]
	return tool, ok
}

func DefaultToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		readTool("axis.products.list", "List products registered in Axis.", "companion:products:read", "products"),
		readTool("axis.products.get", "Get one product registered in Axis.", "companion:products:read", "products"),
		readTool("axis.installations.resolve", "Resolve an active product installation for an org and product.", "companion:products:read", "installations"),
		readTool("axis.capabilities.validate", "Validate a capability manifest against Axis conformance rules.", "companion:capabilities:read", "capabilities"),
		approvalTool("axis.capabilities.import", "Import a capability manifest into Axis.", capabilities.RiskHigh, capabilities.SideEffectWrite, "companion:capabilities:admin", "capabilities"),
		approvalTool("axis.traces.replay", "Replay an Axis trace for debugging or audit.", capabilities.RiskMedium, capabilities.SideEffectRead, "companion:observability:read", "observability"),
		readTool("axis.costs.summary", "Read Axis cost summary by org and product.", "companion:costs:read", "costs"),
		approvalTool("axis.evals.run", "Run a product eval pack.", capabilities.RiskMedium, capabilities.SideEffectExecute, "companion:evals:admin", "evals"),
		approvalTool("axis.tasks.create", "Create an Axis task from an external agent request.", capabilities.RiskHigh, capabilities.SideEffectWrite, "companion:tasks:write", "tasks"),
		readTool("axis.nexus.requests.list", "List Nexus requests visible to Axis.", "companion:nexus:read", "nexus"),
		readTool("axis.ops.console", "Read the Axis operations console aggregate.", "companion:ops:read", "ops"),
		readTool("axis.ops.alerts", "Read Axis operations alerts.", "companion:ops:read", "ops"),
		readTool("axis.ops.slos", "Read Axis product SLO indicators.", "companion:ops:read", "ops"),
	}
}

func readTool(name, description, scope string, tags ...string) ToolDefinition {
	return ToolDefinition{
		Name:             name,
		Description:      description,
		RequiredScopes:   withMCPExecute(scope),
		RiskLevel:        capabilities.RiskLow,
		SideEffectType:   capabilities.SideEffectRead,
		NexusActionType:  nexusclient.ActionTypeAgentCapabilityInvoke,
		ApprovalRequired: false,
		Tags:             tags,
	}
}

func approvalTool(name, description, riskLevel, sideEffectType, scope string, tags ...string) ToolDefinition {
	return ToolDefinition{
		Name:             name,
		Description:      description,
		RequiredScopes:   withMCPExecute(scope),
		RiskLevel:        riskLevel,
		SideEffectType:   sideEffectType,
		NexusActionType:  nexusclient.ActionTypeAgentCapabilityInvoke,
		ApprovalRequired: true,
		Tags:             tags,
	}
}

func withMCPExecute(scope string) []string {
	return []string{ScopeMCPExecute, strings.TrimSpace(scope)}
}

func normalizeTool(tool ToolDefinition) ToolDefinition {
	tool.Name = normalizeToolName(tool.Name)
	tool.Description = strings.TrimSpace(tool.Description)
	tool.RequiredScopes = cleanList(tool.RequiredScopes)
	tool.RiskLevel = strings.ToLower(firstNonEmpty(tool.RiskLevel, capabilities.RiskLow))
	tool.SideEffectType = strings.ToLower(firstNonEmpty(tool.SideEffectType, capabilities.SideEffectRead))
	tool.NexusActionType = strings.TrimSpace(tool.NexusActionType)
	tool.Tags = cleanList(tool.Tags)
	return tool
}

func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func validateTool(tool ToolDefinition) error {
	if !toolNameExpression.MatchString(tool.Name) {
		return fmt.Errorf("%w: name must match %s", ErrInvalidToolDefinition, toolNameExpression.String())
	}
	if len(tool.RequiredScopes) == 0 {
		return fmt.Errorf("%w: %s requires at least one scope", ErrInvalidToolDefinition, tool.Name)
	}
	if !contains(tool.RequiredScopes, ScopeMCPExecute) {
		return fmt.Errorf("%w: %s must require %s", ErrInvalidToolDefinition, tool.Name, ScopeMCPExecute)
	}
	if !oneOf(tool.RiskLevel, capabilities.RiskLow, capabilities.RiskMedium, capabilities.RiskHigh, capabilities.RiskCritical) {
		return fmt.Errorf("%w: %s has invalid risk_level", ErrInvalidToolDefinition, tool.Name)
	}
	if !oneOf(tool.SideEffectType, capabilities.SideEffectRead, capabilities.SideEffectWrite, capabilities.SideEffectNotify, capabilities.SideEffectExecute) {
		return fmt.Errorf("%w: %s has invalid side_effect_type", ErrInvalidToolDefinition, tool.Name)
	}
	if strings.TrimSpace(tool.NexusActionType) == "" {
		return fmt.Errorf("%w: %s must declare nexus_action_type", ErrInvalidToolDefinition, tool.Name)
	}
	if tool.SideEffectType != capabilities.SideEffectRead && !tool.ApprovalRequired {
		return fmt.Errorf("%w: side-effect tool %s must require approval", ErrInvalidToolDefinition, tool.Name)
	}
	return nil
}

func cleanList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
