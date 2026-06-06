package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/productlimits"
)

type fakeConnectorRepo struct {
	connectors map[uuid.UUID]domain.Connector
	executions []domain.ExecutionResult
	locks      map[string]bool
	mu         sync.Mutex
}

func (f *fakeConnectorRepo) SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connectors == nil {
		f.connectors = make(map[uuid.UUID]domain.Connector)
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.connectors[c.ID] = c
	return c, nil
}

func (f *fakeConnectorRepo) GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.connectors[id]
	if !ok {
		return domain.Connector{}, ErrNotFound
	}
	return c, nil
}

func (f *fakeConnectorRepo) ListConnectors(ctx context.Context) ([]domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Connector
	for _, c := range f.connectors {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeConnectorRepo) UpdateConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectors[c.ID] = c
	return c, nil
}

func (f *fakeConnectorRepo) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.connectors, id)
	return nil
}

func (f *fakeConnectorRepo) SaveExecution(ctx context.Context, r domain.ExecutionResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executions = append(f.executions, r)
	return nil
}

func (f *fakeConnectorRepo) AcquireExecutionLock(ctx context.Context, lockKey string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lockKey == "" {
		return true, nil
	}
	if f.locks == nil {
		f.locks = make(map[string]bool)
	}
	if f.locks[lockKey] {
		return false, nil
	}
	f.locks[lockKey] = true
	return true, nil
}

func (f *fakeConnectorRepo) ReleaseExecutionLock(ctx context.Context, lockKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lockKey == "" {
		return nil
	}
	delete(f.locks, lockKey)
	return nil
}

func (f *fakeConnectorRepo) GetExecutionByIdempotency(ctx context.Context, taskID uuid.UUID, operation string, nexusRequestID *uuid.UUID, idempotencyKey string) (domain.ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, execution := range f.executions {
		if execution.TaskID == nil || *execution.TaskID != taskID {
			continue
		}
		if execution.Operation != operation || execution.IdempotencyKey != idempotencyKey {
			continue
		}
		if nexusRequestID == nil && execution.NexusRequestID == nil {
			return execution, nil
		}
		if nexusRequestID != nil && execution.NexusRequestID != nil && *nexusRequestID == *execution.NexusRequestID {
			return execution, nil
		}
	}
	return domain.ExecutionResult{}, ErrNotFound
}

func (f *fakeConnectorRepo) ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ExecutionResult
	for _, execution := range f.executions {
		if execution.ConnectorID == connectorID {
			out = append(out, execution)
		}
	}
	return out, nil
}

type stubChecker struct {
	approved bool
	err      error
	calls    int
	last     NexusExecutionIntent
}

func (s *stubChecker) AuthorizeExecution(ctx context.Context, intent NexusExecutionIntent) (bool, error) {
	s.calls++
	s.last = intent
	return s.approved, s.err
}

type stubInstallationGuard struct {
	err            error
	calls          int
	orgID          string
	productSurface string
	reason         string
}

func (s *stubInstallationGuard) RequireActiveInstallation(_ context.Context, orgID, productSurface, reason string) error {
	s.calls++
	s.orgID = orgID
	s.productSurface = productSurface
	s.reason = reason
	return s.err
}

type denyingConnectorRateLimiter struct{}

func (denyingConnectorRateLimiter) Allow(context.Context, productlimits.Key, productlimits.Limit) (productlimits.Decision, error) {
	return productlimits.Decision{Allowed: false}, nil
}

type stubProductRuntimeController struct {
	err            error
	calls          int
	orgID          string
	productSurface string
}

func (s *stubProductRuntimeController) EnforceProductConnectorExecution(_ context.Context, orgID, productSurface string) error {
	s.calls++
	s.orgID = orgID
	s.productSurface = productSurface
	return s.err
}

func TestNexusCheckerAdapter_AllowsExecutedStatus(t *testing.T) {
	t.Parallel()

	intent := NexusExecutionIntent{NexusRequestID: uuid.New(), OrgID: "org-a", BindingHash: "hash"}
	adapter := NewNexusCheckerAdapter(func(ctx context.Context, id uuid.UUID) (NexusRequestMeta, int, error) {
		return NexusRequestMeta{Status: "executed", OrgID: "org-a", BindingHash: "hash"}, 200, nil
	})
	ok, err := adapter.AuthorizeExecution(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected executed nexus status to authorize connector execution")
	}
}

func TestNexusCheckerAdapter_RejectsBindingMismatch(t *testing.T) {
	t.Parallel()

	intent := NexusExecutionIntent{NexusRequestID: uuid.New(), OrgID: "org-a", BindingHash: "expected"}
	adapter := NewNexusCheckerAdapter(func(ctx context.Context, id uuid.UUID) (NexusRequestMeta, int, error) {
		return NexusRequestMeta{Status: "approved", OrgID: "org-a", BindingHash: "different"}, 200, nil
	})
	ok, err := adapter.AuthorizeExecution(context.Background(), intent)
	if !errors.Is(err, ErrUngated) {
		t.Fatalf("expected ErrUngated, got %v", err)
	}
	if ok {
		t.Fatal("expected binding mismatch to deny execution")
	}
}

func TestUsecases_ExecuteBlocksExternalWithoutActiveInstallation(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	guard := &stubInstallationGuard{err: errors.New("active product installation required")}
	uc := NewUsecases(repo, reg, &stubChecker{})
	uc.SetProductInstallationGuard(guard)

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		ProductSurface: "mock",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.echo",
		Payload:        json.RawMessage(`{"message":"hello"}`),
	})
	if !IsForbidden(err) {
		t.Fatalf("expected forbidden guard error, got %v", err)
	}
	if guard.calls != 1 || guard.orgID != "org-a" || guard.productSurface != "mock" || guard.reason != "connector_execution" {
		t.Fatalf("unexpected guard call: %+v", guard)
	}
	if len(repo.executions) != 0 {
		t.Fatalf("blocked connector execution should not persist executions, got %d", len(repo.executions))
	}
}

func TestUsecases_BuildActionBindingBlocksExternalWithoutActiveInstallation(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	guard := &stubInstallationGuard{err: errors.New("active product installation required")}
	uc := NewUsecases(repo, reg, &stubChecker{})
	uc.SetProductInstallationGuard(guard)

	_, _, err := uc.BuildActionBinding(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		ProductSurface: "mock",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-binding",
	})
	if !IsForbidden(err) {
		t.Fatalf("expected forbidden guard error, got %v", err)
	}
	if guard.reason != "connector_action_binding" {
		t.Fatalf("expected connector_action_binding guard reason, got %q", guard.reason)
	}
}

func TestUsecases_ExecuteBlocksRateLimitedProduct(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{})
	uc.SetRateLimiter(denyingConnectorRateLimiter{})

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		ProductSurface: "mock",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.echo",
		Payload:        json.RawMessage(`{"message":"hello"}`),
	})
	if !IsRateLimited(err) {
		t.Fatalf("expected product rate limit error, got %v", err)
	}
	if len(repo.executions) != 0 {
		t.Fatalf("rate limited connector execution should not persist executions, got %d", len(repo.executions))
	}
}

func TestUsecases_ExecuteBlocksDeniedProductRuntimePolicy(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	controller := &stubProductRuntimeController{err: ErrForbidden}
	uc := NewUsecases(repo, reg, &stubChecker{})
	uc.SetProductRuntimeController(controller)

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		ProductSurface: "mock",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.echo",
		Payload:        json.RawMessage(`{"message":"hello"}`),
	})
	if !IsForbidden(err) {
		t.Fatalf("expected product runtime policy forbidden error, got %v", err)
	}
	if controller.calls != 1 || controller.orgID != "org-a" || controller.productSurface != "mock" {
		t.Fatalf("unexpected product runtime controller call: %+v", controller)
	}
	if len(repo.executions) != 0 {
		t.Fatalf("policy-blocked connector execution should not persist executions, got %d", len(repo.executions))
	}
}

type blockingConnector struct {
	started chan struct{}
	release chan struct{}

	startOnce sync.Once
	mu        sync.Mutex
	calls     int
}

func (b *blockingConnector) ID() string   { return "blocking" }
func (b *blockingConnector) Kind() string { return "blocking" }

func (b *blockingConnector) Capabilities() []domain.Capability {
	return []domain.Capability{
		{
			Operation:             "blocking.write",
			Mode:                  domain.CapabilityModeWrite,
			SideEffect:            true,
			RequiresNexusApproval: true,
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"message"},
			},
		},
	}
}

func (b *blockingConnector) Validate(spec domain.ExecutionSpec) error {
	return nil
}

func (b *blockingConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.startOnce.Do(func() { close(b.started) })

	select {
	case <-ctx.Done():
		return domain.ExecutionResult{}, ctx.Err()
	case <-b.release:
	}

	resultJSON, err := json.Marshal(map[string]string{"status": "ok"})
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	return domain.ExecutionResult{
		ID:             uuid.New(),
		ConnectorID:    spec.ConnectorID,
		OrgID:          spec.OrgID,
		ActorID:        spec.ActorID,
		Operation:      spec.Operation,
		Status:         domain.ExecSuccess,
		ExternalRef:    "blocking-" + uuid.New().String()[:8],
		Payload:        spec.Payload,
		ResultJSON:     json.RawMessage(resultJSON),
		DurationMS:     1,
		IdempotencyKey: spec.IdempotencyKey,
		TaskID:         spec.TaskID,
		NexusRequestID: spec.NexusRequestID,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (b *blockingConnector) CallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func TestUsecases_Execute_resolvesConnectorByKind(t *testing.T) {
	t.Parallel()
	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	nexusRequestID := uuid.New()

	result, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-1",
		NexusRequestID: &nexusRequestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ConnectorID != connectorID {
		t.Fatalf("unexpected connector id %s", result.ConnectorID)
	}
	if len(repo.executions) != 1 {
		t.Fatalf("expected persisted execution, got %d", len(repo.executions))
	}
}

func TestUsecases_Execute_disabledConnector(t *testing.T) {
	t.Parallel()
	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: false,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connectorID,
		OrgID:       "org-a",
		ActorID:     "actor-1",
		AuthScopes:  []string{"companion:connectors:execute"},
		Operation:   "mock.echo",
		Payload:     json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestUsecases_CapabilitiesExposeConnectorContractV1(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	caps := uc.Capabilities(domain.CapabilityFilter{IncludeWrites: true})
	if len(caps) != 1 {
		t.Fatalf("expected one connector, got %d", len(caps))
	}
	var writeCap domain.Capability
	for _, cap := range caps[0].Capabilities {
		if cap.Operation == "mock.write" {
			writeCap = cap
			break
		}
	}
	if writeCap.Operation == "" {
		t.Fatal("mock.write capability not found")
	}
	if writeCap.Mode != domain.CapabilityModeWrite {
		t.Fatalf("expected write mode, got %q", writeCap.Mode)
	}
	if !writeCap.RequiresNexusApproval || !writeCap.SideEffect {
		t.Fatalf("expected requires_nexus_approval side-effect capability: %+v", writeCap)
	}
	if writeCap.RiskClass == "" {
		t.Fatal("expected risk_class")
	}
	if writeCap.ID != "mock.write" || writeCap.Version == "" || writeCap.OwnerDomain != "mock" || writeCap.Product != "mock" {
		t.Fatalf("expected normalized manifest identity fields: %+v", writeCap)
	}
	if writeCap.TenantScope.Mode == "" || writeCap.AuthMode.Type == "" || !writeCap.ApprovalPolicy.Required {
		t.Fatalf("expected tenant/auth/approval defaults: %+v", writeCap)
	}
	if !writeCap.Idempotency.Required || len(writeCap.ErrorContract.TypedErrors) == 0 || !writeCap.Observability.EmitTrace {
		t.Fatalf("expected idempotency, typed errors and observability: %+v", writeCap)
	}
	if len(writeCap.InputSchema) == 0 || len(writeCap.EvidenceFields) == 0 {
		t.Fatalf("expected schema and evidence fields: %+v", writeCap)
	}
	if writeCap.DisplayName == "" || writeCap.Description == "" || writeCap.Connector != "mock" || writeCap.ActionType == "" {
		t.Fatalf("expected explicit manifest fields: %+v", writeCap)
	}
	if writeCap.NexusActionType != "agent.capability.invoke" || writeCap.IdempotencyMode != "required" || !writeCap.EnabledByDefault {
		t.Fatalf("expected approval/idempotency defaults from manifest registry: %+v", writeCap)
	}
	if writeCap.Timeout == "" || writeCap.Retries.MaxAttempts < 1 || len(writeCap.ObservabilityTags) == 0 {
		t.Fatalf("expected operational manifest fields: %+v", writeCap)
	}
}

func TestUsecases_CapabilityManifestsExposeVersionedContracts(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	manifests, err := uc.CapabilityManifests(domain.CapabilityFilter{
		Scopes:             []string{"companion:connectors:execute"},
		IncludeWrites:      true,
		EnforcePermissions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected mock manifests, got %+v", manifests)
	}
	var writeManifestFound bool
	for _, manifest := range manifests {
		if manifest.CapabilityID != "mock.write" {
			continue
		}
		writeManifestFound = true
		if manifest.SchemaVersion != "capability_manifest.v1" || manifest.NexusActionType != "agent.capability.invoke" {
			t.Fatalf("unexpected write manifest identity: %+v", manifest)
		}
		if manifest.InputSchema["type"] != "object" || manifest.EvidenceSchema["type"] != "object" {
			t.Fatalf("expected strict object schemas: %+v", manifest)
		}
	}
	if !writeManifestFound {
		t.Fatalf("mock.write manifest not found: %+v", manifests)
	}
}

func TestUsecases_BuildActionBindingIncludesCapabilityManifestFields(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{})
	taskID := uuid.New()

	binding, hash, err := uc.BuildActionBinding(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-binding",
		TaskID:         &taskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected binding hash")
	}
	if binding["capability_id"] != "mock.write" || binding["capability_version"] != "1.0.0" {
		t.Fatalf("expected capability identity in binding, got %+v", binding)
	}
	if binding["nexus_action_type"] != "agent.capability.invoke" || binding["side_effect_type"] != domain.SideEffectClassWrite {
		t.Fatalf("expected approval manifest fields in binding, got %+v", binding)
	}
	if binding["cost_class"] == "" || binding["rate_limit_class"] == "" {
		t.Fatalf("expected operational classes in binding, got %+v", binding)
	}
}

func TestUsecases_CapabilitiesFiltersWritesByDefault(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	caps := uc.Capabilities(domain.CapabilityFilter{
		Scopes: []string{"companion:connectors:execute"},
	})
	if len(caps) != 1 {
		t.Fatalf("expected one connector, got %d", len(caps))
	}
	if len(caps[0].Capabilities) != 1 {
		t.Fatalf("expected only read-only capability, got %+v", caps[0].Capabilities)
	}
	if caps[0].Capabilities[0].Operation != "mock.echo" {
		t.Fatalf("expected mock.echo, got %s", caps[0].Capabilities[0].Operation)
	}
}

func TestUsecases_CapabilitiesFiltersByPermissionsWhenAuthContextExists(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	caps := uc.Capabilities(domain.CapabilityFilter{
		IncludeWrites:      true,
		EnforcePermissions: true,
	})
	if len(caps) != 0 {
		t.Fatalf("expected no capabilities without required scopes, got %+v", caps)
	}

	caps = uc.Capabilities(domain.CapabilityFilter{
		Scopes:             []string{"companion:connectors:execute"},
		IncludeWrites:      true,
		EnforcePermissions: true,
	})
	if len(caps) != 1 || len(caps[0].Capabilities) != 2 {
		t.Fatalf("expected scoped discovery to expose mock capabilities, got %+v", caps)
	}
}

func TestUsecases_Execute_readOnlyDoesNotRequireNexus(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	checker := &stubChecker{approved: false}
	uc := NewUsecases(repo, reg, checker)

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connectorID,
		OrgID:       "org-a",
		ActorID:     "actor-1",
		AuthScopes:  []string{"companion:connectors:execute"},
		Operation:   "mock.echo",
		Payload:     json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if checker.calls != 0 {
		t.Fatalf("expected read-only execution to skip nexus checker, got %d calls", checker.calls)
	}
}

func TestUsecases_Execute_sideEffectWithoutNexusDenied(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-ungated",
	})
	if !errors.Is(err, ErrUngated) {
		t.Fatalf("expected ErrUngated, got %v", err)
	}
}

func TestUsecases_Execute_validatesInputSchema(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	nexusRequestID := uuid.New()

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{}`),
		IdempotencyKey: "idem-invalid",
		NexusRequestID: &nexusRequestID,
	})
	if !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("expected ErrInvalidPayload, got %v", err)
	}
}

func TestUsecases_Execute_persistsSanitizedEvidence(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	nexusRequestID := uuid.New()
	taskID := uuid.New()

	result, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:        connectorID,
		OrgID:              "org-a",
		ActorID:            "actor-1",
		ActorType:          "human",
		CompanionPrincipal: "companion.employee_ai",
		OnBehalfOf:         "actor-1",
		ServicePrincipal:   true,
		Operation:          "mock.write",
		Payload:            json.RawMessage(`{"message":"hello","api_key":"secret"}`),
		IdempotencyKey:     "idem-1",
		TaskID:             &taskID,
		NexusRequestID:     &nexusRequestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OrgID != "org-a" || result.ActorID != "actor-1" {
		t.Fatalf("expected org/actor on result, got %+v", result)
	}
	if len(repo.executions) != 1 {
		t.Fatalf("expected persisted execution, got %d", len(repo.executions))
	}
	evidence := string(repo.executions[0].EvidenceJSON)
	if !strings.Contains(evidence, `"org_id":"org-a"`) {
		t.Fatalf("expected org evidence, got %s", evidence)
	}
	if !strings.Contains(evidence, `"companion_principal":"companion.employee_ai"`) || !strings.Contains(evidence, `"on_behalf_of":"actor-1"`) {
		t.Fatalf("expected companion identity evidence, got %s", evidence)
	}
	if strings.Contains(evidence, "secret") || !strings.Contains(evidence, `"api_key":"***"`) {
		t.Fatalf("expected sanitized evidence, got %s", evidence)
	}
}

func TestUsecases_Execute_reusesIdempotentExecution(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	nexusRequestID := uuid.New()
	taskID := uuid.New()
	spec := domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-1",
		TaskID:         &taskID,
		NexusRequestID: &nexusRequestID,
	}

	first, err := uc.Execute(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Execute(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same idempotent execution, got %s and %s", first.ID, second.ID)
	}
	if len(repo.executions) != 1 {
		t.Fatalf("expected one persisted execution, got %d", len(repo.executions))
	}
}

func TestUsecases_Execute_conflictsConcurrentIdempotentExecution(t *testing.T) {
	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Blocking Connector",
		Kind:    "blocking",
		Enabled: true,
	}
	conn := &blockingConnector{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	reg := registry.NewRegistry()
	reg.Register(conn)
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	nexusRequestID := uuid.New()
	taskID := uuid.New()
	spec := domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-a",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "blocking.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-concurrent",
		TaskID:         &taskID,
		NexusRequestID: &nexusRequestID,
	}

	type executionResult struct {
		result domain.ExecutionResult
		err    error
	}
	done := make(chan executionResult, 1)
	go func() {
		result, err := uc.Execute(context.Background(), spec)
		done <- executionResult{result: result, err: err}
	}()

	<-conn.started
	_, err := uc.Execute(context.Background(), spec)
	if !IsConflict(err) {
		t.Fatalf("expected concurrent idempotent execution conflict, got %v", err)
	}
	if calls := conn.CallCount(); calls != 1 {
		t.Fatalf("expected only first execution to reach connector, got %d calls", calls)
	}

	close(conn.release)
	first := <-done
	if first.err != nil {
		t.Fatal(first.err)
	}
	again, err := uc.Execute(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.result.ID {
		t.Fatalf("expected stored idempotent result %s, got %s", first.result.ID, again.ID)
	}
	if calls := conn.CallCount(); calls != 1 {
		t.Fatalf("expected replay to skip connector, got %d calls", calls)
	}
}

func TestUsecases_Execute_rejectsConnectorTenantMismatch(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	nexusRequestID := uuid.New()

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:    connectorID,
		OrgID:          "org-b",
		ActorID:        "actor-1",
		AuthScopes:     []string{"companion:connectors:execute"},
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey: "idem-mismatch",
		NexusRequestID: &nexusRequestID,
	})
	if !IsForbidden(err) {
		t.Fatalf("expected forbidden tenant mismatch, got %v", err)
	}
}
