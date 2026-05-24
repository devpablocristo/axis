package runtime

import (
	"context"
	"fmt"
	"strings"
)

type AgentResolver interface {
	ResolveRuntimeAgent(ctx context.Context, orgID, productSurface, agentID string) (RuntimeAgentConfig, error)
}

type RuntimeAgentConfig struct {
	AgentID             string         `json:"agent_id"`
	ProfileID           string         `json:"profile_id,omitempty"`
	Role                string         `json:"role,omitempty"`
	Status              string         `json:"status,omitempty"`
	MaxAutonomy         AutonomyLevel  `json:"max_autonomy,omitempty"`
	AllowedTools        []string       `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string       `json:"allowed_capabilities,omitempty"`
	AllowedConnectors   []string       `json:"allowed_connectors,omitempty"`
	MemoryScopeID       string         `json:"memory_scope_id,omitempty"`
	SharedMemoryPolicy  map[string]any `json:"shared_memory_policy,omitempty"`
	Limits              map[string]any `json:"limits,omitempty"`
	SLA                 map[string]any `json:"sla,omitempty"`
	Version             int64          `json:"version,omitempty"`
}

func applyRuntimeAgent(route AgentRoute, agent RuntimeAgentConfig) (AgentRoute, *GuardrailEvent) {
	agent.AgentID = strings.TrimSpace(agent.AgentID)
	if agent.AgentID == "" {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent", Reason: "agent_id is required"}
	}
	if strings.EqualFold(strings.TrimSpace(agent.Status), "disabled") {
		return route, &GuardrailEvent{Type: "agent_fleet", Target: "agent:" + agent.AgentID, Reason: "agent is disabled"}
	}
	if strings.TrimSpace(agent.ProfileID) != "" {
		route.Profile.ID = strings.TrimSpace(agent.ProfileID)
	}
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
