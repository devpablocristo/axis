package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/jobs"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/securityevals"
)

type ProductCatalog interface {
	ListProducts(ctx context.Context) ([]products.Product, error)
	ListInstallations(ctx context.Context, orgID string) ([]products.Installation, error)
}

type CapabilityCatalog interface {
	ListManifests(ctx context.Context, filter capabilities.ManifestFilter) ([]capabilities.ManifestRecord, error)
	ListConformanceRuns(ctx context.Context, orgID, capabilityID string, limit int) ([]capabilities.ConformanceRun, error)
}

type EvalReports interface {
	ListReports(ctx context.Context, orgID, productSurface, suite string, limit int) ([]securityevals.Report, error)
}

type ObservabilityEvents interface {
	ListObservabilityEvents(ctx context.Context, filter runtime.ObservabilityEventFilter) ([]runtime.ObservabilityEvent, error)
}

type CostLedger interface {
	GetCostSummary(ctx context.Context, orgID, productSurface, period string, limit int) (runtime.CostSummary, error)
}

type RuntimeControls interface {
	GetRuntimePolicy(ctx context.Context, orgID string) (runtime.TenantRuntimePolicy, error)
	GetRuntimeUsage(ctx context.Context, orgID, period string) (runtime.TenantRuntimeUsage, error)
}

type JobQueue interface {
	List(ctx context.Context, orgID, productSurface, status string, limit int) ([]jobs.Job, error)
}

type Deps struct {
	Products        ProductCatalog
	Capabilities    CapabilityCatalog
	Evals           EvalReports
	Observability   ObservabilityEvents
	Costs           CostLedger
	RuntimeControls RuntimeControls
	Jobs            JobQueue
	AlertSink       AlertSink
}

type Usecases struct {
	deps Deps
}

type Query struct {
	OrgID          string
	ProductSurface string
	Period         string
	Limit          int
}

type Console struct {
	OrgID           string                        `json:"org_id"`
	ProductSurface  string                        `json:"product_surface,omitempty"`
	Period          string                        `json:"period"`
	GeneratedAt     time.Time                     `json:"generated_at"`
	Products        []products.Product            `json:"products"`
	Installations   []products.Installation       `json:"installations"`
	Capabilities    []capabilities.ManifestRecord `json:"capabilities"`
	ConformanceRuns []capabilities.ConformanceRun `json:"conformance_runs"`
	EvalReports     []securityevals.Report        `json:"eval_reports"`
	CostSummary     *runtime.CostSummary          `json:"cost_summary,omitempty"`
	RuntimePolicy   *runtime.TenantRuntimePolicy  `json:"runtime_policy,omitempty"`
	RuntimeUsage    *runtime.TenantRuntimeUsage   `json:"runtime_usage,omitempty"`
	RuntimeLimits   []ProductRuntimeLimit         `json:"runtime_limits,omitempty"`
	Metrics         []ProductOperationalMetrics   `json:"metrics,omitempty"`
	JobHealth       []ProductJobHealth            `json:"job_health,omitempty"`
	Events          []runtime.ObservabilityEvent  `json:"events"`
	Alerts          []Alert                       `json:"alerts"`
	SLOs            []ProductSLO                  `json:"slos"`
}

type Alert struct {
	ID             string         `json:"id"`
	Severity       string         `json:"severity"`
	Type           string         `json:"type"`
	ProductSurface string         `json:"product_surface,omitempty"`
	Message        string         `json:"message"`
	Source         string         `json:"source"`
	Evidence       map[string]any `json:"evidence,omitempty"`
	OccurredAt     time.Time      `json:"occurred_at,omitempty"`
}

type ProductRuntimeLimit struct {
	ProductSurface     string  `json:"product_surface"`
	Period             string  `json:"period"`
	CostUsedCents      int64   `json:"cost_used_cents"`
	CostLimitCents     int64   `json:"cost_limit_cents,omitempty"`
	CostRemainingCents int64   `json:"cost_remaining_cents,omitempty"`
	CostUsageRatio     float64 `json:"cost_usage_ratio,omitempty"`
	CostLimitSource    string  `json:"cost_limit_source,omitempty"`
	ToolCallsUsed      int64   `json:"tool_calls_used"`
	ToolCallLimit      int64   `json:"tool_call_limit,omitempty"`
	ToolCallsRemaining int64   `json:"tool_calls_remaining,omitempty"`
	ToolCallUsageRatio float64 `json:"tool_call_usage_ratio,omitempty"`
	ToolCallSource     string  `json:"tool_call_source,omitempty"`
	Status             string  `json:"status"`
}

type ProductOperationalMetrics struct {
	ProductSurface      string    `json:"product_surface"`
	Period              string    `json:"period"`
	EventCount          int       `json:"event_count"`
	RuntimeToolCalls    int       `json:"runtime_tool_calls"`
	MCPToolCalls        int       `json:"mcp_tool_calls"`
	ToolErrors          int       `json:"tool_errors"`
	GuardrailCount      int       `json:"guardrail_count"`
	AlertCount          int       `json:"alert_count"`
	AvgLatencyMS        float64   `json:"avg_latency_ms,omitempty"`
	P95LatencyMS        float64   `json:"p95_latency_ms,omitempty"`
	LatencySamples      int       `json:"latency_samples"`
	CostUsedCents       int64     `json:"cost_used_cents"`
	CostBudgetCents     int64     `json:"cost_budget_cents,omitempty"`
	EvalScore           float64   `json:"eval_score,omitempty"`
	EvalThreshold       float64   `json:"eval_threshold,omitempty"`
	LatestEvalSuite     string    `json:"latest_eval_suite,omitempty"`
	LastObservedAt      time.Time `json:"last_observed_at,omitempty"`
	LastMetricUpdatedAt time.Time `json:"last_metric_updated_at,omitempty"`
}

type ProductJobHealth struct {
	ProductSurface    string         `json:"product_surface"`
	Queued            int            `json:"queued"`
	Running           int            `json:"running"`
	Failed            int            `json:"failed"`
	DeadLetter        int            `json:"dead_letter"`
	RetryScheduled    int            `json:"retry_scheduled"`
	ExpiredLeases     int            `json:"expired_leases"`
	StuckJobs         int            `json:"stuck_jobs"`
	SampleSize        int            `json:"sample_size"`
	RecentDeadLetters []JobReference `json:"recent_dead_letters,omitempty"`
	ExpiredLeaseJobs  []JobReference `json:"expired_lease_jobs,omitempty"`
	StuckJobRefs      []JobReference `json:"stuck_jobs_refs,omitempty"`
}

type JobReference struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	MaxAttempts     int        `json:"max_attempts"`
	LastError       string     `json:"last_error,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	DeadlineAt      *time.Time `json:"deadline_at,omitempty"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at,omitempty"`
}

type ProductSLO struct {
	ProductSurface  string       `json:"product_surface"`
	Latency         SLOIndicator `json:"latency"`
	Availability    SLOIndicator `json:"availability"`
	ToolSuccessRate SLOIndicator `json:"tool_success_rate"`
	EvalScore       SLOIndicator `json:"eval_score"`
	CostCeiling     SLOIndicator `json:"cost_ceiling"`
	Status          string       `json:"status"`
}

type SLOIndicator struct {
	Status    string  `json:"status"`
	Value     float64 `json:"value,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	Reason    string  `json:"reason,omitempty"`
}

func NewUsecases(deps Deps) *Usecases {
	return &Usecases{deps: deps}
}

func (u *Usecases) GetConsole(ctx context.Context, q Query) (Console, error) {
	q = normalizeQuery(q)
	if q.OrgID == "" {
		return Console{}, fmt.Errorf("org_id is required")
	}
	console := Console{
		OrgID:          q.OrgID,
		ProductSurface: q.ProductSurface,
		Period:         q.Period,
		GeneratedAt:    time.Now().UTC(),
	}
	var err error
	if u.deps.Products != nil {
		console.Products, err = u.deps.Products.ListProducts(ctx)
		if err != nil {
			return Console{}, fmt.Errorf("list products: %w", err)
		}
		console.Products = filterProducts(console.Products, q.ProductSurface)
		console.Installations, err = u.deps.Products.ListInstallations(ctx, q.OrgID)
		if err != nil {
			return Console{}, fmt.Errorf("list installations: %w", err)
		}
		console.Installations = filterInstallations(console.Installations, q.ProductSurface)
	}
	if u.deps.Capabilities != nil {
		console.Capabilities, err = u.deps.Capabilities.ListManifests(ctx, capabilities.ManifestFilter{Limit: q.Limit})
		if err != nil {
			return Console{}, fmt.Errorf("list capabilities: %w", err)
		}
		console.Capabilities = filterCapabilities(console.Capabilities, q.ProductSurface)
		console.ConformanceRuns, err = u.deps.Capabilities.ListConformanceRuns(ctx, q.OrgID, "", q.Limit)
		if err != nil {
			return Console{}, fmt.Errorf("list conformance runs: %w", err)
		}
		console.ConformanceRuns = filterConformanceRuns(console.ConformanceRuns, console.Capabilities, q.ProductSurface)
	}
	if u.deps.Evals != nil {
		console.EvalReports, err = u.deps.Evals.ListReports(ctx, q.OrgID, q.ProductSurface, "", q.Limit)
		if err != nil {
			return Console{}, fmt.Errorf("list eval reports: %w", err)
		}
	}
	if u.deps.Observability != nil {
		console.Events, err = u.deps.Observability.ListObservabilityEvents(ctx, runtime.ObservabilityEventFilter{
			OrgID:          q.OrgID,
			ProductSurface: q.ProductSurface,
			Limit:          q.Limit,
		})
		if err != nil {
			return Console{}, fmt.Errorf("list observability events: %w", err)
		}
	}
	if u.deps.Costs != nil {
		summary, err := u.deps.Costs.GetCostSummary(ctx, q.OrgID, q.ProductSurface, q.Period, q.Limit)
		if err != nil {
			return Console{}, fmt.Errorf("get cost summary: %w", err)
		}
		console.CostSummary = &summary
	}
	if u.deps.RuntimeControls != nil {
		policy, err := u.deps.RuntimeControls.GetRuntimePolicy(ctx, q.OrgID)
		if err != nil && !errors.Is(err, runtime.ErrRuntimePolicyNotFound) {
			return Console{}, fmt.Errorf("get runtime policy: %w", err)
		}
		if err == nil {
			console.RuntimePolicy = &policy
		}
		usage, err := u.deps.RuntimeControls.GetRuntimeUsage(ctx, q.OrgID, q.Period)
		if err != nil && !errors.Is(err, runtime.ErrRuntimePolicyNotFound) {
			return Console{}, fmt.Errorf("get runtime usage: %w", err)
		}
		if err == nil {
			console.RuntimeUsage = &usage
		}
	}
	if u.deps.Jobs != nil {
		console.JobHealth, err = u.buildJobHealth(ctx, q)
		if err != nil {
			return Console{}, fmt.Errorf("build job health: %w", err)
		}
	}
	console.RuntimeLimits = buildRuntimeLimits(console)
	console.Metrics = buildOperationalMetrics(console)
	console.Alerts = buildAlerts(console)
	console.Metrics = attachAlertCounts(console.Metrics, console.Alerts)
	console.SLOs = buildSLOs(console)
	return console, nil
}

func (u *Usecases) ListAlerts(ctx context.Context, q Query) ([]Alert, error) {
	console, err := u.GetConsole(ctx, q)
	if err != nil {
		return nil, err
	}
	return console.Alerts, nil
}

func (u *Usecases) ListSLOs(ctx context.Context, q Query) ([]ProductSLO, error) {
	console, err := u.GetConsole(ctx, q)
	if err != nil {
		return nil, err
	}
	return console.SLOs, nil
}

func (u *Usecases) ListMetrics(ctx context.Context, q Query) ([]ProductOperationalMetrics, error) {
	console, err := u.GetConsole(ctx, q)
	if err != nil {
		return nil, err
	}
	return console.Metrics, nil
}

func (u *Usecases) buildJobHealth(ctx context.Context, q Query) ([]ProductJobHealth, error) {
	if u.deps.Jobs == nil {
		return nil, nil
	}
	samples := make([]jobs.Job, 0)
	for _, status := range []jobs.Status{jobs.StatusQueued, jobs.StatusRunning, jobs.StatusFailed, jobs.StatusDeadLetter} {
		items, err := u.deps.Jobs.List(ctx, q.OrgID, q.ProductSurface, string(status), q.Limit)
		if err != nil {
			return nil, err
		}
		samples = append(samples, items...)
	}
	return buildJobHealth(samples, q.ProductSurface, time.Now().UTC()), nil
}

func buildJobHealth(samples []jobs.Job, productSurface string, now time.Time) []ProductJobHealth {
	bySurface := map[string]*ProductJobHealth{}
	for _, job := range samples {
		surface := strings.TrimSpace(job.ProductSurface)
		if surface == "" {
			surface = runtime.DefaultProductSurface
		}
		if productSurface != "" && surface != productSurface {
			continue
		}
		health := bySurface[surface]
		if health == nil {
			health = &ProductJobHealth{ProductSurface: surface}
			bySurface[surface] = health
		}
		health.SampleSize++
		switch job.Status {
		case jobs.StatusQueued:
			health.Queued++
			if job.Attempts > 0 {
				health.RetryScheduled++
			}
		case jobs.StatusRunning:
			health.Running++
			if isExpiredLease(job, now) {
				health.ExpiredLeases++
				health.ExpiredLeaseJobs = appendJobRef(health.ExpiredLeaseJobs, job)
			}
			if isStuckJob(job, now) {
				health.StuckJobs++
				health.StuckJobRefs = appendJobRef(health.StuckJobRefs, job)
			}
		case jobs.StatusFailed:
			health.Failed++
		case jobs.StatusDeadLetter:
			health.DeadLetter++
			health.RecentDeadLetters = appendJobRef(health.RecentDeadLetters, job)
		}
	}
	out := make([]ProductJobHealth, 0, len(bySurface))
	for _, health := range bySurface {
		out = append(out, *health)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ProductSurface < out[j].ProductSurface
	})
	return out
}

func isExpiredLease(job jobs.Job, now time.Time) bool {
	return job.Status == jobs.StatusRunning && job.LeaseUntil != nil && job.LeaseUntil.Before(now)
}

func isStuckJob(job jobs.Job, now time.Time) bool {
	if job.Status != jobs.StatusRunning {
		return false
	}
	if job.DeadlineAt != nil && job.DeadlineAt.Before(now) {
		return true
	}
	if job.HeartbeatAt == nil || job.TimeoutSeconds <= 0 {
		return false
	}
	return job.HeartbeatAt.Add(time.Duration(job.TimeoutSeconds) * time.Second).Before(now)
}

func appendJobRef(values []JobReference, job jobs.Job) []JobReference {
	const maxRefs = 5
	if len(values) >= maxRefs {
		return values
	}
	ref := JobReference{
		ID:          job.ID.String(),
		Kind:        job.Kind,
		Status:      string(job.Status),
		Attempts:    job.Attempts,
		MaxAttempts: job.MaxAttempts,
		LastError:   job.LastError,
		UpdatedAt:   job.UpdatedAt,
	}
	if job.LeaseUntil != nil {
		ref.LeaseUntil = job.LeaseUntil
	}
	if job.DeadlineAt != nil {
		ref.DeadlineAt = job.DeadlineAt
	}
	if job.HeartbeatAt != nil {
		ref.LastHeartbeatAt = job.HeartbeatAt
	}
	return append(values, ref)
}

func buildAlerts(console Console) []Alert {
	alerts := make([]Alert, 0)
	for _, product := range console.Products {
		if product.Status == products.ProductStatusDisabled {
			alerts = append(alerts, alert("product_disabled", "critical", product.ProductSurface, "Product is disabled", "products", map[string]any{"status": product.Status}, product.UpdatedAt))
		}
	}
	for _, installation := range console.Installations {
		if !installation.Enabled {
			alerts = append(alerts, alert("installation_disabled", "critical", installation.ProductSurface, "Product installation is disabled", "products", map[string]any{"org_id": installation.OrgID}, installation.UpdatedAt))
		}
	}
	for _, run := range latestConformanceByCapability(console.ConformanceRuns) {
		if run.Status == capabilities.ConformanceStatusFailed {
			productSurface := productForCapability(console.Capabilities, run.CapabilityID)
			alerts = append(alerts, alert("capability_conformance_failed", "critical", productSurface, "Capability conformance failed", "capabilities", map[string]any{"capability_id": run.CapabilityID, "version": run.Version, "errors": run.Errors}, run.CreatedAt))
		}
	}
	for _, report := range latestEvalByProductSuite(console.EvalReports) {
		if report.Status == "failed" || (report.Threshold > 0 && report.Score < report.Threshold) {
			alerts = append(alerts, alert("eval_regression", "critical", report.ProductSurface, "Product eval regression detected", "security_evals", map[string]any{"suite": report.Suite, "score": report.Score, "threshold": report.Threshold}, report.CreatedAt))
		}
	}
	for _, event := range console.Events {
		if event.EventType == "guardrail" && strings.Contains(event.EventName, "rate_limit") {
			alerts = append(alerts, alert("rate_limit_abuse", "warning", event.ProductSurface, "Product rate limit guardrail triggered", "observability", guardrailEventEvidence(event), event.OccurredAt))
		}
		if event.EventType == "guardrail" && strings.Contains(event.EventName, "budget") {
			alerts = append(alerts, alert("runtime_budget_block", "critical", event.ProductSurface, "Runtime budget guardrail triggered", "observability", guardrailEventEvidence(event), event.OccurredAt))
		}
		if event.EventType == "guardrail" && event.EventName == "mcp_runtime_policy" {
			alerts = append(alerts, alert("mcp_runtime_policy_block", "warning", event.ProductSurface, "MCP tool blocked by runtime policy", "observability", mcpRuntimePolicyEvidence(event), event.OccurredAt))
		}
		if event.EventType == "guardrail" && event.EventName == "mcp_scope_required" {
			alerts = append(alerts, alert("mcp_scope_block", "warning", event.ProductSurface, "MCP tool blocked by missing scope", "observability", guardrailEventEvidence(event), event.OccurredAt))
		}
		if isLeakageEvent(event) {
			alerts = append(alerts, alert("tenant_product_leakage", "critical", event.ProductSurface, "Tenant/product leakage signal detected", "observability", map[string]any{"event_name": event.EventName}, event.OccurredAt))
		}
	}
	if console.CostSummary != nil && console.RuntimePolicy != nil {
		alerts = append(alerts, costAlerts(*console.CostSummary, *console.RuntimePolicy)...)
	}
	if console.RuntimeUsage != nil && console.RuntimeUsage.ToolCalls > 0 {
		rate := float64(console.RuntimeUsage.ToolErrors) / float64(console.RuntimeUsage.ToolCalls)
		if rate >= 0.25 {
			alerts = append(alerts, alert("high_error_rate", "critical", console.ProductSurface, "Runtime tool error rate is high", "runtime_usage", map[string]any{"tool_error_rate": rate}, console.RuntimeUsage.UpdatedAt))
		} else if rate >= 0.10 {
			alerts = append(alerts, alert("high_error_rate", "warning", console.ProductSurface, "Runtime tool error rate is elevated", "runtime_usage", map[string]any{"tool_error_rate": rate}, console.RuntimeUsage.UpdatedAt))
		}
	}
	for _, health := range console.JobHealth {
		if health.DeadLetter > 0 {
			alerts = append(alerts, alert("job_dead_letter", "critical", health.ProductSurface, "Durable jobs reached dead letter", "jobs", map[string]any{"dead_letter": health.DeadLetter, "recent": health.RecentDeadLetters}, time.Now().UTC()))
		}
		if health.StuckJobs > 0 {
			alerts = append(alerts, alert("job_stuck", "critical", health.ProductSurface, "Durable jobs appear stuck", "jobs", map[string]any{"stuck_jobs": health.StuckJobs, "recent": health.StuckJobRefs}, time.Now().UTC()))
		}
		if health.ExpiredLeases > 0 {
			alerts = append(alerts, alert("job_expired_lease", "warning", health.ProductSurface, "Durable jobs have expired leases", "jobs", map[string]any{"expired_leases": health.ExpiredLeases, "recent": health.ExpiredLeaseJobs}, time.Now().UTC()))
		}
	}
	alerts = dedupeAlerts(alerts)
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Severity == alerts[j].Severity {
			return alerts[i].OccurredAt.After(alerts[j].OccurredAt)
		}
		return severityRank(alerts[i].Severity) > severityRank(alerts[j].Severity)
	})
	return alerts
}

func guardrailEventEvidence(event runtime.ObservabilityEvent) map[string]any {
	evidence := map[string]any{"event_name": event.EventName}
	if len(event.Payload) == 0 {
		return evidence
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return evidence
	}
	for _, key := range []string{"tool_name", "target", "reason", "org_id", "product_surface", "missing_scopes"} {
		if value, ok := payload[key]; ok {
			evidence[key] = value
		}
	}
	return evidence
}

func mcpRuntimePolicyEvidence(event runtime.ObservabilityEvent) map[string]any {
	evidence := map[string]any{"event_name": event.EventName}
	if len(event.Payload) == 0 {
		return evidence
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return evidence
	}
	for _, key := range []string{"tool_name", "target", "reason"} {
		if value, ok := payload[key]; ok {
			evidence[key] = value
		}
	}
	return evidence
}

const alertDedupeWindow = 15 * time.Minute

func dedupeAlerts(values []Alert) []Alert {
	out := make([]Alert, 0, len(values))
	for _, item := range values {
		idx := duplicateAlertIndex(out, item)
		if idx < 0 {
			out = append(out, item)
			continue
		}
		out[idx] = mergeDuplicateAlert(out[idx], item)
	}
	return out
}

func duplicateAlertIndex(values []Alert, candidate Alert) int {
	key := alertDedupeKey(candidate)
	for i, item := range values {
		if alertDedupeKey(item) != key {
			continue
		}
		if withinAlertDedupeWindow(item.OccurredAt, candidate.OccurredAt) {
			return i
		}
	}
	return -1
}

func alertDedupeKey(item Alert) string {
	parts := []string{item.Type, item.Severity, item.ProductSurface, item.Source}
	for _, key := range []string{"event_name", "tool_name", "target", "reason", "capability_id", "suite"} {
		if item.Evidence == nil {
			continue
		}
		if value, ok := item.Evidence[key]; ok {
			parts = append(parts, key+"="+fmt.Sprint(value))
		}
	}
	if len(parts) == 4 {
		parts = append(parts, "message="+item.Message)
	}
	return strings.Join(parts, "|")
}

func withinAlertDedupeWindow(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return true
	}
	diff := a.Sub(b)
	if diff < 0 {
		diff = -diff
	}
	return diff <= alertDedupeWindow
}

func mergeDuplicateAlert(current, duplicate Alert) Alert {
	primary := current
	if duplicate.OccurredAt.After(current.OccurredAt) {
		primary = duplicate
	}
	evidence := copyEvidence(primary.Evidence)
	suppressed := evidenceInt(current.Evidence, "suppressed_count") + evidenceInt(duplicate.Evidence, "suppressed_count") + 1
	evidence["suppressed_count"] = suppressed
	first := minAlertTime(current.OccurredAt, duplicate.OccurredAt)
	last := maxAlertTime(current.OccurredAt, duplicate.OccurredAt)
	if !first.IsZero() {
		evidence["first_occurred_at"] = first.Format(time.RFC3339)
	}
	if !last.IsZero() {
		evidence["last_occurred_at"] = last.Format(time.RFC3339)
	}
	primary.Evidence = evidence
	return primary
}

func copyEvidence(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(values)+3)
	for key, value := range values {
		out[key] = value
	}
	return out
}

func evidenceInt(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func minAlertTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}

func maxAlertTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.After(b) {
		return a
	}
	return b
}

func buildRuntimeLimits(console Console) []ProductRuntimeLimit {
	if console.RuntimePolicy == nil && console.CostSummary == nil {
		return nil
	}
	surfaces := runtimeLimitSurfaces(console)
	out := make([]ProductRuntimeLimit, 0, len(surfaces))
	for _, surface := range surfaces {
		usage := productCostUsage(console.CostSummary, surface)
		item := ProductRuntimeLimit{
			ProductSurface: surface,
			Period:         console.Period,
			CostUsedCents:  usage.EstimatedCostCents,
			ToolCallsUsed:  usage.ToolCalls,
			Status:         "unknown",
		}
		if console.RuntimePolicy != nil {
			applyRuntimeLimitPolicy(&item, *console.RuntimePolicy, surface)
		}
		item.Status = runtimeLimitStatus(item)
		out = append(out, item)
	}
	return out
}

func buildOperationalMetrics(console Console) []ProductOperationalMetrics {
	surfaces := productSurfaces(console)
	bySurface := make(map[string]*ProductOperationalMetrics, len(surfaces))
	latencySamples := make(map[string][]float64, len(surfaces))
	for _, surface := range surfaces {
		bySurface[surface] = &ProductOperationalMetrics{
			ProductSurface: surface,
			Period:         console.Period,
		}
	}
	for _, event := range console.Events {
		surface := strings.TrimSpace(event.ProductSurface)
		if surface == "" {
			surface = runtime.DefaultProductSurface
		}
		if console.ProductSurface != "" && surface != console.ProductSurface {
			continue
		}
		metric := bySurface[surface]
		if metric == nil {
			metric = &ProductOperationalMetrics{ProductSurface: surface, Period: console.Period}
			bySurface[surface] = metric
		}
		metric.EventCount++
		if event.OccurredAt.After(metric.LastObservedAt) {
			metric.LastObservedAt = event.OccurredAt
		}
		if event.EventType == "guardrail" {
			metric.GuardrailCount++
		}
		if event.EventType == "mcp" && event.EventName == "mcp_tool_call" {
			metric.MCPToolCalls++
			if isErrorEvent(event) {
				metric.ToolErrors++
			}
		}
		if event.EventType == "tool" && event.EventName == "executed" {
			metric.RuntimeToolCalls++
			if isErrorEvent(event) {
				metric.ToolErrors++
			}
		}
		if durationMS := eventDurationMS(event); durationMS > 0 {
			latencySamples[surface] = append(latencySamples[surface], durationMS)
			addLatency(metric, durationMS)
		}
	}
	applyCostMetrics(bySurface, console)
	applyEvalMetrics(bySurface, console)
	out := make([]ProductOperationalMetrics, 0, len(bySurface))
	for _, metric := range bySurface {
		finalizeLatency(metric, latencySamples[metric.ProductSurface])
		metric.LastMetricUpdatedAt = console.GeneratedAt
		out = append(out, *metric)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ProductSurface < out[j].ProductSurface
	})
	return out
}

func attachAlertCounts(metrics []ProductOperationalMetrics, alerts []Alert) []ProductOperationalMetrics {
	for i := range metrics {
		for _, item := range alerts {
			if item.ProductSurface == metrics[i].ProductSurface {
				metrics[i].AlertCount++
			}
		}
	}
	return metrics
}

func isErrorEvent(event runtime.ObservabilityEvent) bool {
	if event.Severity == "warn" || event.Severity == "error" {
		return true
	}
	if len(event.Payload) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false
	}
	if status, ok := payload["status"].(string); ok {
		status = strings.TrimSpace(strings.ToLower(status))
		return status == "blocked" || status == "denied" || status == "failed" || status == "error"
	}
	if _, ok := payload["error"]; ok {
		return true
	}
	return false
}

func eventDurationMS(event runtime.ObservabilityEvent) float64 {
	if len(event.Payload) == 0 {
		return 0
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return 0
	}
	return numericPayloadValue(payload["duration_ms"])
}

func numericPayloadValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func addLatency(metric *ProductOperationalMetrics, value float64) {
	if metric == nil || value <= 0 {
		return
	}
	if metric.LatencySamples == 0 {
		metric.AvgLatencyMS = value
	} else {
		metric.AvgLatencyMS = ((metric.AvgLatencyMS * float64(metric.LatencySamples)) + value) / float64(metric.LatencySamples+1)
	}
	metric.LatencySamples++
}

func finalizeLatency(metric *ProductOperationalMetrics, samples []float64) {
	if metric == nil || metric.LatencySamples == 0 {
		return
	}
	metric.AvgLatencyMS = math.Round(metric.AvgLatencyMS*100) / 100
	if len(samples) > 0 {
		sort.Float64s(samples)
		idx := int(math.Ceil(float64(len(samples))*0.95)) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		metric.P95LatencyMS = samples[idx]
	}
	metric.P95LatencyMS = math.Round(metric.P95LatencyMS*100) / 100
}

func applyCostMetrics(metrics map[string]*ProductOperationalMetrics, console Console) {
	if console.CostSummary == nil {
		return
	}
	apply := func(surface string, used int64) {
		if surface == "" {
			surface = runtime.DefaultProductSurface
		}
		if console.ProductSurface != "" && surface != console.ProductSurface {
			return
		}
		metric := metrics[surface]
		if metric == nil {
			metric = &ProductOperationalMetrics{ProductSurface: surface, Period: console.Period}
			metrics[surface] = metric
		}
		metric.CostUsedCents = used
		if console.RuntimePolicy != nil {
			metric.CostBudgetCents = costBudgetForProduct(*console.RuntimePolicy, surface)
		}
	}
	if console.CostSummary.ProductSurface != "" {
		apply(console.CostSummary.ProductSurface, console.CostSummary.EstimatedCostCents)
		return
	}
	for _, item := range console.CostSummary.ByProduct {
		apply(item.Key, item.EstimatedCostCents)
	}
}

func applyEvalMetrics(metrics map[string]*ProductOperationalMetrics, console Console) {
	latest := latestEvalByProductSuite(console.EvalReports)
	latestBySurface := map[string]securityevals.Report{}
	for _, report := range latest {
		current, ok := latestBySurface[report.ProductSurface]
		if !ok || report.CreatedAt.After(current.CreatedAt) {
			latestBySurface[report.ProductSurface] = report
		}
	}
	for surface, report := range latestBySurface {
		if console.ProductSurface != "" && surface != console.ProductSurface {
			continue
		}
		metric := metrics[surface]
		if metric == nil {
			metric = &ProductOperationalMetrics{ProductSurface: surface, Period: console.Period}
			metrics[surface] = metric
		}
		metric.EvalScore = report.Score
		metric.EvalThreshold = report.Threshold
		metric.LatestEvalSuite = report.Suite
	}
}

func costBudgetForProduct(policy runtime.TenantRuntimePolicy, productSurface string) int64 {
	budget := policy.ControlPlane.MonthlyCostBudgetCents
	if productPolicy, ok := policy.ControlPlane.ProductPolicies[productSurface]; ok && productPolicy.MonthlyCostBudgetCents > 0 {
		budget = productPolicy.MonthlyCostBudgetCents
	}
	return budget
}

func runtimeLimitSurfaces(console Console) []string {
	seen := map[string]struct{}{}
	for _, surface := range productSurfaces(console) {
		if surface != "" {
			seen[surface] = struct{}{}
		}
	}
	if console.CostSummary != nil {
		if console.CostSummary.ProductSurface != "" {
			seen[console.CostSummary.ProductSurface] = struct{}{}
		}
		for _, item := range console.CostSummary.ByProduct {
			if item.Key != "" {
				seen[item.Key] = struct{}{}
			}
		}
	}
	if console.RuntimePolicy != nil {
		for surface := range console.RuntimePolicy.ControlPlane.ProductPolicies {
			if surface != "" {
				seen[surface] = struct{}{}
			}
		}
	}
	if console.ProductSurface != "" {
		for surface := range seen {
			if surface != console.ProductSurface {
				delete(seen, surface)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for surface := range seen {
		out = append(out, surface)
	}
	sort.Strings(out)
	return out
}

func productCostUsage(summary *runtime.CostSummary, productSurface string) runtime.CostBreakdown {
	if summary == nil {
		return runtime.CostBreakdown{Dimension: "product", Key: productSurface}
	}
	if summary.ProductSurface == productSurface {
		return runtime.CostBreakdown{
			Dimension:          "product",
			Key:                productSurface,
			EstimatedTokens:    summary.EstimatedTokens,
			EstimatedCostCents: summary.EstimatedCostCents,
			LLMCalls:           summary.LLMCalls,
			ToolCalls:          summary.ToolCalls,
			JobEvents:          summary.JobEvents,
			EmbeddingEvents:    summary.EmbeddingEvents,
		}
	}
	for _, item := range summary.ByProduct {
		if item.Key == productSurface {
			return item
		}
	}
	return runtime.CostBreakdown{Dimension: "product", Key: productSurface}
}

func applyRuntimeLimitPolicy(item *ProductRuntimeLimit, policy runtime.TenantRuntimePolicy, productSurface string) {
	if policy.ControlPlane.MonthlyCostBudgetCents > 0 {
		item.CostLimitCents = policy.ControlPlane.MonthlyCostBudgetCents
		item.CostLimitSource = "org_control_plane"
	}
	if policy.MonthlyToolCallBudget > 0 {
		item.ToolCallLimit = policy.MonthlyToolCallBudget
		item.ToolCallSource = "org_runtime_policy"
	}
	if productPolicy, ok := policy.ControlPlane.ProductPolicies[productSurface]; ok {
		if productPolicy.MonthlyCostBudgetCents > 0 {
			item.CostLimitCents = productPolicy.MonthlyCostBudgetCents
			item.CostLimitSource = "product_policy"
		}
		if productPolicy.MonthlyToolCallBudget > 0 {
			item.ToolCallLimit = productPolicy.MonthlyToolCallBudget
			item.ToolCallSource = "product_policy"
		}
	}
	if item.CostLimitCents > 0 {
		item.CostRemainingCents = item.CostLimitCents - item.CostUsedCents
		item.CostUsageRatio = roundedRatio(item.CostUsedCents, item.CostLimitCents)
	}
	if item.ToolCallLimit > 0 {
		item.ToolCallsRemaining = item.ToolCallLimit - item.ToolCallsUsed
		item.ToolCallUsageRatio = roundedRatio(item.ToolCallsUsed, item.ToolCallLimit)
	}
}

func runtimeLimitStatus(item ProductRuntimeLimit) string {
	status := "unknown"
	for _, ratio := range []float64{ratioIfLimited(item.CostUsageRatio, item.CostLimitCents), ratioIfLimited(item.ToolCallUsageRatio, item.ToolCallLimit)} {
		if ratio < 0 {
			continue
		}
		if ratio >= 1 {
			return "critical"
		}
		if ratio >= 0.80 {
			status = "warning"
		} else if status == "unknown" {
			status = "ok"
		}
	}
	return status
}

func ratioIfLimited(ratio float64, limit int64) float64 {
	if limit <= 0 {
		return -1
	}
	return ratio
}

func roundedRatio(used, limit int64) float64 {
	if limit <= 0 {
		return 0
	}
	return math.Round((float64(used)/float64(limit))*10000) / 10000
}

func buildSLOs(console Console) []ProductSLO {
	surfaces := productSurfaces(console)
	out := make([]ProductSLO, 0, len(surfaces))
	for _, surface := range surfaces {
		latency := latencySLO(console, surface)
		availability := availabilitySLO(console, surface)
		toolSuccess := toolSuccessSLO(console, surface)
		evalScore := evalScoreSLO(console, surface)
		costCeiling := costCeilingSLO(console, surface)
		slo := ProductSLO{
			ProductSurface:  surface,
			Latency:         latency,
			Availability:    availability,
			ToolSuccessRate: toolSuccess,
			EvalScore:       evalScore,
			CostCeiling:     costCeiling,
		}
		slo.Status = worstSLOStatus(latency, availability, toolSuccess, evalScore, costCeiling)
		out = append(out, slo)
	}
	return out
}

func normalizeQuery(q Query) Query {
	q.OrgID = strings.TrimSpace(q.OrgID)
	q.ProductSurface = strings.TrimSpace(strings.ToLower(q.ProductSurface))
	q.Period = strings.TrimSpace(q.Period)
	if q.Period == "" {
		q.Period = time.Now().UTC().Format("2006-01")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	return q
}

func filterProducts(values []products.Product, productSurface string) []products.Product {
	if productSurface == "" {
		return values
	}
	out := make([]products.Product, 0, len(values))
	for _, item := range values {
		if item.ProductSurface == productSurface {
			out = append(out, item)
		}
	}
	return out
}

func filterInstallations(values []products.Installation, productSurface string) []products.Installation {
	if productSurface == "" {
		return values
	}
	out := make([]products.Installation, 0, len(values))
	for _, item := range values {
		if item.ProductSurface == productSurface {
			out = append(out, item)
		}
	}
	return out
}

func filterCapabilities(values []capabilities.ManifestRecord, productSurface string) []capabilities.ManifestRecord {
	if productSurface == "" {
		return values
	}
	out := make([]capabilities.ManifestRecord, 0, len(values))
	for _, item := range values {
		if item.Manifest.ProductSurface == productSurface {
			out = append(out, item)
		}
	}
	return out
}

func filterConformanceRuns(values []capabilities.ConformanceRun, caps []capabilities.ManifestRecord, productSurface string) []capabilities.ConformanceRun {
	if productSurface == "" {
		return values
	}
	out := make([]capabilities.ConformanceRun, 0, len(values))
	for _, item := range values {
		if productForCapability(caps, item.CapabilityID) == productSurface {
			out = append(out, item)
		}
	}
	return out
}

func latestConformanceByCapability(values []capabilities.ConformanceRun) []capabilities.ConformanceRun {
	latest := map[string]capabilities.ConformanceRun{}
	for _, run := range values {
		key := run.CapabilityID + "@" + run.Version
		current, ok := latest[key]
		if !ok || run.CreatedAt.After(current.CreatedAt) {
			latest[key] = run
		}
	}
	out := make([]capabilities.ConformanceRun, 0, len(latest))
	for _, run := range latest {
		out = append(out, run)
	}
	return out
}

func latestEvalByProductSuite(values []securityevals.Report) []securityevals.Report {
	latest := map[string]securityevals.Report{}
	for _, report := range values {
		key := report.ProductSurface + "|" + report.Suite
		current, ok := latest[key]
		if !ok || report.CreatedAt.After(current.CreatedAt) {
			latest[key] = report
		}
	}
	out := make([]securityevals.Report, 0, len(latest))
	for _, report := range latest {
		out = append(out, report)
	}
	return out
}

func productForCapability(caps []capabilities.ManifestRecord, capabilityID string) string {
	for _, record := range caps {
		if record.Manifest.CapabilityID == capabilityID {
			return record.Manifest.ProductSurface
		}
	}
	return ""
}

func isLeakageEvent(event runtime.ObservabilityEvent) bool {
	text := strings.ToLower(event.EventName)
	if strings.Contains(text, "leakage") || strings.Contains(text, "cross_org") || strings.Contains(text, "cross_product") {
		return true
	}
	if len(event.Payload) == 0 {
		return false
	}
	var payload any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false
	}
	raw, _ := json.Marshal(payload)
	text = strings.ToLower(string(raw))
	return strings.Contains(text, "leakage") || strings.Contains(text, "cross_org") || strings.Contains(text, "cross_product")
}

func costAlerts(summary runtime.CostSummary, policy runtime.TenantRuntimePolicy) []Alert {
	budget := policy.ControlPlane.MonthlyCostBudgetCents
	if summary.ProductSurface != "" {
		if productPolicy, ok := policy.ControlPlane.ProductPolicies[summary.ProductSurface]; ok && productPolicy.MonthlyCostBudgetCents > 0 {
			budget = productPolicy.MonthlyCostBudgetCents
		}
	}
	if budget <= 0 {
		return nil
	}
	ratio := float64(summary.EstimatedCostCents) / float64(budget)
	if ratio >= 1 {
		return []Alert{alert("cost_anomaly", "critical", summary.ProductSurface, "Cost budget exhausted", "costs", map[string]any{"estimated_cost_cents": summary.EstimatedCostCents, "budget_cents": budget}, time.Now().UTC())}
	}
	if ratio >= 0.80 {
		return []Alert{alert("cost_anomaly", "warning", summary.ProductSurface, "Cost budget is near the ceiling", "costs", map[string]any{"estimated_cost_cents": summary.EstimatedCostCents, "budget_cents": budget}, time.Now().UTC())}
	}
	return nil
}

func productSurfaces(console Console) []string {
	seen := map[string]struct{}{}
	for _, product := range console.Products {
		if product.ProductSurface != "" {
			seen[product.ProductSurface] = struct{}{}
		}
	}
	for _, installation := range console.Installations {
		if installation.ProductSurface != "" {
			seen[installation.ProductSurface] = struct{}{}
		}
	}
	for _, record := range console.Capabilities {
		if record.Manifest.ProductSurface != "" {
			seen[record.Manifest.ProductSurface] = struct{}{}
		}
	}
	for _, report := range console.EvalReports {
		if report.ProductSurface != "" {
			seen[report.ProductSurface] = struct{}{}
		}
	}
	for _, metric := range console.Metrics {
		if metric.ProductSurface != "" {
			seen[metric.ProductSurface] = struct{}{}
		}
	}
	for _, health := range console.JobHealth {
		if health.ProductSurface != "" {
			seen[health.ProductSurface] = struct{}{}
		}
	}
	if console.ProductSurface != "" {
		seen[console.ProductSurface] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func availabilitySLO(console Console, productSurface string) SLOIndicator {
	for _, product := range console.Products {
		if product.ProductSurface == productSurface && product.Status == products.ProductStatusDisabled {
			return SLOIndicator{Status: "critical", Value: 0, Threshold: 1, Reason: "product disabled"}
		}
	}
	for _, installation := range console.Installations {
		if installation.ProductSurface == productSurface {
			if !installation.Enabled {
				return SLOIndicator{Status: "critical", Value: 0, Threshold: 1, Reason: "installation disabled"}
			}
			return SLOIndicator{Status: "ok", Value: 1, Threshold: 1}
		}
	}
	return SLOIndicator{Status: "unknown", Reason: "no installation data"}
}

func latencySLO(console Console, productSurface string) SLOIndicator {
	metric := metricForProduct(console.Metrics, productSurface)
	if metric == nil || metric.LatencySamples == 0 {
		return SLOIndicator{Status: "unknown", Reason: "no latency samples in period"}
	}
	status := "ok"
	if metric.P95LatencyMS > 15000 {
		status = "critical"
	} else if metric.P95LatencyMS > 5000 {
		status = "warning"
	}
	return SLOIndicator{Status: status, Value: metric.P95LatencyMS, Threshold: 5000, Reason: "p95 duration_ms from observability events"}
}

func toolSuccessSLO(console Console, productSurface string) SLOIndicator {
	if metric := metricForProduct(console.Metrics, productSurface); metric != nil {
		calls := metric.RuntimeToolCalls + metric.MCPToolCalls
		if calls > 0 {
			success := 1 - float64(metric.ToolErrors)/float64(calls)
			status := "ok"
			if success < 0.90 {
				status = "critical"
			} else if success < 0.95 {
				status = "warning"
			}
			return SLOIndicator{Status: status, Value: success, Threshold: 0.95, Reason: "tool and MCP events"}
		}
	}
	if console.RuntimeUsage == nil || console.RuntimeUsage.ToolCalls == 0 {
		return SLOIndicator{Status: "unknown", Reason: "no tool calls in period"}
	}
	success := 1 - float64(console.RuntimeUsage.ToolErrors)/float64(console.RuntimeUsage.ToolCalls)
	status := "ok"
	if success < 0.90 {
		status = "critical"
	} else if success < 0.95 {
		status = "warning"
	}
	return SLOIndicator{Status: status, Value: success, Threshold: 0.95}
}

func metricForProduct(values []ProductOperationalMetrics, productSurface string) *ProductOperationalMetrics {
	for i := range values {
		if values[i].ProductSurface == productSurface {
			return &values[i]
		}
	}
	return nil
}

func evalScoreSLO(console Console, productSurface string) SLOIndicator {
	var latest *securityevals.Report
	for _, report := range console.EvalReports {
		if report.ProductSurface != productSurface {
			continue
		}
		if latest == nil || report.CreatedAt.After(latest.CreatedAt) {
			current := report
			latest = &current
		}
	}
	if latest == nil {
		return SLOIndicator{Status: "unknown", Reason: "no eval report"}
	}
	status := "ok"
	if latest.Status == "failed" || (latest.Threshold > 0 && latest.Score < latest.Threshold) {
		status = "critical"
	}
	return SLOIndicator{Status: status, Value: latest.Score, Threshold: latest.Threshold, Reason: latest.Suite}
}

func costCeilingSLO(console Console, productSurface string) SLOIndicator {
	if console.CostSummary == nil || console.RuntimePolicy == nil {
		return SLOIndicator{Status: "unknown", Reason: "cost summary or runtime policy unavailable"}
	}
	budget := console.RuntimePolicy.ControlPlane.MonthlyCostBudgetCents
	if productPolicy, ok := console.RuntimePolicy.ControlPlane.ProductPolicies[productSurface]; ok && productPolicy.MonthlyCostBudgetCents > 0 {
		budget = productPolicy.MonthlyCostBudgetCents
	}
	if budget <= 0 {
		return SLOIndicator{Status: "unknown", Reason: "no cost ceiling configured"}
	}
	used := console.CostSummary.EstimatedCostCents
	if console.CostSummary.ProductSurface == "" && productSurface != "" {
		for _, item := range console.CostSummary.ByProduct {
			if item.Key == productSurface {
				used = item.EstimatedCostCents
				break
			}
		}
	}
	ratio := float64(used) / float64(budget)
	status := "ok"
	if ratio >= 1 {
		status = "critical"
	} else if ratio >= 0.80 {
		status = "warning"
	}
	return SLOIndicator{Status: status, Value: ratio, Threshold: 1, Reason: "monthly cost budget"}
}

func worstSLOStatus(values ...SLOIndicator) string {
	status := "ok"
	for _, value := range values {
		if value.Status == "critical" {
			return "critical"
		}
		if value.Status == "warning" {
			status = "warning"
		}
	}
	return status
}

func alert(kind, severity, productSurface, message, source string, evidence map[string]any, at time.Time) Alert {
	productSurface = strings.TrimSpace(productSurface)
	if at.IsZero() {
		at = time.Now().UTC()
	}
	id := kind
	if productSurface != "" {
		id += ":" + productSurface
	}
	return Alert{ID: id, Severity: severity, Type: kind, ProductSurface: productSurface, Message: message, Source: source, Evidence: evidence, OccurredAt: at}
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}
