package agentfleet

import (
	"context"
	"fmt"
	"strings"
)

type Repository interface {
	ListAgents(ctx context.Context, orgID, productSurface string) ([]Agent, error)
	GetAgent(ctx context.Context, orgID, productSurface, agentID string) (Agent, error)
	SaveAgent(ctx context.Context, agent Agent) (Agent, error)
	DisableAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error)
	SetAgentLifecycle(ctx context.Context, orgID, productSurface, agentID, lifecycleStatus, status, reviewStatus, changedBy string) (Agent, error)
	DeleteAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) error
	CreateHandoff(ctx context.Context, handoff Handoff) (Handoff, error)
	ListHandoffs(ctx context.Context, orgID, productSurface string, limit int) ([]Handoff, error)
	UpdateHandoffStatus(ctx context.Context, orgID, productSurface, handoffID, status, changedBy string) (Handoff, error)
}

type Usecases struct {
	repo      Repository
	ownership TaskOwnershipPort
}

type TaskOwnershipPort interface {
	TransferTaskOwnership(ctx context.Context, orgID, taskID, agentID string) error
}

type AssignmentInput struct {
	OrgID          string   `json:"org_id,omitempty"`
	ProductSurface string   `json:"product_surface,omitempty"`
	Intent         string   `json:"intent,omitempty"`
	CapabilityID   string   `json:"capability_id,omitempty"`
	Connector      string   `json:"connector,omitempty"`
	RequiredTools  []string `json:"required_tools,omitempty"`
}

type AssignmentResult struct {
	Agent   Agent    `json:"agent"`
	Reason  string   `json:"reason"`
	Matches []string `json:"matches,omitempty"`
}

type AgentAssignmentPolicy interface {
	AssignAgent(ctx context.Context, in AssignmentInput) (AssignmentResult, error)
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) WithTaskOwnership(port TaskOwnershipPort) *Usecases {
	u.ownership = port
	return u
}

func (u *Usecases) ListAgents(ctx context.Context, orgID, productSurface string) ([]Agent, error) {
	return u.repo.ListAgents(ctx, orgID, productSurface)
}

func (u *Usecases) GetAgent(ctx context.Context, orgID, productSurface, agentID string) (Agent, error) {
	return u.repo.GetAgent(ctx, orgID, productSurface, agentID)
}

func (u *Usecases) SaveAgent(ctx context.Context, agent Agent) (Agent, error) {
	agent = normalizeAgent(agent)
	if err := validateAgent(agent); err != nil {
		return Agent{}, fmt.Errorf("%w: org_id, agent_id, status and max_autonomy are required", err)
	}
	return u.repo.SaveAgent(ctx, agent)
}

func (u *Usecases) DisableAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	return u.repo.DisableAgent(ctx, orgID, productSurface, agentID, changedBy)
}

func (u *Usecases) ArchiveAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	return u.repo.SetAgentLifecycle(ctx, orgID, productSurface, agentID, LifecycleArchived, StatusDisabled, "", changedBy)
}

func (u *Usecases) TrashAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	return u.repo.SetAgentLifecycle(ctx, orgID, productSurface, agentID, LifecycleTrash, StatusDisabled, "", changedBy)
}

func (u *Usecases) RestoreAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	agent, err := u.repo.GetAgent(ctx, orgID, productSurface, agentID)
	if err != nil {
		return Agent{}, err
	}
	if agent.ReviewStatus != ReviewApproved {
		return Agent{}, fmt.Errorf("%w: only approved agents can be restored", ErrValidation)
	}
	return u.repo.SetAgentLifecycle(ctx, orgID, productSurface, agentID, LifecycleActive, StatusActive, "", changedBy)
}

func (u *Usecases) ApproveAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	agent, err := u.repo.GetAgent(ctx, orgID, productSurface, agentID)
	if err != nil {
		return Agent{}, err
	}
	if strings.TrimSpace(agent.ProfileID) == "" || agent.ProfileID == "legacy.unprofiled" {
		return Agent{}, fmt.Errorf("%w: approved agents require a real profile_id", ErrValidation)
	}
	return u.repo.SetAgentLifecycle(ctx, orgID, productSurface, agentID, LifecycleActive, StatusActive, ReviewApproved, changedBy)
}

func (u *Usecases) IgnoreAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) (Agent, error) {
	return u.repo.SetAgentLifecycle(ctx, orgID, productSurface, agentID, LifecycleArchived, StatusDisabled, ReviewIgnored, changedBy)
}

func (u *Usecases) DeleteAgent(ctx context.Context, orgID, productSurface, agentID, changedBy string) error {
	return u.repo.DeleteAgent(ctx, orgID, productSurface, agentID, changedBy)
}

func (u *Usecases) CreateHandoff(ctx context.Context, handoff Handoff) (Handoff, error) {
	handoff = normalizeHandoff(handoff)
	if err := validateHandoff(handoff); err != nil {
		return Handoff{}, fmt.Errorf("%w: handoff requires distinct source and target agents", err)
	}
	source, err := u.repo.GetAgent(ctx, handoff.OrgID, handoff.ProductSurface, handoff.FromAgentID)
	if err != nil {
		return Handoff{}, fmt.Errorf("source agent: %w", err)
	}
	if !agentExecutable(source) {
		return Handoff{}, fmt.Errorf("%w: source agent is not executable", ErrValidation)
	}
	target, err := u.repo.GetAgent(ctx, handoff.OrgID, handoff.ProductSurface, handoff.ToAgentID)
	if err != nil {
		return Handoff{}, fmt.Errorf("target agent: %w", err)
	}
	if !agentExecutable(target) {
		return Handoff{}, fmt.Errorf("%w: target agent is not executable", ErrValidation)
	}
	return u.repo.CreateHandoff(ctx, handoff)
}

func (u *Usecases) ListHandoffs(ctx context.Context, orgID, productSurface string, limit int) ([]Handoff, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return u.repo.ListHandoffs(ctx, orgID, productSurface, limit)
}

func (u *Usecases) UpdateHandoffStatus(ctx context.Context, orgID, productSurface, handoffID, status, changedBy string) (Handoff, error) {
	switch status {
	case HandoffAccepted, HandoffRejected, HandoffCompleted, HandoffCancelled:
	default:
		return Handoff{}, fmt.Errorf("%w: invalid handoff status", ErrValidation)
	}
	handoff, err := u.repo.UpdateHandoffStatus(ctx, orgID, productSurface, handoffID, status, changedBy)
	if err != nil {
		return Handoff{}, err
	}
	if u.ownership != nil && handoff.TaskID != "" && (status == HandoffAccepted || status == HandoffCompleted) {
		if err := u.ownership.TransferTaskOwnership(ctx, handoff.OrgID, handoff.TaskID, handoff.ToAgentID); err != nil {
			return Handoff{}, fmt.Errorf("transfer task ownership: %w", err)
		}
	}
	return handoff, nil
}

func (u *Usecases) AssignAgent(ctx context.Context, in AssignmentInput) (AssignmentResult, error) {
	in.OrgID = strings.TrimSpace(in.OrgID)
	in.ProductSurface = strings.TrimSpace(in.ProductSurface)
	if in.ProductSurface == "" {
		in.ProductSurface = "companion"
	}
	if in.OrgID == "" {
		return AssignmentResult{}, fmt.Errorf("%w: org_id is required", ErrValidation)
	}
	agents, err := u.repo.ListAgents(ctx, in.OrgID, in.ProductSurface)
	if err != nil {
		return AssignmentResult{}, err
	}
	var best Agent
	var bestMatches []string
	bestScore := -1
	for _, agent := range agents {
		if !agentExecutable(agent) {
			continue
		}
		matches := agentAssignmentMatches(agent, in)
		score := agentAssignmentScore(agent, matches)
		if best.AgentID == "" || score > bestScore {
			best = agent
			bestMatches = matches
			bestScore = score
		}
	}
	if best.AgentID == "" {
		return AssignmentResult{}, ErrNotFound
	}
	reason := "active_agent_fallback"
	if len(bestMatches) > 0 {
		reason = "capability_match"
	}
	return AssignmentResult{Agent: best, Reason: reason, Matches: bestMatches}, nil
}

func agentExecutable(agent Agent) bool {
	return agent.Status == StatusActive &&
		agent.LifecycleStatus == LifecycleActive &&
		agent.ReviewStatus == ReviewApproved
}

func agentAssignmentScore(agent Agent, matches []string) int {
	score := len(matches) * 10
	if agent.MaxAutonomy != "" {
		score += autonomyScore(agent.MaxAutonomy)
	}
	if value, ok := numericLimit(agent.Limits, "active_tasks"); ok {
		score -= value
	}
	if value, ok := numericLimit(agent.SLA, "breaches"); ok {
		score -= value * 5
	}
	return score
}

func autonomyScore(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A5":
		return 5
	case "A4":
		return 4
	case "A3":
		return 3
	case "A2":
		return 2
	case "A1":
		return 1
	default:
		return 0
	}
}

func numericLimit(values map[string]any, key string) (int, bool) {
	if values == nil {
		return 0, false
	}
	switch value := values[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func agentAssignmentMatches(agent Agent, in AssignmentInput) []string {
	var matches []string
	if value := strings.TrimSpace(in.CapabilityID); value != "" && listAllows(agent.AllowedCapabilities, value) {
		matches = append(matches, "capability:"+value)
	}
	if value := strings.TrimSpace(in.Connector); value != "" && listAllows(agent.AllowedConnectors, value) {
		matches = append(matches, "connector:"+value)
	}
	for _, tool := range in.RequiredTools {
		if listAllows(agent.AllowedTools, tool) {
			matches = append(matches, "tool:"+strings.TrimSpace(tool))
		}
	}
	if value := strings.TrimSpace(in.Intent); value != "" && strings.Contains(strings.ToLower(agent.Role+" "+agent.DisplayName+" "+agent.ProfileID), strings.ToLower(value)) {
		matches = append(matches, "intent:"+value)
	}
	return matches
}

func listAllows(values []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "*" || item == value {
			return true
		}
		if strings.HasSuffix(item, "*") && strings.HasPrefix(value, strings.TrimSuffix(item, "*")) {
			return true
		}
	}
	return false
}
