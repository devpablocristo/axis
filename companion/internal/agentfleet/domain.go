package agentfleet

import (
	"errors"
	"strings"
	"time"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"

	LifecycleActive   = "active"
	LifecycleArchived = "archived"
	LifecycleTrash    = "trash"

	OriginCompanionFleet  = "companion_fleet"
	OriginRuntimeInferred = "runtime_inferred"
	OriginManual          = "manual"

	ReviewApproved    = "approved"
	ReviewNeedsReview = "needs_review"
	ReviewIgnored     = "ignored"

	HandoffPending   = "pending"
	HandoffAccepted  = "accepted"
	HandoffRejected  = "rejected"
	HandoffCompleted = "completed"
	HandoffCancelled = "cancelled"
)

var (
	ErrNotFound   = errors.New("agent not found")
	ErrValidation = errors.New("agent validation failed")
)

type Agent struct {
	OrgID               string         `json:"org_id"`
	ProductSurface      string         `json:"product_surface"`
	AgentID             string         `json:"agent_id"`
	DisplayName         string         `json:"display_name,omitempty"`
	Role                string         `json:"role,omitempty"`
	ProfileID           string         `json:"profile_id,omitempty"`
	Status              string         `json:"status"`
	LifecycleStatus     string         `json:"lifecycle_status"`
	OriginKind          string         `json:"origin_kind,omitempty"`
	ReviewStatus        string         `json:"review_status"`
	MaxAutonomy         string         `json:"max_autonomy"`
	AllowedTools        []string       `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string       `json:"allowed_capabilities,omitempty"`
	AllowedConnectors   []string       `json:"allowed_connectors,omitempty"`
	MemoryScopeID       string         `json:"memory_scope_id,omitempty"`
	SharedMemoryPolicy  map[string]any `json:"shared_memory_policy,omitempty"`
	Limits              map[string]any `json:"limits,omitempty"`
	SLA                 map[string]any `json:"sla,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	Version             int64          `json:"version,omitempty"`
	CreatedBy           string         `json:"created_by,omitempty"`
	CreatedAt           time.Time      `json:"created_at,omitempty"`
	UpdatedAt           time.Time      `json:"updated_at,omitempty"`
}

type Handoff struct {
	ID             string         `json:"id,omitempty"`
	OrgID          string         `json:"org_id"`
	ProductSurface string         `json:"product_surface"`
	TaskID         string         `json:"task_id,omitempty"`
	FromAgentID    string         `json:"from_agent_id"`
	ToAgentID      string         `json:"to_agent_id"`
	Status         string         `json:"status"`
	Reason         string         `json:"reason,omitempty"`
	Context        map[string]any `json:"context,omitempty"`
	CreatedBy      string         `json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
}

func normalizeAgent(agent Agent) Agent {
	agent.OrgID = strings.TrimSpace(agent.OrgID)
	agent.ProductSurface = strings.TrimSpace(agent.ProductSurface)
	if agent.ProductSurface == "" {
		agent.ProductSurface = "companion"
	}
	agent.AgentID = strings.TrimSpace(agent.AgentID)
	agent.DisplayName = strings.TrimSpace(agent.DisplayName)
	agent.Role = strings.TrimSpace(agent.Role)
	agent.ProfileID = strings.TrimSpace(agent.ProfileID)
	agent.Status = strings.TrimSpace(agent.Status)
	if agent.Status == "" {
		// Safe-by-default: un agente creado sin status explícito queda DISABLED.
		// La activación es un opt-in deliberado (status=active explícito o
		// ApproveAgent). En updates, SaveAgent preserva el status existente.
		agent.Status = StatusDisabled
	}
	agent.LifecycleStatus = strings.TrimSpace(agent.LifecycleStatus)
	if agent.LifecycleStatus == "" {
		agent.LifecycleStatus = LifecycleActive
	}
	agent.OriginKind = strings.TrimSpace(agent.OriginKind)
	if agent.OriginKind == "" {
		agent.OriginKind = OriginManual
	}
	agent.ReviewStatus = strings.TrimSpace(agent.ReviewStatus)
	if agent.ReviewStatus == "" {
		// Safe-by-default: sin review_status explícito el agente queda
		// NEEDS_REVIEW (no ejecutable hasta aprobarse), evitando el auto-approve
		// silencioso en la creación por API. En updates, SaveAgent preserva el
		// review_status existente para no desaprobar agentes en updates parciales.
		agent.ReviewStatus = ReviewNeedsReview
	}
	agent.MaxAutonomy = strings.TrimSpace(agent.MaxAutonomy)
	if agent.MaxAutonomy == "" {
		agent.MaxAutonomy = "A2"
	}
	agent.AllowedTools = normalizeList(agent.AllowedTools)
	agent.AllowedCapabilities = normalizeList(agent.AllowedCapabilities)
	agent.AllowedConnectors = normalizeList(agent.AllowedConnectors)
	agent.MemoryScopeID = strings.TrimSpace(agent.MemoryScopeID)
	agent.CreatedBy = strings.TrimSpace(agent.CreatedBy)
	if agent.SharedMemoryPolicy == nil {
		agent.SharedMemoryPolicy = map[string]any{}
	}
	if agent.Limits == nil {
		agent.Limits = map[string]any{}
	}
	if agent.SLA == nil {
		agent.SLA = map[string]any{}
	}
	if agent.Metadata == nil {
		agent.Metadata = map[string]any{}
	}
	return agent
}

func normalizeHandoff(handoff Handoff) Handoff {
	handoff.ID = strings.TrimSpace(handoff.ID)
	handoff.OrgID = strings.TrimSpace(handoff.OrgID)
	handoff.ProductSurface = strings.TrimSpace(handoff.ProductSurface)
	if handoff.ProductSurface == "" {
		handoff.ProductSurface = "companion"
	}
	handoff.TaskID = strings.TrimSpace(handoff.TaskID)
	handoff.FromAgentID = strings.TrimSpace(handoff.FromAgentID)
	handoff.ToAgentID = strings.TrimSpace(handoff.ToAgentID)
	handoff.Status = strings.TrimSpace(handoff.Status)
	if handoff.Status == "" {
		handoff.Status = HandoffPending
	}
	handoff.Reason = strings.TrimSpace(handoff.Reason)
	handoff.CreatedBy = strings.TrimSpace(handoff.CreatedBy)
	if handoff.Context == nil {
		handoff.Context = map[string]any{}
	}
	return handoff
}

func validateAgent(agent Agent) error {
	if agent.OrgID == "" || agent.AgentID == "" {
		return ErrValidation
	}
	switch agent.Status {
	case StatusActive, StatusDisabled:
	default:
		return ErrValidation
	}
	switch agent.LifecycleStatus {
	case LifecycleActive, LifecycleArchived, LifecycleTrash:
	default:
		return ErrValidation
	}
	switch agent.OriginKind {
	case OriginCompanionFleet, OriginRuntimeInferred, OriginManual:
	default:
		return ErrValidation
	}
	switch agent.ReviewStatus {
	case ReviewApproved, ReviewNeedsReview, ReviewIgnored:
	default:
		return ErrValidation
	}
	switch agent.MaxAutonomy {
	case "A0", "A1", "A2", "A3", "A4", "A5":
	default:
		return ErrValidation
	}
	return nil
}

func validateHandoff(handoff Handoff) error {
	if handoff.OrgID == "" || handoff.FromAgentID == "" || handoff.ToAgentID == "" {
		return ErrValidation
	}
	if handoff.FromAgentID == handoff.ToAgentID {
		return ErrValidation
	}
	switch handoff.Status {
	case HandoffPending, HandoffAccepted, HandoffRejected, HandoffCompleted, HandoffCancelled:
	default:
		return ErrValidation
	}
	return nil
}

func normalizeList(values []string) []string {
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
