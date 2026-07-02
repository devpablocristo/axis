package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeObservabilityRepo struct {
	filter ObservabilityEventFilter
	events []ObservabilityEvent
}

func (f *fakeObservabilityRepo) RecordObservabilityEvent(context.Context, ObservabilityEvent) error {
	return nil
}

func (f *fakeObservabilityRepo) ListObservabilityEvents(_ context.Context, filter ObservabilityEventFilter) ([]ObservabilityEvent, error) {
	f.filter = filter
	return f.events, nil
}

func (f *fakeObservabilityRepo) GetRunReplay(context.Context, uuid.UUID) (RunReplay, error) {
	return RunReplay{}, ErrTraceNotFound
}

func TestObservabilityHandlerListEventsBuildsFilters(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	taskID := uuid.New()
	jobID := uuid.New()
	tests := []struct {
		name  string
		query string
		check func(t *testing.T, filter ObservabilityEventFilter)
	}{
		{
			name:  "event type and name",
			query: "org_id=org-a&product_surface=companion&event_type=guardrail&event_name=mcp_runtime_policy",
			check: func(t *testing.T, filter ObservabilityEventFilter) {
				if filter.OrgID != "org-a" || filter.ProductSurface != "companion" || filter.EventType != "guardrail" || filter.EventName != "mcp_runtime_policy" {
					t.Fatalf("unexpected filter: %+v", filter)
				}
			},
		},
		{
			name:  "severity and capability",
			query: "org_id=org-a&severity=warn&capability_id=axis.products.list",
			check: func(t *testing.T, filter ObservabilityEventFilter) {
				if filter.Severity != "warn" || filter.CapabilityID != "axis.products.list" {
					t.Fatalf("unexpected filter: %+v", filter)
				}
			},
		},
		{
			name:  "tool name alias",
			query: "org_id=org-a&tool_name=axis.products.list",
			check: func(t *testing.T, filter ObservabilityEventFilter) {
				if filter.CapabilityID != "" || filter.ToolName != "axis.products.list" {
					t.Fatalf("handler should pass tool_name alias separately before repo normalization, got %+v", filter)
				}
			},
		},
		{
			name:  "agent task and job",
			query: "org_id=org-a&agent_id=agent-a&task_id=" + taskID.String() + "&job_id=" + jobID.String(),
			check: func(t *testing.T, filter ObservabilityEventFilter) {
				if filter.AgentID != "agent-a" || filter.TaskID == nil || *filter.TaskID != taskID || filter.JobID == nil || *filter.JobID != jobID {
					t.Fatalf("unexpected filter: %+v", filter)
				}
			},
		},
		{
			name:  "run id and event type",
			query: "run_id=" + runID.String() + "&event_type=tool",
			check: func(t *testing.T, filter ObservabilityEventFilter) {
				if filter.RunID == nil || *filter.RunID != runID || filter.EventType != "tool" {
					t.Fatalf("unexpected filter: %+v", filter)
				}
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeObservabilityRepo{events: []ObservabilityEvent{{OrgID: "org-a", EventType: "test", EventName: "test"}}}
			mux := http.NewServeMux()
			NewObservabilityHandler(repo).Register(mux)
			req := httptest.NewRequest(http.MethodGet, "/v1/observability/events?"+tt.query, nil)
			req = withRuntimePrincipal(req, []string{scopeObservabilityRead})
			res := httptest.NewRecorder()

			mux.ServeHTTP(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
			}
			tt.check(t, repo.filter)
		})
	}
}

func TestObservabilityHandlerListEventsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		scopes []string
		auth   bool
		want   int
	}{
		{
			name:   "invalid run id",
			path:   "/v1/observability/events?org_id=org-a&run_id=bad",
			scopes: []string{scopeObservabilityRead},
			auth:   true,
			want:   http.StatusBadRequest,
		},
		{
			name: "missing org and run id",
			path: "/v1/observability/events",
			auth: false,
			want: http.StatusBadRequest,
		},
		{
			name:   "missing scope",
			path:   "/v1/observability/events?org_id=org-a",
			scopes: []string{"companion:tasks:read"},
			auth:   true,
			want:   http.StatusForbidden,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeObservabilityRepo{}
			mux := http.NewServeMux()
			NewObservabilityHandler(repo).Register(mux)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.auth {
				req = withRuntimePrincipal(req, tt.scopes)
			}
			res := httptest.NewRecorder()

			mux.ServeHTTP(res, req)

			if res.Code != tt.want {
				t.Fatalf("expected %d, got %d body=%s", tt.want, res.Code, res.Body.String())
			}
		})
	}
}

func TestObservabilityEventQueryBuildsFilters(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	taskID := uuid.New()
	jobID := uuid.New()
	query, args := observabilityEventQuery(ObservabilityEventFilter{
		OrgID:          " org-a ",
		ProductSurface: " companion ",
		RunID:          &runID,
		EventType:      " guardrail ",
		EventName:      " mcp_runtime_policy ",
		Severity:       " warn ",
		ToolName:       " axis.products.list ",
		AgentID:        " agent-a ",
		TaskID:         &taskID,
		JobID:          &jobID,
		Limit:          42,
	})
	for _, fragment := range []string{
		"run_id = $1",
		"org_id = $2",
		"product_surface = $3",
		"event_type = $4",
		"event_name = $5",
		"severity = $6",
		"capability_id = $7",
		"agent_id = $8",
		"task_id = $9",
		"job_id = $10",
		"ORDER BY occurred_at ASC LIMIT $11",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("expected query to contain %q, got %s", fragment, query)
		}
	}
	wantArgs := []any{runID, "org-a", "companion", "guardrail", "mcp_runtime_policy", "warn", "axis.products.list", "agent-a", taskID, jobID, 42}
	if len(args) != len(wantArgs) {
		t.Fatalf("expected %d args, got %d: %+v", len(wantArgs), len(args), args)
	}
	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Fatalf("arg %d: want %+v got %+v", i, wantArgs[i], args[i])
		}
	}
}

func TestObservabilityEventQueryDefaultsLimitAndSortsDescendingWithoutRun(t *testing.T) {
	t.Parallel()

	query, args := observabilityEventQuery(ObservabilityEventFilter{OrgID: "org-a", Limit: 999})
	if !strings.Contains(query, "ORDER BY occurred_at DESC LIMIT $2") {
		t.Fatalf("expected desc order for org query, got %s", query)
	}
	if got := args[len(args)-1]; got != 100 {
		t.Fatalf("expected normalized limit 100, got %+v", got)
	}
}
