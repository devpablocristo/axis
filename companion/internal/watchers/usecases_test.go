package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/jobs"
	"github.com/devpablocristo/companion/internal/nexusclient"
	"github.com/devpablocristo/companion/internal/productlimits"
	"github.com/devpablocristo/platform/errors/go/domainerr"
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

// --- nexus fake ---

type fakeNexus struct {
	decision    string
	submitCalls int
	reportCalls int
}

func (f *fakeNexus) SubmitRequest(_ context.Context, _ string, _ nexusclient.SubmitRequestBody) (nexusclient.SubmitResponse, error) {
	f.submitCalls++
	return nexusclient.SubmitResponse{
		RequestID: uuid.New().String(),
		Decision:  f.decision,
		Status:    f.decision,
	}, nil
}

func (f *fakeNexus) GetRequest(_ context.Context, _ string) (nexusclient.RequestSummary, int, error) {
	return nexusclient.RequestSummary{Status: f.decision, Decision: f.decision}, 200, nil
}

func (f *fakeNexus) ReportResult(_ context.Context, _ string, _ bool, _ map[string]any, _ int64, _ string) (int, error) {
	f.reportCalls++
	return 200, nil
}

type fakeConnectorExecutor struct {
	connectorID   uuid.UUID
	connectorKind string
	connectorOrg  string
	execCalls     int
	readCalls     int
	lastSpec      connectordomain.ExecutionSpec
	readResults   map[string]json.RawMessage
}

type fakeProductGuard struct {
	err            error
	calls          int
	productSurface string
	reason         string
}

func (f *fakeProductGuard) RequireActiveInstallation(_ context.Context, _ string, productSurface, reason string) error {
	f.calls++
	f.productSurface = productSurface
	f.reason = reason
	return f.err
}

type denyingWatcherRateLimiter struct{}

func (denyingWatcherRateLimiter) Allow(context.Context, productlimits.Key, productlimits.Limit) (productlimits.Decision, error) {
	return productlimits.Decision{Allowed: false}, nil
}

func (f *fakeConnectorExecutor) ListConnectors(context.Context) ([]connectordomain.Connector, error) {
	if f.connectorID == uuid.Nil {
		f.connectorID = uuid.New()
	}
	kind := f.connectorKind
	if kind == "" {
		kind = "pymes"
	}
	orgID := f.connectorOrg
	if orgID == "" {
		orgID = "org-1"
	}
	return []connectordomain.Connector{{ID: f.connectorID, OrgID: orgID, Kind: kind, Enabled: true}}, nil
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
		"target_system":   spec.ProductSurface,
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
		ID:             uuid.New(),
		ConnectorID:    spec.ConnectorID,
		OrgID:          spec.OrgID,
		ActorID:        spec.ActorID,
		Operation:      spec.Operation,
		Status:         connectordomain.ExecSuccess,
		ExternalRef:    "pymes-send",
		Payload:        spec.Payload,
		ResultJSON:     json.RawMessage(`{"sent":true}`),
		IdempotencyKey: spec.IdempotencyKey,
		NexusRequestID: spec.NexusRequestID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

// --- tests ---

func TestUsecases_Create(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})

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
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})

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
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})

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
	nexus := &fakeNexus{decision: "allowed"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, nexus)
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
	if nexus.reportCalls != 2 {
		t.Fatalf("expected 2 nexus result reports, got %d", nexus.reportCalls)
	}
	if len(repo.proposals) != 2 {
		t.Fatalf("expected 2 persisted proposals, got %d", len(repo.proposals))
	}
}

func TestUsecases_RunWatcher_GenericCapability_AutoExecutesNonPymes(t *testing.T) {
	t.Parallel()
	nexus := &fakeNexus{decision: "allowed"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, nexus)
	executor := &fakeConnectorExecutor{
		connectorKind: "demo",
		connectorOrg:  "org-1",
		readResults: map[string]json.RawMessage{
			"demo.orders.search": json.RawMessage(`{"items":[
				{"id":"order-1","type":"order","name":"Late order","status":"late","contact_id":"party-1"},
				{"id":"order-2","type":"order","name":"Ok order","status":"ok","contact_id":"party-2"}
			]}`),
		},
	}
	uc.SetConnectorExecutor(executor)

	config := json.RawMessage(`{
		"product_surface":"demo",
		"connector_kind":"demo",
		"query_operation":"demo.orders.search",
		"query_payload":{"status":"late"},
		"result_items_path":"items",
		"condition":{"path":"status","operator":"eq","value":"late"},
		"action_operation":"demo.orders.notify",
		"action_payload_template":{"org_id":"${org_id}","party_id":"${party_id}","body":"${watcher_message}"},
		"action_type":"notification.send"
	}`)
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Demo orders", WatcherType: domain.WatcherCapability,
		Config: config, Enabled: true,
	})

	result, err := uc.RunWatcher(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found != 1 || result.Proposed != 1 || result.Executed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if executor.readCalls != 1 || executor.execCalls != 1 {
		t.Fatalf("unexpected connector calls: read=%d exec=%d", executor.readCalls, executor.execCalls)
	}
	if executor.lastSpec.ProductSurface != "demo" || executor.lastSpec.Operation != "demo.orders.notify" {
		t.Fatalf("generic watcher used wrong execution spec: %+v", executor.lastSpec)
	}
	var payload map[string]any
	if err := json.Unmarshal(executor.lastSpec.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["org_id"] != "org-1" || payload["party_id"] != "party-1" {
		t.Fatalf("unexpected action payload: %+v", payload)
	}
}

func TestUsecases_RunWatcher_GenericCapability_ProposalOnlyDoesNotExecute(t *testing.T) {
	t.Parallel()
	nexus := &fakeNexus{decision: "allowed"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, nexus)
	executor := &fakeConnectorExecutor{
		connectorKind: "demo",
		connectorOrg:  "org-1",
		readResults: map[string]json.RawMessage{
			"demo.stock.summary": json.RawMessage(`{"items":[{"id":"stock-1","type":"stock","name":"Seed low","status":"risk"}]}`),
		},
	}
	uc.SetConnectorExecutor(executor)

	config := json.RawMessage(`{
		"product_surface":"demo",
		"connector_kind":"demo",
		"query_operation":"demo.stock.summary",
		"result_items_path":"items",
		"condition":{"path":"status","operator":"eq","value":"risk"},
		"action_type":"demo.axis.propose.stock_review",
		"proposal_only":true
	}`)
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Demo stock", WatcherType: domain.WatcherCapability,
		Config: config, Enabled: true,
	})

	result, err := uc.RunWatcher(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found != 1 || result.Proposed != 1 || result.Executed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if executor.readCalls != 1 || executor.execCalls != 0 {
		t.Fatalf("unexpected connector calls: read=%d exec=%d", executor.readCalls, executor.execCalls)
	}
	if nexus.submitCalls != 0 || nexus.reportCalls != 0 {
		t.Fatalf("proposal-only should not call nexus: submit=%d report=%d", nexus.submitCalls, nexus.reportCalls)
	}
	if len(repo.proposals) != 1 || repo.proposals[0].ExecutionStatus != domain.ProposalPending {
		t.Fatalf("expected one pending proposal, got %+v", repo.proposals)
	}
}

func TestUsecases_RunWatcher_GenericCapability_FailsClosedOnConnectorOrgMismatch(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})
	executor := &fakeConnectorExecutor{
		connectorKind: "demo",
		connectorOrg:  "org-other",
		readResults: map[string]json.RawMessage{
			"demo.orders.search": json.RawMessage(`{"items":[{"id":"order-1"}]}`),
		},
	}
	uc.SetConnectorExecutor(executor)

	config := json.RawMessage(`{
		"product_surface":"demo",
		"connector_kind":"demo",
		"query_operation":"demo.orders.search",
		"result_items_path":"items",
		"action_operation":"demo.orders.notify",
		"action_payload_template":{"org_id":"${org_id}"},
		"action_type":"notification.send"
	}`)
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Demo orders", WatcherType: domain.WatcherCapability,
		Config: config, Enabled: true,
	})

	_, err := uc.RunWatcher(context.Background(), w.ID)
	if err == nil {
		t.Fatal("expected connector org mismatch to fail closed")
	}
	if executor.readCalls != 0 || executor.execCalls != 0 {
		t.Fatalf("expected no connector execution, got read=%d exec=%d", executor.readCalls, executor.execCalls)
	}
}

func TestUsecases_RunWatcher_BlocksExternalWithoutActiveInstallation(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})
	executor := &fakeConnectorExecutor{
		connectorKind: "demo",
		connectorOrg:  "org-1",
		readResults: map[string]json.RawMessage{
			"demo.orders.search": json.RawMessage(`{"items":[{"id":"order-1"}]}`),
		},
	}
	uc.SetConnectorExecutor(executor)
	guard := &fakeProductGuard{err: errors.New("active product installation required")}
	uc.SetProductInstallationGuard(guard)

	config := json.RawMessage(`{
		"product_surface":"demo",
		"connector_kind":"demo",
		"query_operation":"demo.orders.search",
		"result_items_path":"items",
		"action_operation":"demo.orders.notify",
		"action_payload_template":{"org_id":"${org_id}"},
		"action_type":"notification.send"
	}`)
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Demo orders", WatcherType: domain.WatcherCapability,
		Config: config, Enabled: true,
	})

	_, err := uc.RunWatcher(context.Background(), w.ID)
	if !domainerr.IsForbidden(err) {
		t.Fatalf("expected forbidden installation guard error, got %v", err)
	}
	if guard.calls != 1 || guard.productSurface != "demo" || guard.reason != "watcher_query" {
		t.Fatalf("unexpected guard call: %+v", guard)
	}
	if executor.readCalls != 0 || executor.execCalls != 0 {
		t.Fatalf("expected no connector execution, got read=%d exec=%d", executor.readCalls, executor.execCalls)
	}
}

func TestUsecases_RunWatcher_RateLimitedByProduct(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})
	executor := &fakeConnectorExecutor{
		connectorKind: "demo",
		connectorOrg:  "org-1",
		readResults: map[string]json.RawMessage{
			"demo.orders.search": json.RawMessage(`{"items":[{"id":"order-1"}]}`),
		},
	}
	uc.SetConnectorExecutor(executor)
	uc.SetRateLimiter(denyingWatcherRateLimiter{})

	config := json.RawMessage(`{
		"product_surface":"demo",
		"connector_kind":"demo",
		"query_operation":"demo.orders.search",
		"result_items_path":"items",
		"action_operation":"demo.orders.notify",
		"action_payload_template":{"org_id":"${org_id}"},
		"action_type":"notification.send"
	}`)
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Demo orders", WatcherType: domain.WatcherCapability,
		Config: config, Enabled: true,
	})

	_, err := uc.RunWatcher(context.Background(), w.ID)
	if !productlimits.IsRateLimited(err) {
		t.Fatalf("expected watcher product rate limit error, got %v", err)
	}
	if executor.readCalls != 0 || executor.execCalls != 0 {
		t.Fatalf("expected no connector execution, got read=%d exec=%d", executor.readCalls, executor.execCalls)
	}
}

func TestUsecases_RunWatcher_DeniedSkipsExecution(t *testing.T) {
	t.Parallel()
	nexus := &fakeNexus{decision: "denied"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, nexus)
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
	if nexus.reportCalls != 0 {
		t.Fatalf("expected 0 nexus reports when denied, got %d", nexus.reportCalls)
	}
}

func TestUsecases_Delete(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeNexus{})

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

func TestUsecases_EnqueueWatcherRunsUsesDurableJobQueue(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	queue := jobs.NewMemoryRepository()
	uc := NewUsecases(repo, &fakeNexus{})
	uc.SetJobQueue(queue)
	enabled, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Enabled", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{"threshold_units":1}`), Enabled: true,
	})
	_, _ = uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Disabled", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{"threshold_units":1}`), Enabled: false,
	})

	count, err := uc.EnqueueWatcherRuns(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one watcher job enqueued, got %d", count)
	}
	claimed, err := queue.Claim(context.Background(), jobs.ClaimOptions{WorkerID: "w1", Kinds: []string{JobKindWatcherRun}, BatchSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].Kind != JobKindWatcherRun {
		t.Fatalf("expected watcher.run job, got %+v", claimed)
	}
	var payload watcherRunJobPayload
	if err := json.Unmarshal(claimed[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.WatcherID != enabled.ID.String() {
		t.Fatalf("expected watcher_id %s, got %s", enabled.ID, payload.WatcherID)
	}
	if payload.ProductSurface != "pymes" || claimed[0].ProductSurface != "pymes" {
		t.Fatalf("expected pymes product scope, payload=%+v job=%+v", payload, claimed[0])
	}
}

func TestUsecases_JobHandlerRunsWatcherAndCompletes(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	queue := jobs.NewMemoryRepository()
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})
	uc.SetConnectorExecutor(&fakeConnectorExecutor{readResults: map[string]json.RawMessage{
		"pymes.get_low_stock": json.RawMessage(`[]`),
	}})
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Low stock", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{"threshold_units":1}`), Enabled: true,
	})
	payload, err := json.Marshal(watcherRunJobPayload{WatcherID: w.ID.String(), ProductSurface: "pymes"})
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := queue.Enqueue(context.Background(), jobs.EnqueueInput{
		OrgID:          w.OrgID,
		ProductSurface: "pymes",
		Kind:           JobKindWatcherRun,
		ShardKey:       w.OrgID + ":pymes",
		DedupeKey:      "watcher.run:" + w.ID.String(),
		Payload:        payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := jobs.NewWorker(queue, jobs.WorkerConfig{WorkerID: "w1", Concurrency: 1})
	uc.RegisterJobHandlers(worker)
	if claimed, err := worker.RunOnce(context.Background()); err != nil || claimed != 1 {
		t.Fatalf("worker claimed=%d err=%v", claimed, err)
	}
	stored, err := queue.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != jobs.StatusSucceeded {
		t.Fatalf("expected job succeeded, got %+v", stored)
	}
}

func TestUsecases_JobHandlerRejectsProductSurfaceMismatch(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakeNexus{decision: "allowed"})
	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Low stock", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{"threshold_units":1}`), Enabled: true,
	})
	payload, err := json.Marshal(watcherRunJobPayload{WatcherID: w.ID.String(), ProductSurface: "ponti"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = uc.handleWatcherRunJob(context.Background(), jobs.Job{
		OrgID:          w.OrgID,
		ProductSurface: "ponti",
		Kind:           JobKindWatcherRun,
		Payload:        payload,
	})
	if !jobs.IsPermanent(err) {
		t.Fatalf("expected permanent product mismatch error, got %v", err)
	}
}
