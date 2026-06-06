package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/capabilities"
	"github.com/devpablocristo/companion/internal/products"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/securityevals"
	"github.com/google/uuid"
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
	ListObservabilityEvents(ctx context.Context, orgID, productSurface string, runID *uuid.UUID, limit int) ([]runtime.ObservabilityEvent, error)
}

type CostLedger interface {
	GetCostSummary(ctx context.Context, orgID, productSurface, period string, limit int) (runtime.CostSummary, error)
}

type RuntimeControls interface {
	GetRuntimePolicy(ctx context.Context, orgID string) (runtime.TenantRuntimePolicy, error)
	GetRuntimeUsage(ctx context.Context, orgID, period string) (runtime.TenantRuntimeUsage, error)
}

type Deps struct {
	Products        ProductCatalog
	Capabilities    CapabilityCatalog
	Evals           EvalReports
	Observability   ObservabilityEvents
	Costs           CostLedger
	RuntimeControls RuntimeControls
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
		console.Events, err = u.deps.Observability.ListObservabilityEvents(ctx, q.OrgID, q.ProductSurface, nil, q.Limit)
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
	console.Alerts = buildAlerts(console)
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
			alerts = append(alerts, alert("rate_limit_abuse", "warning", event.ProductSurface, "Product rate limit guardrail triggered", "observability", map[string]any{"event_name": event.EventName}, event.OccurredAt))
		}
		if event.EventType == "guardrail" && event.EventName == "mcp_runtime_policy" {
			alerts = append(alerts, alert("mcp_runtime_policy_block", "warning", event.ProductSurface, "MCP tool blocked by runtime policy", "observability", mcpRuntimePolicyEvidence(event), event.OccurredAt))
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
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Severity == alerts[j].Severity {
			return alerts[i].OccurredAt.After(alerts[j].OccurredAt)
		}
		return severityRank(alerts[i].Severity) > severityRank(alerts[j].Severity)
	})
	return alerts
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

func buildSLOs(console Console) []ProductSLO {
	surfaces := productSurfaces(console)
	out := make([]ProductSLO, 0, len(surfaces))
	for _, surface := range surfaces {
		availability := availabilitySLO(console, surface)
		toolSuccess := toolSuccessSLO(console)
		evalScore := evalScoreSLO(console, surface)
		costCeiling := costCeilingSLO(console, surface)
		slo := ProductSLO{
			ProductSurface:  surface,
			Latency:         SLOIndicator{Status: "unknown", Reason: "latency metrics are not persisted yet"},
			Availability:    availability,
			ToolSuccessRate: toolSuccess,
			EvalScore:       evalScore,
			CostCeiling:     costCeiling,
		}
		slo.Status = worstSLOStatus(availability, toolSuccess, evalScore, costCeiling)
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

func toolSuccessSLO(console Console) SLOIndicator {
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
