package agentfleet

import (
	"context"
	"net/http"
	"testing"
)

type fakeRepo struct {
	agents   map[string]Agent
	handoffs []Handoff
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{agents: map[string]Agent{}}
}

func (f *fakeRepo) ListAgents(context.Context, string, string) ([]Agent, error) {
	out := make([]Agent, 0, len(f.agents))
	for _, agent := range f.agents {
		out = append(out, agent)
	}
	return out, nil
}

func (f *fakeRepo) GetAgent(_ context.Context, orgID, productSurface, agentID string) (Agent, error) {
	agent, ok := f.agents[key(orgID, productSurface, agentID)]
	if !ok {
		return Agent{}, ErrNotFound
	}
	return agent, nil
}

func (f *fakeRepo) SaveAgent(_ context.Context, agent Agent) (Agent, error) {
	agent.Version++
	f.agents[key(agent.OrgID, agent.ProductSurface, agent.AgentID)] = agent
	return agent, nil
}

func (f *fakeRepo) DisableAgent(_ context.Context, orgID, productSurface, agentID, _ string) (Agent, error) {
	agent, ok := f.agents[key(orgID, productSurface, agentID)]
	if !ok {
		return Agent{}, ErrNotFound
	}
	agent.Status = StatusDisabled
	agent.LifecycleStatus = LifecycleArchived
	agent.Version++
	f.agents[key(orgID, productSurface, agentID)] = agent
	return agent, nil
}

func (f *fakeRepo) SetAgentLifecycle(_ context.Context, orgID, productSurface, agentID, lifecycleStatus, status, reviewStatus, _ string) (Agent, error) {
	agent, ok := f.agents[key(orgID, productSurface, agentID)]
	if !ok {
		return Agent{}, ErrNotFound
	}
	if lifecycleStatus != "" {
		agent.LifecycleStatus = lifecycleStatus
	}
	if status != "" {
		agent.Status = status
	}
	if reviewStatus != "" {
		agent.ReviewStatus = reviewStatus
	}
	agent.Version++
	f.agents[key(orgID, productSurface, agentID)] = agent
	return agent, nil
}

func (f *fakeRepo) DeleteAgent(_ context.Context, orgID, productSurface, agentID, _ string) error {
	k := key(orgID, productSurface, agentID)
	if _, ok := f.agents[k]; !ok {
		return ErrNotFound
	}
	delete(f.agents, k)
	return nil
}

func (f *fakeRepo) CreateHandoff(_ context.Context, handoff Handoff) (Handoff, error) {
	handoff.ID = "handoff-1"
	f.handoffs = append(f.handoffs, handoff)
	return handoff, nil
}

func (f *fakeRepo) ListHandoffs(context.Context, string, string, int) ([]Handoff, error) {
	return append([]Handoff(nil), f.handoffs...), nil
}

func (f *fakeRepo) UpdateHandoffStatus(_ context.Context, _, _, handoffID, status, _ string) (Handoff, error) {
	for i := range f.handoffs {
		if f.handoffs[i].ID == handoffID {
			f.handoffs[i].Status = status
			return f.handoffs[i], nil
		}
	}
	return Handoff{}, ErrNotFound
}

func TestUsecases_SaveAgentNormalizesAndVersions(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	agent, err := uc.SaveAgent(context.Background(), Agent{
		OrgID:               " org-1 ",
		ProductSurface:      "",
		AgentID:             " support ",
		MaxAutonomy:         "",
		AllowedTools:        []string{"remember", "remember", ""},
		AllowedCapabilities: []string{"demo.read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.ProductSurface != "companion" || agent.Status != StatusActive || agent.MaxAutonomy != "A2" {
		t.Fatalf("agent defaults not applied: %+v", agent)
	}
	if agent.LifecycleStatus != LifecycleActive || agent.ReviewStatus != ReviewApproved {
		t.Fatalf("expected executable lifecycle defaults, got %+v", agent)
	}
	if len(agent.AllowedTools) != 1 || agent.AllowedTools[0] != "remember" {
		t.Fatalf("expected normalized tools, got %+v", agent.AllowedTools)
	}
}

func TestUsecases_LifecycleControlsExecutability(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "active", ProfileID: "support.v1"})
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "review", ReviewStatus: ReviewNeedsReview})

	if _, err := uc.AssignAgent(context.Background(), AssignmentInput{OrgID: "org-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.ArchiveAgent(context.Background(), "org-1", "companion", "active", "admin"); err != nil {
		t.Fatal(err)
	}
	_, err := uc.AssignAgent(context.Background(), AssignmentInput{OrgID: "org-1"})
	if err == nil {
		t.Fatal("expected no executable agents")
	}
}

func TestUsecases_ApproveRequiresRealProfile(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "inferred", ProfileID: "legacy.unprofiled", ReviewStatus: ReviewNeedsReview, Status: StatusDisabled, LifecycleStatus: LifecycleArchived})

	if _, err := uc.ApproveAgent(context.Background(), "org-1", "companion", "inferred", "admin"); err == nil {
		t.Fatal("expected approve without profile to fail")
	}
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "inferred", ProfileID: "support.v1", ReviewStatus: ReviewNeedsReview, Status: StatusDisabled, LifecycleStatus: LifecycleArchived})
	agent, err := uc.ApproveAgent(context.Background(), "org-1", "companion", "inferred", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if agentExecutable(agent) {
		t.Fatalf("newly approved archived agent should not be executable before restore: %+v", agent)
	}
	if agent.LifecycleStatus != LifecycleArchived || agent.Status != StatusDisabled || agent.ReviewStatus != ReviewApproved {
		t.Fatalf("approve should preserve lifecycle/status and only set review_status: %+v", agent)
	}
	agent, err = uc.RestoreAgent(context.Background(), "org-1", "companion", "inferred", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !agentExecutable(agent) {
		t.Fatalf("restored approved agent should be executable: %+v", agent)
	}
}

func TestUsecases_RejectsInvalidAgentAutonomy(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(newFakeRepo())
	_, err := uc.SaveAgent(context.Background(), Agent{
		OrgID:       "org-1",
		AgentID:     "support",
		MaxAutonomy: "A9",
	})
	if err == nil {
		t.Fatal("expected invalid autonomy to fail")
	}
}

func TestUsecases_CreateHandoffValidatesTargetAgent(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "source"})
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "target", Status: StatusDisabled})

	_, err := uc.CreateHandoff(context.Background(), Handoff{
		OrgID:       "org-1",
		FromAgentID: "source",
		ToAgentID:   "target",
	})
	if err == nil {
		t.Fatal("expected disabled target to fail")
	}

	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "target", Status: StatusActive})
	handoff, err := uc.CreateHandoff(context.Background(), Handoff{
		OrgID:       "org-1",
		FromAgentID: "source",
		ToAgentID:   "target",
	})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Status != HandoffPending {
		t.Fatalf("expected pending handoff, got %+v", handoff)
	}
}

func TestHandler_RegisterPatterns(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)
}

func key(orgID, productSurface, agentID string) string {
	if productSurface == "" {
		productSurface = "companion"
	}
	return orgID + "/" + productSurface + "/" + agentID
}
