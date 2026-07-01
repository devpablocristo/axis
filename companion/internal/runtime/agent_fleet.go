package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion/internal/agentprofiles"
)

type AgentResolver interface {
	ResolveRuntimeAgent(ctx context.Context, orgID, productSurface, agentID string) (RuntimeAgentConfig, error)
}

type VirployeeResolver interface {
	ResolveRuntimeVirployee(ctx context.Context, tenantID, orgID, productSurface, virployeeID string) (RuntimeVirployeeConfig, error)
}

type AgentProfileResolver interface {
	ResolveRuntimeAgentProfile(ctx context.Context, profileID string) (RuntimeAgentProfileConfig, error)
}

type RuntimeAgentConfig struct {
	AgentID             string         `json:"agent_id"`
	ProfileID           string         `json:"profile_id,omitempty"`
	Role                string         `json:"role,omitempty"`
	Status              string         `json:"status,omitempty"`
	LifecycleStatus     string         `json:"lifecycle_status,omitempty"`
	ReviewStatus        string         `json:"review_status,omitempty"`
	MaxAutonomy         AutonomyLevel  `json:"max_autonomy,omitempty"`
	AllowedTools        []string       `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string       `json:"allowed_capabilities,omitempty"`
	MemoryScopeID       string         `json:"memory_scope_id,omitempty"`
	SharedMemoryPolicy  map[string]any `json:"shared_memory_policy,omitempty"`
	Limits              map[string]any `json:"limits,omitempty"`
	SLA                 map[string]any `json:"sla,omitempty"`
	Version             int64          `json:"version,omitempty"`
}

type RuntimeVirployeeConfig struct {
	VirployeeID   string        `json:"virployee_id"`
	TenantID      string        `json:"tenant_id"`
	Name          string        `json:"name,omitempty"`
	Status        string        `json:"status,omitempty"`
	ProfileID     string        `json:"profile_id"`
	Autonomy      AutonomyLevel `json:"autonomy,omitempty"`
	CapabilityIDs []string      `json:"capability_ids,omitempty"`
	MemoryID      string        `json:"memory_id,omitempty"`
}

type RuntimeAgentProfileConfig struct {
	ProfileID           string           `json:"profile_id"`
	FamilyID            string           `json:"family_id,omitempty"`
	VersionLabel        string           `json:"version_label,omitempty"`
	Name                string           `json:"name,omitempty"`
	Description         string           `json:"description,omitempty"`
	SystemPrompt        string           `json:"system_prompt,omitempty"`
	MaxAutonomy         AutonomyLevel    `json:"max_autonomy,omitempty"`
	AllowedTools        []string         `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string         `json:"allowed_capabilities,omitempty"`
	MemoryPolicy        map[string]any   `json:"memory_policy,omitempty"`
	LLM                 RuntimeLLMConfig `json:"llm,omitempty"`
	Enabled             bool             `json:"enabled"`
	Archived            bool             `json:"archived,omitempty"`
	SnapshotID          string           `json:"snapshot_id,omitempty"`
}

// RuntimeLLMConfig son los parámetros del LLM resueltos desde el LLMConfig de un
// perfil de agente. Los valores vacíos/cero indican "usar el default del runtime".
type RuntimeLLMConfig struct {
	Model       string  `json:"model,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

func applyRuntimeAgent(route AgentRoute, agent RuntimeAgentConfig) (AgentRoute, *GuardrailEvent) {
	agent.AgentID = strings.TrimSpace(agent.AgentID)
	if agent.AgentID == "" {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent", Reason: "agent_id is required"}
	}
	if strings.EqualFold(strings.TrimSpace(agent.Status), "disabled") {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent:" + agent.AgentID, Reason: "agent is disabled"}
	}
	if lifecycle := strings.TrimSpace(agent.LifecycleStatus); lifecycle != "" && !strings.EqualFold(lifecycle, "active") {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent:" + agent.AgentID, Reason: "agent lifecycle is not active"}
	}
	if review := strings.TrimSpace(agent.ReviewStatus); review != "" && !strings.EqualFold(review, "approved") {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent:" + agent.AgentID, Reason: "agent is not approved"}
	}
	profileID := strings.TrimSpace(agent.ProfileID)
	if profileID == "" || profileID == agentprofiles.UnprofiledProfileID {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent:" + agent.AgentID, Reason: "agent profile is required"}
	}
	route.Profile.ID = profileID
	route.Profile.AgentID = agent.AgentID
	route.Profile.Role = strings.TrimSpace(agent.Role)
	route.Profile.MemoryScopeID = strings.TrimSpace(agent.MemoryScopeID)
	if agent.Version > 0 {
		route.Profile.Version = fmt.Sprintf("%s#%d", route.Profile.Version, agent.Version)
	}
	if agent.MaxAutonomy != "" && autonomyRankRuntime(agent.MaxAutonomy) < autonomyRankRuntime(route.Autonomy) {
		route.Autonomy = agent.MaxAutonomy
		route.Profile.MaxAutonomy = agent.MaxAutonomy
	}
	if len(agent.AllowedTools) > 0 {
		route.AllowedTools = intersectRuntimePatterns(route.AllowedTools, agent.AllowedTools)
		route.Profile.AllowedTools = append([]string(nil), route.AllowedTools...)
	}
	if len(agent.AllowedCapabilities) > 0 {
		route.Profile.AllowedCapabilities = intersectRuntimePatterns(route.Profile.AllowedCapabilities, agent.AllowedCapabilities)
	}
	return route, nil
}

func applyRuntimeVirployee(route AgentRoute, virployee RuntimeVirployeeConfig) (AgentRoute, *GuardrailEvent) {
	virployee.VirployeeID = strings.TrimSpace(virployee.VirployeeID)
	if virployee.VirployeeID == "" {
		return route, &GuardrailEvent{Type: "virployee", Target: "virployee", Reason: "virployee_id is required"}
	}
	if !strings.EqualFold(strings.TrimSpace(virployee.Status), "active") {
		return route, &GuardrailEvent{Type: "virployee", Target: "virployee:" + virployee.VirployeeID, Reason: "virployee is not active"}
	}
	profileID := strings.TrimSpace(virployee.ProfileID)
	if profileID == "" || profileID == agentprofiles.UnprofiledProfileID {
		return route, &GuardrailEvent{Type: "virployee", Target: "virployee:" + virployee.VirployeeID, Reason: "virployee profile is required"}
	}
	route.Profile.ID = profileID
	route.Profile.Role = strings.TrimSpace(virployee.Name)
	if virployee.MemoryID != "" {
		route.Profile.MemoryScopeID = strings.TrimSpace(virployee.MemoryID)
	}
	if virployee.Autonomy != "" && autonomyRankRuntime(virployee.Autonomy) < autonomyRankRuntime(route.Autonomy) {
		route.Autonomy = virployee.Autonomy
		route.Profile.MaxAutonomy = virployee.Autonomy
	}
	if len(virployee.CapabilityIDs) > 0 {
		route.Profile.AllowedCapabilities = intersectRuntimePatterns(route.Profile.AllowedCapabilities, virployee.CapabilityIDs)
	}
	return route, nil
}

func applyRuntimeAgentProfile(route AgentRoute, profile RuntimeAgentProfileConfig) (AgentRoute, *GuardrailEvent) {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	if profile.ProfileID == "" {
		return route, &GuardrailEvent{Type: "agent_profile", Target: "profile", Reason: "profile_id is required"}
	}
	if !profile.Enabled {
		return route, &GuardrailEvent{Type: "agent_profile", Target: "profile:" + profile.ProfileID, Reason: "profile is disabled"}
	}
	if profile.Archived {
		return route, &GuardrailEvent{Type: "agent_profile", Target: "profile:" + profile.ProfileID, Reason: "profile is archived"}
	}
	if strings.TrimSpace(profile.SystemPrompt) == "" {
		return route, &GuardrailEvent{Type: "agent_profile", Target: "profile:" + profile.ProfileID, Reason: "profile system_prompt is required"}
	}
	route.Profile.ID = profile.ProfileID
	route.Profile.SystemPrompt = strings.TrimSpace(profile.SystemPrompt)
	route.Profile.ProfileSnapshotID = strings.TrimSpace(profile.SnapshotID)
	route.Profile.LLM = profile.LLM
	if strings.TrimSpace(profile.VersionLabel) != "" {
		route.Profile.Version = strings.TrimSpace(profile.VersionLabel)
	}
	if profile.MemoryPolicy != nil {
		route.Profile.MemoryPolicy = profile.MemoryPolicy
	}
	if profile.MaxAutonomy != "" && autonomyRankRuntime(profile.MaxAutonomy) < autonomyRankRuntime(route.Autonomy) {
		route.Autonomy = profile.MaxAutonomy
		route.Profile.MaxAutonomy = profile.MaxAutonomy
	}
	if len(profile.AllowedTools) > 0 {
		route.AllowedTools = intersectRuntimePatterns(route.AllowedTools, profile.AllowedTools)
		route.Profile.AllowedTools = append([]string(nil), route.AllowedTools...)
	}
	if len(profile.AllowedCapabilities) > 0 {
		route.Profile.AllowedCapabilities = intersectRuntimePatterns(route.Profile.AllowedCapabilities, profile.AllowedCapabilities)
	}
	return route, nil
}

func lowerAutonomy(left, right AutonomyLevel) AutonomyLevel {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	if autonomyRankRuntime(right) < autonomyRankRuntime(left) {
		return right
	}
	return left
}

func intersectRuntimePatterns(values, allowed []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if stringListAllows(allowed, value) {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				out = append(out, value)
			}
			continue
		}
		if strings.HasSuffix(value, "*") {
			for _, item := range allowed {
				item = strings.TrimSpace(item)
				if item == "" || strings.HasSuffix(item, "*") {
					continue
				}
				if stringListAllows([]string{value}, item) {
					if _, ok := seen[item]; ok {
						continue
					}
					seen[item] = struct{}{}
					out = append(out, item)
				}
			}
		}
		if value == "*" {
			for _, item := range allowed {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				if _, ok := seen[item]; ok {
					continue
				}
				seen[item] = struct{}{}
				out = append(out, item)
			}
		}
	}
	return out
}
