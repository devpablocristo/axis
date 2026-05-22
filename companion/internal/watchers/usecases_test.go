package watchers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// --- fakes ---

type fakeWatcherRepo struct {
	watchers  map[uuid.UUID]domain.Watcher
	proposals []domain.Proposal
}

func newFakeRepo() *fakeWatcherRepo {
	return &fakeWatcherRepo{watchers: make(map[uuid.UUID]domain.Watcher)}
}

func (f *fakeWatcherRepo) CreateWatcher(_ context.Context, w domain.Watcher) (domain.Watcher, error) {
	w.ID = uuid.New()
	f.watchers[w.ID] = w
	return w, nil
}

func (f *fakeWatcherRepo) GetWatcher(_ context.Context, id uuid.UUID) (domain.Watcher, error) {
	w, ok := f.watchers[id]
	if !ok {
		return domain.Watcher{}, ErrNotFound
	}
	return w, nil
}

func (f *fakeWatcherRepo) ListWatchers(_ context.Context, orgID string) ([]domain.Watcher, error) {
	var out []domain.Watcher
	for _, w := range f.watchers {
		if orgID == "" || w.OrgID == orgID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (f *fakeWatcherRepo) ListEnabledOrgIDs(_ context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	for _, w := range f.watchers {
		if w.Enabled {
			seen[w.OrgID] = struct{}{}
		}
	}
	var out []string
	for orgID := range seen {
		out = append(out, orgID)
	}
	return out, nil
}

func (f *fakeWatcherRepo) UpdateWatcher(_ context.Context, w domain.Watcher) (domain.Watcher, error) {
	if _, ok := f.watchers[w.ID]; !ok {
		return domain.Watcher{}, ErrNotFound
	}
	f.watchers[w.ID] = w
	return w, nil
}

func (f *fakeWatcherRepo) DeleteWatcher(_ context.Context, id uuid.UUID) error {
	if _, ok := f.watchers[id]; !ok {
		return ErrNotFound
	}
	delete(f.watchers, id)
	return nil
}

func (f *fakeWatcherRepo) CreateProposal(_ context.Context, p domain.Proposal) (domain.Proposal, error) {
	p.ID = uuid.New()
	f.proposals = append(f.proposals, p)
	return p, nil
}

func (f *fakeWatcherRepo) UpdateProposal(_ context.Context, p domain.Proposal) error {
	for i, existing := range f.proposals {
		if existing.ID == p.ID {
			f.proposals[i] = p
			return nil
		}
	}
	return nil
}

func (f *fakeWatcherRepo) ListProposalsByWatcher(_ context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if p.WatcherID == watcherID {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeWatcherRepo) PendingProposals(_ context.Context, _ string) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if p.ExecutionStatus == domain.ProposalPending {
			out = append(out, p)
		}
	}
	return out, nil
}

// --- pymes fake ---

type fakePymes struct {
	staleItems []domain.PymesItem
	sendErr    error
	sendCalls  int
}

func (f *fakePymes) GetStaleWorkOrders(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return f.staleItems, nil
}

func (f *fakePymes) GetUnconfirmedAppointments(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return nil, nil
}

func (f *fakePymes) GetLowStockItems(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return nil, nil
}

func (f *fakePymes) GetInactiveCustomers(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return nil, nil
}

func (f *fakePymes) GetRevenueComparison(_ context.Context, _ string) (*domain.RevenueComparison, error) {
	return &domain.RevenueComparison{CurrentMonth: 100, PreviousMonth: 100, DropPercent: 0}, nil
}

func (f *fakePymes) SendWhatsAppTemplate(_ context.Context, _, _, _ string, _ map[string]string) error {
	f.sendCalls++
	return f.sendErr
}

func (f *fakePymes) SendWhatsAppText(_ context.Context, _, _, _ string) error {
	f.sendCalls++
	return f.sendErr
}

// --- governance fake ---

type fakeGovernance struct {
	decision    string
	reportCalls int
}

func (f *fakeGovernance) SubmitRequest(_ context.Context, _ string, _ governanceclient.SubmitRequestBody) (governanceclient.SubmitResponse, error) {
	return governanceclient.SubmitResponse{
		RequestID: uuid.New().String(),
		Decision:  f.decision,
		Status:    f.decision,
	}, nil
}

func (f *fakeGovernance) GetRequest(_ context.Context, _ string) (governanceclient.RequestSummary, int, error) {
	return governanceclient.RequestSummary{Status: f.decision, Decision: f.decision}, 200, nil
}

func (f *fakeGovernance) ReportResult(_ context.Context, _ string, _ bool, _ map[string]any, _ int64, _ string) (int, error) {
	f.reportCalls++
	return 200, nil
}

type fakeConnectorExecutor struct {
	connectorID uuid.UUID
	execCalls   int
	readCalls   int
	lastSpec    connectordomain.ExecutionSpec
	readResults map[string]json.RawMessage
}

func (f *fakeConnectorExecutor) ListConnectors(context.Context) ([]connectordomain.Connector, error) {
	if f.connectorID == uuid.Nil {
		f.connectorID = uuid.New()
	}
	return []connectordomain.Connector{{ID: f.connectorID, OrgID: "org-1", Kind: "pymes", Enabled: true}}, nil
}

func (f *fakeConnectorExecutor) BuildActionBinding(_ context.Context, spec connectordomain.ExecutionSpec) (map[string]any, string, error) {
	return map[string]any{
		"org_id":          spec.OrgID,
		"actor_id":        spec.ActorID,
		"actor_type":      "agent",
		"product_surface": spec.ProductSurface,
		"connector_id":    spec.ConnectorID.String(),
		"capability_id":   spec.Operation,
		"operation":       spec.Operation,
		"target_system":   "pymes",
		"target_resource": spec.ConnectorID.String(),
		"payload_hash":    "payload-hash",
		"idempotency_key": spec.IdempotencyKey,
	}, "binding-hash", nil
}

func (f *fakeConnectorExecutor) Execute(_ context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
	f.lastSpec = spec
	if raw, ok := f.readResults[spec.Operation]; ok {
		f.readCalls++
		return connectordomain.ExecutionResult{
			ID:          uuid.New(),
			ConnectorID: spec.ConnectorID,
			OrgID:       spec.OrgID,
			ActorID:     spec.ActorID,
			Operation:   spec.Operation,
			Status:      connectordomain.ExecSuccess,
			Payload:     spec.Payload,
			ResultJSON:  raw,
			CreatedAt:   time.Now().UTC(),
		}, nil
	}
	f.execCalls++
	return connectordomain.ExecutionResult{
		ID:                  uuid.New(),
		ConnectorID:         spec.ConnectorID,
		OrgID:               spec.OrgID,
		ActorID:             spec.ActorID,
		Operation:           spec.Operation,
		Status:              connectordomain.ExecSuccess,
		ExternalRef:         "pymes-send",
		Payload:             spec.Payload,
		ResultJSON:          json.RawMessage(`{"sent":true}`),
		IdempotencyKey:      spec.IdempotencyKey,
		GovernanceRequestID: spec.GovernanceRequestID,
		CreatedAt:           time.Now().UTC(),
	}, nil
}

// --- tests ---

func TestUsecases_Create(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeGovernance{decision: "allowed"})

	w, err := uc.Create(context.Background(), CreateWatcherInput{
		OrgID:       "org-1",
		Name:        "Stale Orders",
		WatcherType: domain.WatcherStaleWorkOrders,
		Config:      json.RawMessage(`{"threshold_days":5}`),
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == uuid.Nil {
		t.Fatal("expected generated ID")
	}
	if w.Name != "Stale Orders" {
		t.Fatalf("unexpected name: %s", w.Name)
	}
}

func TestUsecases_UpdatePartialFields(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeGovernance{decision: "allowed"})

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Original", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{"threshold_units":10}`), Enabled: true,
	})

	newName := "Updated"
	disabled := false
	updated, err := uc.Update(context.Background(), w.ID, UpdateWatcherInput{
		Name:    &newName,
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Updated" {
		t.Fatalf("expected Updated, got %s", updated.Name)
	}
	if updated.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestUsecases_RunWatcher_DisabledReturnsError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeGovernance{decision: "allowed"})

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Disabled", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{}`), Enabled: false,
	})

	_, err := uc.RunWatcher(context.Background(), w.ID)
	if err == nil {
		t.Fatal("expected error for disabled watcher")
	}
}

func TestUsecases_RunWatcher_StaleWorkOrders_AutoExecutes(t *testing.T) {
	t.Parallel()
	governance := &fakeGovernance{decision: "allowed"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, governance)
	executor := &fakeConnectorExecutor{readResults: map[string]json.RawMessage{
		"pymes.get_work_orders": json.RawMessage(`[
			{"id":"wo-1","type":"work_order","name":"Orden atrasada","party_id":"party-1"},
			{"id":"wo-2","type":"work_order","name":"Otra orden","party_id":"party-2"}
		]`),
	}}
	uc.SetConnectorExecutor(executor)

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Stale WO", WatcherType: domain.WatcherStaleWorkOrders,
		Config: json.RawMessage(`{"threshold_days":3}`), Enabled: true,
	})

	result, err := uc.RunWatcher(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found != 2 {
		t.Fatalf("expected 2 found, got %d", result.Found)
	}
	if result.Proposed != 2 {
		t.Fatalf("expected 2 proposed, got %d", result.Proposed)
	}
	if result.Executed != 2 {
		t.Fatalf("expected 2 executed, got %d", result.Executed)
	}
	if executor.execCalls != 2 {
		t.Fatalf("expected 2 connector executions, got %d", executor.execCalls)
	}
	if executor.readCalls != 1 {
		t.Fatalf("expected 1 read capability execution, got %d", executor.readCalls)
	}
	if governance.reportCalls != 2 {
		t.Fatalf("expected 2 governance result reports, got %d", governance.reportCalls)
	}
	if len(repo.proposals) != 2 {
		t.Fatalf("expected 2 persisted proposals, got %d", len(repo.proposals))
	}
}

func TestUsecases_RunWatcher_DeniedSkipsExecution(t *testing.T) {
	t.Parallel()
	governance := &fakeGovernance{decision: "denied"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, governance)
	executor := &fakeConnectorExecutor{readResults: map[string]json.RawMessage{
		"pymes.get_work_orders": json.RawMessage(`[{"id":"wo-1","type":"work_order","name":"Denied order","party_id":"party-1"}]`),
	}}
	uc.SetConnectorExecutor(executor)

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Denied WO", WatcherType: domain.WatcherStaleWorkOrders,
		Config: json.RawMessage(`{"threshold_days":3}`), Enabled: true,
	})

	result, err := uc.RunWatcher(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Executed != 0 {
		t.Fatalf("expected 0 executed when denied, got %d", result.Executed)
	}
	if executor.execCalls != 0 {
		t.Fatalf("expected 0 connector executions when denied, got %d", executor.execCalls)
	}
	if governance.reportCalls != 0 {
		t.Fatalf("expected 0 governance reports when denied, got %d", governance.reportCalls)
	}
}

func TestUsecases_Delete(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeGovernance{})

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "To Delete", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{}`), Enabled: true,
	})

	if err := uc.Delete(context.Background(), w.ID); err != nil {
		t.Fatal(err)
	}

	_, err := uc.Get(context.Background(), w.ID)
	if err == nil {
		t.Fatal("expected not found after delete")
	}
}
