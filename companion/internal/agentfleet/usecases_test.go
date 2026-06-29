package agentfleet

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/devpablocristo/companion/internal/agentprofiles"
	"github.com/devpablocristo/companion/internal/identityctx"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
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
	handoff.ID = "11111111-1111-1111-1111-111111111111"
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
	if agent.ProductSurface != "companion" || agent.Status != StatusDisabled || agent.MaxAutonomy != "A2" {
		t.Fatalf("agent defaults not applied: %+v", agent)
	}
	// Safe-by-default (F4.8): un create sin status/review explícitos NO queda
	// ejecutable; debe pasar por activación/aprobación deliberada.
	if agent.LifecycleStatus != LifecycleActive || agent.ReviewStatus != ReviewNeedsReview {
		t.Fatalf("expected safe-by-default lifecycle (disabled/needs_review), got %+v", agent)
	}
	if len(agent.AllowedTools) != 1 || agent.AllowedTools[0] != "remember" {
		t.Fatalf("expected normalized tools, got %+v", agent.AllowedTools)
	}
}

func TestUsecases_SaveAgentEmptyProfileIDMapsToSentinel(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	// Un profile_id="" explícito (el seed lo manda así para agentes sin perfil)
	// debe normalizarse al centinela legacy.unprofiled para satisfacer la FK
	// companion_agents.profile_id (migración 0038), no quedar en "".
	agent, err := uc.SaveAgent(context.Background(), Agent{
		OrgID:        "org-1",
		AgentID:      "clinical_archivist",
		ProfileID:    "",
		Status:       StatusActive,
		ReviewStatus: ReviewApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.ProfileID != agentprofiles.UnprofiledProfileID {
		t.Fatalf("expected empty profile_id to map to %q, got %q", agentprofiles.UnprofiledProfileID, agent.ProfileID)
	}
}

func TestUsecases_SaveAgentSafeDefaultAndUpdatePreserve(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFakeRepo()
	uc := NewUsecases(repo)

	// CREATE sin status/review explícitos -> safe default (disabled + needs_review).
	bare, err := uc.SaveAgent(ctx, Agent{OrgID: "org-1", AgentID: "bare"})
	if err != nil {
		t.Fatal(err)
	}
	if bare.Status != StatusDisabled || bare.ReviewStatus != ReviewNeedsReview {
		t.Fatalf("create pelado debía quedar disabled/needs_review, got %+v", bare)
	}

	// CREATE con opt-in explícito -> active + approved.
	explicit, err := uc.SaveAgent(ctx, Agent{OrgID: "org-1", AgentID: "explicit", ProfileID: "support.v1", Status: StatusActive, ReviewStatus: ReviewApproved})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Status != StatusActive || explicit.ReviewStatus != ReviewApproved {
		t.Fatalf("create explícito debía quedar active/approved, got %+v", explicit)
	}

	// UPDATE parcial del agente aprobado (sin reenviar status/review) -> preserva
	// active/approved; editar otros campos no debe desactivar ni desaprobar.
	updated, err := uc.SaveAgent(ctx, Agent{OrgID: "org-1", AgentID: "explicit", ProfileID: "support.v1", DisplayName: "Soporte 2"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusActive || updated.ReviewStatus != ReviewApproved {
		t.Fatalf("update parcial debía preservar active/approved, got %+v", updated)
	}
}

func TestUsecases_SaveAgentPreservesMetadataOnPartialUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepo()
	uc := NewUsecases(repo)
	metadata := map[string]any{
		"job_title":        "Billing Specialist",
		"mission":          "Keep invoices healthy",
		"responsibilities": []string{"review invoices"},
		"owner_user_id":    "user-1",
	}
	created, err := uc.SaveAgent(ctx, Agent{
		OrgID:        "org-1",
		AgentID:      "billing",
		ProfileID:    "billing.v1",
		Status:       StatusActive,
		ReviewStatus: ReviewApproved,
		Metadata:     metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.Metadata, metadata) {
		t.Fatalf("expected metadata round-trip on create, got %+v", created.Metadata)
	}

	updated, err := uc.SaveAgent(ctx, Agent{
		OrgID:       "org-1",
		AgentID:     "billing",
		ProfileID:   "billing.v1",
		DisplayName: "Billing Lead",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Metadata, metadata) {
		t.Fatalf("partial update should preserve metadata, got %+v", updated.Metadata)
	}

	replaced, err := uc.SaveAgent(ctx, Agent{
		OrgID:     "org-1",
		AgentID:   "billing",
		ProfileID: "billing.v1",
		Metadata:  map[string]any{"job_title": "AR Lead"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replaced.Metadata, map[string]any{"job_title": "AR Lead"}) {
		t.Fatalf("explicit metadata update should replace metadata, got %+v", replaced.Metadata)
	}
}

func TestUsecases_LifecycleControlsExecutability(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo)
	// "active" se crea con activación/aprobación explícitas (opt-in deliberado),
	// porque el default seguro de F4.8 ya no las da gratis.
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "active", ProfileID: "support.v1", Status: StatusActive, ReviewStatus: ReviewApproved})
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
	// source y target con activación/aprobación explícitas (el default seguro de
	// F4.8 ya no las da gratis); el test ejercita la validación de ejecutabilidad
	// del target en el handoff.
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "source", Status: StatusActive, ReviewStatus: ReviewApproved})
	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "target", Status: StatusDisabled})

	_, err := uc.CreateHandoff(context.Background(), Handoff{
		OrgID:       "org-1",
		FromAgentID: "source",
		ToAgentID:   "target",
	})
	if err == nil {
		t.Fatal("expected disabled target to fail")
	}

	_, _ = uc.SaveAgent(context.Background(), Agent{OrgID: "org-1", AgentID: "target", Status: StatusActive, ReviewStatus: ReviewApproved})
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

func TestHandler_VirtualEmployeesCRUDLifecycleParity(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)

	res := serveAgentFleetRequest(t, mux, http.MethodPut, "/v1/virtual-employees/support", `{
		"display_name":"Support",
		"profile_id":"support.v1",
		"status":"active",
		"review_status":"approved",
		"max_autonomy":"A2",
		"allowed_tools":["remember"],
		"allowed_capabilities":["demo.read"],
		"metadata":{
			"job_title":"Support Specialist",
			"mission":"Keep customers moving"
		}
	}`)
	requireStatus(t, res, http.StatusOK)
	var saved Agent
	decodeResponse(t, res, &saved)
	if saved.AgentID != "support" || saved.Status != StatusActive || saved.ReviewStatus != ReviewApproved {
		t.Fatalf("expected virtual employee to map onto agent support, got %+v", saved)
	}
	if saved.Metadata["job_title"] != "Support Specialist" {
		t.Fatalf("expected metadata round-trip on create, got %+v", saved.Metadata)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodGet, "/v1/virtual-employees/support", "")
	requireStatus(t, res, http.StatusOK)
	var fetched Agent
	decodeResponse(t, res, &fetched)
	if fetched.AgentID != "support" {
		t.Fatalf("expected employee_id to map to agent_id support, got %+v", fetched)
	}
	if fetched.Metadata["mission"] != "Keep customers moving" {
		t.Fatalf("expected metadata round-trip on detail, got %+v", fetched.Metadata)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodGet, "/v1/virtual-employees", "")
	requireStatus(t, res, http.StatusOK)
	var list struct {
		Data []Agent `json:"data"`
	}
	decodeResponse(t, res, &list)
	if len(list.Data) != 1 || list.Data[0].AgentID != "support" {
		t.Fatalf("expected virtual employees list to use agent list, got %+v", list.Data)
	}
	if list.Data[0].Metadata["job_title"] != "Support Specialist" {
		t.Fatalf("expected metadata round-trip on list, got %+v", list.Data[0].Metadata)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/support/archive", "")
	requireStatus(t, res, http.StatusOK)
	var archived Agent
	decodeResponse(t, res, &archived)
	if archived.LifecycleStatus != LifecycleArchived || archived.Status != StatusDisabled {
		t.Fatalf("expected archive parity with agent lifecycle, got %+v", archived)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/support/approve", "")
	requireStatus(t, res, http.StatusOK)
	var approved Agent
	decodeResponse(t, res, &approved)
	if approved.ReviewStatus != ReviewApproved {
		t.Fatalf("expected approve parity with agent review, got %+v", approved)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/support/trash", "")
	requireStatus(t, res, http.StatusOK)
	var trashed Agent
	decodeResponse(t, res, &trashed)
	if trashed.LifecycleStatus != LifecycleTrash || trashed.Status != StatusDisabled {
		t.Fatalf("expected trash parity with agent lifecycle, got %+v", trashed)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/support/restore", "")
	requireStatus(t, res, http.StatusOK)
	var restored Agent
	decodeResponse(t, res, &restored)
	if restored.LifecycleStatus != LifecycleActive || restored.Status != StatusActive {
		t.Fatalf("expected restore parity with agent lifecycle, got %+v", restored)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/support/disable", "")
	requireStatus(t, res, http.StatusOK)
	var disabled Agent
	decodeResponse(t, res, &disabled)
	if disabled.Status != StatusDisabled || disabled.LifecycleStatus != LifecycleArchived {
		t.Fatalf("expected disable parity with agent lifecycle, got %+v", disabled)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodDelete, "/v1/virtual-employees/support", "")
	requireStatus(t, res, http.StatusNoContent)

	res = serveAgentFleetRequest(t, mux, http.MethodGet, "/v1/virtual-employees/support", "")
	requireStatus(t, res, http.StatusNotFound)
}

func TestHandler_VirtualEmployeesAssignmentAndHandoffParity(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(newFakeRepo())).Register(mux)

	source := serveAgentFleetRequest(t, mux, http.MethodPut, "/v1/virtual-employees/source", `{
		"display_name":"Source",
		"profile_id":"source.v1",
		"status":"active",
		"review_status":"approved",
		"allowed_capabilities":["demo.read"]
	}`)
	requireStatus(t, source, http.StatusOK)

	target := serveAgentFleetRequest(t, mux, http.MethodPut, "/v1/virtual-employees/target", `{
		"display_name":"Target",
		"profile_id":"target.v1",
		"status":"active",
		"review_status":"approved"
	}`)
	requireStatus(t, target, http.StatusOK)

	res := serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/assignments", `{"capability_id":"demo.read"}`)
	requireStatus(t, res, http.StatusOK)
	var assignment AssignmentResult
	decodeResponse(t, res, &assignment)
	if assignment.Agent.AgentID != "source" {
		t.Fatalf("expected assignment to reuse agent assignment, got %+v", assignment)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPost, "/v1/virtual-employees/handoffs", `{
		"from_agent_id":"source",
		"to_agent_id":"target",
		"task_id":"task-1"
	}`)
	requireStatus(t, res, http.StatusCreated)
	var handoff Handoff
	decodeResponse(t, res, &handoff)
	if handoff.FromAgentID != "source" || handoff.ToAgentID != "target" || handoff.Status != HandoffPending {
		t.Fatalf("expected handoff parity with agents, got %+v", handoff)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodGet, "/v1/virtual-employees/handoffs", "")
	requireStatus(t, res, http.StatusOK)
	var handoffList struct {
		Data []Handoff `json:"data"`
	}
	decodeResponse(t, res, &handoffList)
	if len(handoffList.Data) != 1 || handoffList.Data[0].ID != handoff.ID {
		t.Fatalf("expected handoff list parity, got %+v", handoffList.Data)
	}

	res = serveAgentFleetRequest(t, mux, http.MethodPatch, "/v1/virtual-employees/handoffs/"+handoff.ID, `{"status":"accepted"}`)
	requireStatus(t, res, http.StatusOK)
	var accepted Handoff
	decodeResponse(t, res, &accepted)
	if accepted.Status != HandoffAccepted {
		t.Fatalf("expected handoff status update parity, got %+v", accepted)
	}
}

func serveAgentFleetRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Buffer
	if strings.TrimSpace(body) == "" {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path+"?org_id=org-1", reader)
	req = withAgentFleetPrincipal(req)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func withAgentFleetPrincipal(req *http.Request) *http.Request {
	principal := &authn.Principal{OrgID: "org-1", Actor: "admin", Scopes: []string{scopeAgentRuntimeAdmin}, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}

func requireStatus(t *testing.T, res *httptest.ResponseRecorder, want int) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("expected status %d, got %d body=%s", want, res.Code, res.Body.String())
	}
}

func decodeResponse(t *testing.T, res *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v body=%s", err, res.Body.String())
	}
}

func key(orgID, productSurface, agentID string) string {
	if productSurface == "" {
		productSurface = "companion"
	}
	return orgID + "/" + productSurface + "/" + agentID
}
