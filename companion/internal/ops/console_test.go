package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/companion/internal/jobs"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/securityevals"
	authn "github.com/devpablocristo/platform/authn/go"
	"github.com/devpablocristo/platform/authn/go/identityhttp"
	"github.com/google/uuid"
)

type fakeOpsStore struct {
	products      []products.Product
	installations []products.Installation
	manifests     []capabilities.ManifestRecord
	conformance   []capabilities.ConformanceRun
	evals         []securityevals.Report
	events        []runtime.ObservabilityEvent
	cost          runtime.CostSummary
	policy        runtime.TenantRuntimePolicy
	usage         runtime.TenantRuntimeUsage
}

func (f fakeOpsStore) ListProducts(context.Context) ([]products.Product, error) {
	return f.products, nil
}

func (f fakeOpsStore) ListInstallations(_ context.Context, orgID string) ([]products.Installation, error) {
	out := make([]products.Installation, 0)
	for _, item := range f.installations {
		if item.OrgID == orgID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (f fakeOpsStore) ListManifests(context.Context, capabilities.ManifestFilter) ([]capabilities.ManifestRecord, error) {
	return f.manifests, nil
}

func (f fakeOpsStore) ListConformanceRuns(context.Context, string, string, int) ([]capabilities.ConformanceRun, error) {
	return f.conformance, nil
}

func (f fakeOpsStore) ListReports(_ context.Context, orgID, productSurface, _ string, _ int) ([]securityevals.Report, error) {
	out := make([]securityevals.Report, 0)
	for _, item := range f.evals {
		if item.OrgID != orgID {
			continue
		}
		if productSurface != "" && item.ProductSurface != productSurface {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (f fakeOpsStore) ListObservabilityEvents(_ context.Context, _ runtime.ObservabilityEventFilter) ([]runtime.ObservabilityEvent, error) {
	return nil, nil
}

func (f fakeOpsStore) GetCostSummary(context.Context, string, string, string, int) (runtime.CostSummary, error) {
	return f.cost, nil
}

func (f fakeOpsStore) GetRuntimePolicy(context.Context, string) (runtime.TenantRuntimePolicy, error) {
	return f.policy, nil
}

func (f fakeOpsStore) GetRuntimeUsage(context.Context, string, string) (runtime.TenantRuntimeUsage, error) {
	return f.usage, nil
}

func TestUsecasesGetConsoleBuildsOperationalAlertsAndSLOs(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	store := fakeOpsStore{
		products: []products.Product{{
			ProductSurface: "ponti",
			DisplayName:    "Ponti",
			Status:         products.ProductStatusActive,
		}},
		installations: []products.Installation{{
			OrgID:          "org-a",
			ProductSurface: "ponti",
			BaseURL:        "https://ponti.example.com",
			AuthMode:       products.AuthModeInternalJWT,
			Enabled:        false,
			UpdatedAt:      now,
		}},
		manifests: []capabilities.ManifestRecord{{
			Manifest: capabilities.Manifest{CapabilityID: "ponti.summary", ProductSurface: "ponti", Version: "1.0.0"},
			Status:   capabilities.ManifestStatusActive,
		}},
		conformance: []capabilities.ConformanceRun{{
			OrgID:        "org-a",
			CapabilityID: "ponti.summary",
			Version:      "1.0.0",
			Status:       capabilities.ConformanceStatusFailed,
			Errors:       []string{"evidence_schema missing"},
			CreatedAt:    now,
		}},
		evals: []securityevals.Report{{
			OrgID:          "org-a",
			ProductSurface: "ponti",
			Suite:          "ponti-golden",
			Status:         "failed",
			Score:          0.5,
			Threshold:      0.9,
			CreatedAt:      now,
		}},
		events: []runtime.ObservabilityEvent{{
			OrgID:          "org-a",
			ProductSurface: "ponti",
			EventType:      "guardrail",
			EventName:      "product_rate_limit",
			OccurredAt:     now,
		}, {
			OrgID:          "org-a",
			ProductSurface: "ponti",
			EventType:      "mcp",
			EventName:      "mcp_tool_call",
			Severity:       "info",
			Payload:        []byte(`{"tool_name":"axis.products.list","status":"executed","duration_ms":120}`),
			OccurredAt:     now,
		}, {
			OrgID:          "org-a",
			ProductSurface: "ponti",
			EventType:      "guardrail",
			EventName:      "mcp_runtime_policy",
			Payload:        []byte(`{"tool_name":"axis.products.list","target":"tool:axis.products.list","reason":"tool is denied for this customer org"}`),
			OccurredAt:     now,
		}, {
			OrgID:          "org-a",
			ProductSurface: "ponti",
			EventType:      "guardrail",
			EventName:      "mcp_scope_required",
			Payload:        []byte(`{"tool_name":"axis.costs.summary","target":"tool:axis.costs.summary","reason":"mcp tool required scopes are missing","missing_scopes":["companion:costs:read"]}`),
			OccurredAt:     now,
		}},
		cost: runtime.CostSummary{
			OrgID:              "org-a",
			ProductSurface:     "ponti",
			Period:             "2026-06",
			EstimatedCostCents: 90,
			ToolCalls:          45,
		},
		policy: runtime.TenantRuntimePolicy{
			OrgID: "org-a",
			ControlPlane: runtime.OrgControlPlaneSettings{
				ProductPolicies: map[string]runtime.ProductRuntimePolicy{
					"ponti": {MonthlyCostBudgetCents: 100, MonthlyToolCallBudget: 50},
				},
			},
		},
		usage: runtime.TenantRuntimeUsage{
			OrgID:      "org-a",
			Period:     "2026-06",
			ToolCalls:  10,
			ToolErrors: 2,
			UpdatedAt:  now,
		},
	}
	uc := NewUsecases(Deps{
		Products:        store,
		Capabilities:    store,
		Evals:           store,
		Observability:   staticEvents{events: store.events},
		Costs:           store,
		RuntimeControls: store,
	})
	console, err := uc.GetConsole(context.Background(), Query{OrgID: "org-a", ProductSurface: "ponti", Period: "2026-06"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"installation_disabled", "capability_conformance_failed", "eval_regression", "rate_limit_abuse", "mcp_runtime_policy_block", "mcp_scope_block", "cost_anomaly", "high_error_rate"} {
		if !hasAlert(console.Alerts, want) {
			t.Fatalf("expected alert %s in %+v", want, console.Alerts)
		}
	}
	if len(console.SLOs) != 1 || console.SLOs[0].Status != "critical" {
		t.Fatalf("expected critical ponti slo, got %+v", console.SLOs)
	}
	if len(console.RuntimeLimits) != 1 {
		t.Fatalf("expected one runtime limit row, got %+v", console.RuntimeLimits)
	}
	limit := console.RuntimeLimits[0]
	if limit.ProductSurface != "ponti" || limit.Status != "warning" {
		t.Fatalf("expected warning ponti runtime limit, got %+v", limit)
	}
	if limit.CostUsedCents != 90 || limit.CostLimitCents != 100 || limit.CostUsageRatio != 0.9 || limit.CostLimitSource != "product_policy" {
		t.Fatalf("unexpected cost limit usage: %+v", limit)
	}
	if limit.ToolCallsUsed != 45 || limit.ToolCallLimit != 50 || limit.ToolCallUsageRatio != 0.9 || limit.ToolCallSource != "product_policy" {
		t.Fatalf("unexpected tool call limit usage: %+v", limit)
	}
	if len(console.Metrics) != 1 || console.Metrics[0].P95LatencyMS != 120 || console.Metrics[0].MCPToolCalls != 1 {
		t.Fatalf("expected latency and MCP metrics, got %+v", console.Metrics)
	}
	if console.SLOs[0].Latency.Status != "ok" || console.SLOs[0].Latency.Value != 120 {
		t.Fatalf("expected latency SLO from metrics, got %+v", console.SLOs[0].Latency)
	}
}

func TestUsecasesGetConsoleDedupesRepeatedGuardrailAlerts(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	events := []runtime.ObservabilityEvent{{
		OrgID:          "org-a",
		ProductSurface: "companion",
		EventType:      "guardrail",
		EventName:      "mcp_runtime_policy",
		Payload:        []byte(`{"tool_name":"axis.products.list","target":"tool:axis.products.list","reason":"tool denied"}`),
		OccurredAt:     now,
	}, {
		OrgID:          "org-a",
		ProductSurface: "companion",
		EventType:      "guardrail",
		EventName:      "mcp_runtime_policy",
		Payload:        []byte(`{"tool_name":"axis.products.list","target":"tool:axis.products.list","reason":"tool denied"}`),
		OccurredAt:     now.Add(-2 * time.Minute),
	}}
	uc := NewUsecases(Deps{Observability: staticEvents{events: events}})

	console, err := uc.GetConsole(context.Background(), Query{OrgID: "org-a", ProductSurface: "companion"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var found Alert
	for _, item := range console.Alerts {
		if item.Type == "mcp_runtime_policy_block" {
			count++
			found = item
		}
	}
	if count != 1 {
		t.Fatalf("expected one deduped MCP runtime policy alert, got %d alerts=%+v", count, console.Alerts)
	}
	if found.Evidence["suppressed_count"] != 1 {
		t.Fatalf("expected one suppressed duplicate, got %+v", found.Evidence)
	}
}

func TestUsecasesGetConsoleBuildsJobHealthAlerts(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	deadline := now.Add(-2 * time.Minute)
	uc := NewUsecases(Deps{
		Jobs: staticJobs{jobs: []jobs.Job{{
			ID:             uuid.New(),
			OrgID:          "org-a",
			ProductSurface: "companion",
			Kind:           "watcher.run",
			Status:         jobs.StatusDeadLetter,
			Attempts:       3,
			MaxAttempts:    3,
			LastError:      "permanent failure",
			UpdatedAt:      now,
		}, {
			ID:             uuid.New(),
			OrgID:          "org-a",
			ProductSurface: "companion",
			Kind:           "memory.embedding",
			Status:         jobs.StatusRunning,
			Attempts:       1,
			MaxAttempts:    3,
			LeaseUntil:     &expired,
			DeadlineAt:     &deadline,
			TimeoutSeconds: 30,
			UpdatedAt:      now,
		}}},
	})

	console, err := uc.GetConsole(context.Background(), Query{OrgID: "org-a", ProductSurface: "companion"})
	if err != nil {
		t.Fatal(err)
	}
	if len(console.JobHealth) != 1 {
		t.Fatalf("expected one job health row, got %+v", console.JobHealth)
	}
	health := console.JobHealth[0]
	if health.DeadLetter != 1 || health.ExpiredLeases != 1 || health.StuckJobs != 1 {
		t.Fatalf("unexpected job health: %+v", health)
	}
	for _, want := range []string{"job_dead_letter", "job_expired_lease", "job_stuck"} {
		if !hasAlert(console.Alerts, want) {
			t.Fatalf("expected alert %s in %+v", want, console.Alerts)
		}
	}
}

func TestHandlerRequiresOpsScope(t *testing.T) {
	t.Parallel()

	uc := NewUsecases(Deps{})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/ops/alerts?org_id=org-a", nil)
	req = withPrincipal(req, []string{"companion:tasks:read"})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden without ops scope, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestHandlerAllowsOpsScope(t *testing.T) {
	t.Parallel()

	store := fakeOpsStore{}
	uc := NewUsecases(Deps{Products: store})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/ops/alerts?org_id=org-a", nil)
	req = withPrincipal(req, []string{"companion:ops:read"})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected ok with ops scope, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestHandlerDispatchAlertsPostsWebhook(t *testing.T) {
	t.Parallel()

	var received AlertDispatchPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST webhook, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode webhook payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	uc := NewUsecases(Deps{
		Observability: staticEvents{events: []runtime.ObservabilityEvent{{
			OrgID:          "org-a",
			ProductSurface: "companion",
			EventType:      "guardrail",
			EventName:      "mcp_runtime_policy",
			Payload:        []byte(`{"tool_name":"axis.products.list","target":"tool:axis.products.list","reason":"denied"}`),
			OccurredAt:     time.Now().UTC(),
		}}},
		AlertSink: NewWebhookAlertSink(server.URL, time.Second),
	})
	mux := http.NewServeMux()
	NewHandler(uc).Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/ops/alerts/dispatch?org_id=org-a&product_surface=companion", nil)
	req = withPrincipal(req, []string{"companion:runtime:admin"})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("expected accepted dispatch, got %d body=%s", res.Code, res.Body.String())
	}
	if received.OrgID != "org-a" || received.ProductSurface != "companion" || len(received.Alerts) != 1 || received.Alerts[0].Type != "mcp_runtime_policy_block" {
		t.Fatalf("unexpected webhook payload: %+v", received)
	}
}

type staticEvents struct {
	events []runtime.ObservabilityEvent
}

func (s staticEvents) ListObservabilityEvents(context.Context, runtime.ObservabilityEventFilter) ([]runtime.ObservabilityEvent, error) {
	return s.events, nil
}

type staticJobs struct {
	jobs []jobs.Job
}

func (s staticJobs) List(_ context.Context, orgID, productSurface, status string, _ int) ([]jobs.Job, error) {
	out := make([]jobs.Job, 0)
	for _, item := range s.jobs {
		if item.OrgID != orgID {
			continue
		}
		if productSurface != "" && item.ProductSurface != productSurface {
			continue
		}
		if status != "" && string(item.Status) != status {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func hasAlert(alerts []Alert, kind string) bool {
	for _, item := range alerts {
		if item.Type == kind {
			return true
		}
	}
	return false
}

func withPrincipal(req *http.Request, scopes []string) *http.Request {
	principal := &authn.Principal{OrgID: "org-a", Actor: "user-a", Scopes: scopes, AuthMethod: "internal_jwt"}
	req = identityhttp.WithPrincipal(req, principal, "internal_jwt")
	return identityctx.WithPrincipal(req, principal)
}
