package productevals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var productSurfacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Tenants struct {
	Primary string `json:"primary"`
	Shadow  string `json:"shadow,omitempty"`
}

type Pack struct {
	Version        int                `json:"version"`
	SuiteID        string             `json:"suite_id,omitempty"`
	ProductSurface string             `json:"product_surface,omitempty"`
	Description    string             `json:"description,omitempty"`
	NonBlocking    bool               `json:"non_blocking,omitempty"`
	Thresholds     map[string]float64 `json:"thresholds"`
	Tenants        Tenants            `json:"tenants"`
	Cases          []Case             `json:"cases"`
}

type Case struct {
	ID                      string   `json:"id"`
	Category                string   `json:"category,omitempty"`
	Query                   string   `json:"query"`
	ExpectedIntent          string   `json:"expected_intent,omitempty"`
	ExpectedCapability      string   `json:"expected_capability,omitempty"`
	ExpectedArgsContains    []string `json:"expected_args_contains,omitempty"`
	ExpectedEvidenceKeys    []string `json:"expected_evidence_keys,omitempty"`
	ExpectedGuardrail       string   `json:"expected_guardrail,omitempty"`
	ActionSafety            string   `json:"action_safety,omitempty"`
	TenantLeakageCheck      bool     `json:"tenant_leakage_check,omitempty"`
	MustCiteEvidence        bool     `json:"must_cite_evidence,omitempty"`
	ForbiddenAnswerContains []string `json:"forbidden_answer_contains,omitempty"`
}

type EvalInput struct {
	CaseID         string
	OrgID          string
	ProductSurface string
	UserID         string
	Query          string
}

type ToolCall struct {
	CapabilityID string         `json:"capability_id"`
	Args         map[string]any `json:"args,omitempty"`
	Evidence     map[string]any `json:"evidence,omitempty"`
}

type Action struct {
	CapabilityID     string `json:"capability_id,omitempty"`
	SideEffectType   string `json:"side_effect_type,omitempty"`
	ApprovalRequired bool   `json:"approval_required,omitempty"`
}

type EvalOutput struct {
	OrgID          string         `json:"org_id,omitempty"`
	ProductSurface string         `json:"product_surface,omitempty"`
	Intent         string         `json:"intent,omitempty"`
	Reply          string         `json:"reply,omitempty"`
	Guardrails     []string       `json:"guardrails,omitempty"`
	ToolCalls      []ToolCall     `json:"tool_calls,omitempty"`
	Evidence       map[string]any `json:"evidence,omitempty"`
	Actions        []Action       `json:"actions,omitempty"`
}

type Runner interface {
	RunProductEvalCase(ctx context.Context, in EvalInput) (EvalOutput, error)
}

type CaseResult struct {
	ID     string          `json:"id"`
	Checks map[string]bool `json:"checks"`
	Passed bool            `json:"passed"`
	Errors []string        `json:"errors,omitempty"`
}

type Report struct {
	SuiteID          string             `json:"suite_id"`
	ProductSurface   string             `json:"product_surface"`
	Status           string             `json:"status"`
	NonBlocking      bool               `json:"non_blocking"`
	Metrics          map[string]float64 `json:"metrics"`
	Thresholds       map[string]float64 `json:"thresholds"`
	FailedThresholds []string           `json:"failed_thresholds,omitempty"`
	Results          []CaseResult       `json:"results"`
}

func LoadPack(path string) (Pack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, fmt.Errorf("read product eval pack: %w", err)
	}
	var pack Pack
	if err := json.Unmarshal(raw, &pack); err != nil {
		return Pack{}, fmt.Errorf("parse product eval pack: %w", err)
	}
	pack = normalizePack(pack, path)
	if err := ValidatePack(pack); err != nil {
		return Pack{}, err
	}
	return pack, nil
}

func LoadPacks(root string) ([]Pack, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read eval pack directory: %w", err)
	}
	packs := make([]Pack, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, "-golden.json") {
			continue
		}
		pack, err := LoadPack(filepath.Join(root, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool {
		return packs[i].SuiteID < packs[j].SuiteID
	})
	return packs, nil
}

func ValidatePack(pack Pack) error {
	if pack.Version != 1 {
		return fmt.Errorf("unsupported product eval pack version %d", pack.Version)
	}
	if !productSurfacePattern.MatchString(strings.TrimSpace(pack.ProductSurface)) {
		return fmt.Errorf("valid product_surface is required")
	}
	if strings.TrimSpace(pack.SuiteID) == "" {
		return fmt.Errorf("suite_id is required")
	}
	if strings.TrimSpace(pack.Tenants.Primary) == "" {
		return fmt.Errorf("tenants.primary is required")
	}
	if len(pack.Cases) == 0 {
		return fmt.Errorf("product eval pack must contain cases")
	}
	seen := make(map[string]struct{}, len(pack.Cases))
	for _, c := range pack.Cases {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			return fmt.Errorf("case id is required")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate case id %q", id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(c.Query) == "" {
			return fmt.Errorf("case %q query is required", id)
		}
	}
	return nil
}

func EvaluatePack(ctx context.Context, pack Pack, runner Runner) (Report, error) {
	pack = normalizePack(pack, "")
	if err := ValidatePack(pack); err != nil {
		return Report{}, err
	}
	if runner == nil {
		return Report{}, fmt.Errorf("product eval runner is required")
	}
	stats := newMetricStats()
	results := make([]CaseResult, 0, len(pack.Cases))
	for _, c := range pack.Cases {
		output, err := runner.RunProductEvalCase(ctx, EvalInput{
			CaseID:         c.ID,
			OrgID:          pack.Tenants.Primary,
			ProductSurface: pack.ProductSurface,
			UserID:         "product-eval",
			Query:          c.Query,
		})
		result := evaluateCase(pack, c, output, err, stats)
		results = append(results, result)
	}
	metrics := stats.metrics()
	failedThresholds := evaluateThresholds(metrics, pack.Thresholds)
	status := "passed"
	if len(failedThresholds) > 0 {
		status = "failed"
	}
	return Report{
		SuiteID:          pack.SuiteID,
		ProductSurface:   pack.ProductSurface,
		Status:           status,
		NonBlocking:      pack.NonBlocking,
		Metrics:          metrics,
		Thresholds:       cloneThresholds(pack.Thresholds),
		FailedThresholds: failedThresholds,
		Results:          results,
	}, nil
}

func FindRepoFile(rel string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("file %q not found searching upward from %s", rel, cwd)
}

func evaluateCase(pack Pack, c Case, output EvalOutput, runErr error, stats *metricStats) CaseResult {
	checks := map[string]bool{}
	var errs []string
	fail := func(check, message string) {
		checks[check] = false
		errs = append(errs, message)
	}
	pass := func(check string) {
		checks[check] = true
	}
	if runErr != nil {
		fail("runner", runErr.Error())
		return CaseResult{ID: c.ID, Checks: checks, Passed: false, Errors: errs}
	}
	if strings.TrimSpace(output.OrgID) != "" && output.OrgID != pack.Tenants.Primary {
		fail("scope", fmt.Sprintf("org_id mismatch: got %q want %q", output.OrgID, pack.Tenants.Primary))
	} else {
		pass("scope")
	}
	if strings.TrimSpace(output.ProductSurface) != "" && output.ProductSurface != pack.ProductSurface {
		fail("product_scope", fmt.Sprintf("product_surface mismatch: got %q want %q", output.ProductSurface, pack.ProductSurface))
	} else {
		pass("product_scope")
	}
	if c.ExpectedGuardrail != "" {
		ok := containsString(output.Guardrails, c.ExpectedGuardrail)
		stats.add("guardrail_accuracy", ok)
		if ok {
			pass("guardrail")
		} else {
			fail("guardrail", fmt.Sprintf("expected guardrail %q", c.ExpectedGuardrail))
		}
		return CaseResult{ID: c.ID, Checks: checks, Passed: len(errs) == 0, Errors: errs}
	}
	if c.ExpectedIntent != "" {
		ok := output.Intent == c.ExpectedIntent
		stats.add("routing_accuracy", ok)
		if ok {
			pass("routing")
		} else {
			fail("routing", fmt.Sprintf("expected intent %q got %q", c.ExpectedIntent, output.Intent))
		}
	}
	var matchedTool *ToolCall
	if c.ExpectedCapability != "" {
		for i := range output.ToolCalls {
			if output.ToolCalls[i].CapabilityID == c.ExpectedCapability {
				matchedTool = &output.ToolCalls[i]
				break
			}
		}
		ok := matchedTool != nil
		stats.add("tool_selection_accuracy", ok)
		if ok {
			pass("tool_selection")
		} else {
			fail("tool_selection", fmt.Sprintf("expected capability %q", c.ExpectedCapability))
		}
	}
	if len(c.ExpectedArgsContains) > 0 {
		ok := matchedTool != nil && containsAllJSONKeys(matchedTool.Args, c.ExpectedArgsContains)
		stats.add("tool_args_accuracy", ok)
		if ok {
			pass("tool_args")
		} else {
			fail("tool_args", fmt.Sprintf("expected args containing %v", c.ExpectedArgsContains))
		}
	}
	if len(c.ExpectedEvidenceKeys) > 0 || c.MustCiteEvidence {
		evidence := output.Evidence
		if matchedTool != nil && len(matchedTool.Evidence) > 0 {
			evidence = matchedTool.Evidence
		}
		ok := len(evidence) > 0
		if len(c.ExpectedEvidenceKeys) > 0 {
			ok = ok && containsAllJSONKeys(evidence, c.ExpectedEvidenceKeys)
		}
		stats.add("evidence_quality", ok)
		if ok {
			pass("evidence")
		} else {
			fail("evidence", fmt.Sprintf("expected evidence keys %v", c.ExpectedEvidenceKeys))
		}
	}
	if c.TenantLeakageCheck {
		ok := !mentions(pack.Tenants.Shadow, output.Reply, output.Evidence, output.ToolCalls)
		stats.addCount("tenant_leakage", !ok)
		if ok {
			pass("tenant_leakage")
		} else {
			fail("tenant_leakage", fmt.Sprintf("output mentions shadow tenant %q", pack.Tenants.Shadow))
		}
	}
	if len(c.ForbiddenAnswerContains) > 0 {
		ok := true
		for _, token := range c.ForbiddenAnswerContains {
			if strings.Contains(strings.ToLower(output.Reply), strings.ToLower(token)) {
				ok = false
				break
			}
		}
		stats.addCount("hallucination_rate", !ok)
		if ok {
			pass("hallucination")
		} else {
			fail("hallucination", "reply contains forbidden answer token")
		}
	}
	if c.ActionSafety != "" {
		ok := actionSafetyOK(c.ActionSafety, output.Actions)
		stats.add("action_safety", ok)
		if ok {
			pass("action_safety")
		} else {
			fail("action_safety", fmt.Sprintf("action safety policy %q failed", c.ActionSafety))
		}
	}
	return CaseResult{ID: c.ID, Checks: checks, Passed: len(errs) == 0, Errors: errs}
}

type metricStats struct {
	passed map[string]int
	total  map[string]int
	count  map[string]int
}

func newMetricStats() *metricStats {
	return &metricStats{passed: map[string]int{}, total: map[string]int{}, count: map[string]int{}}
}

func (s *metricStats) add(metric string, ok bool) {
	s.total[metric]++
	if ok {
		s.passed[metric]++
	}
}

func (s *metricStats) addCount(metric string, increment bool) {
	s.total[metric]++
	if increment {
		s.count[metric]++
	}
}

func (s *metricStats) metrics() map[string]float64 {
	out := map[string]float64{}
	for metric, total := range s.total {
		if total == 0 {
			out[metric] = 1
			continue
		}
		if strings.HasSuffix(metric, "_rate") || metric == "tenant_leakage" {
			out[metric] = float64(s.count[metric]) / float64(total)
			continue
		}
		out[metric] = float64(s.passed[metric]) / float64(total)
	}
	return out
}

func evaluateThresholds(metrics, thresholds map[string]float64) []string {
	var failed []string
	for key, threshold := range thresholds {
		switch {
		case strings.HasSuffix(key, "_min"):
			metric := strings.TrimSuffix(key, "_min")
			if metrics[metric] < threshold {
				failed = append(failed, key)
			}
		case strings.HasSuffix(key, "_max"):
			metric := strings.TrimSuffix(key, "_max")
			if metrics[metric] > threshold {
				failed = append(failed, key)
			}
		}
	}
	sort.Strings(failed)
	return failed
}

func normalizePack(pack Pack, path string) Pack {
	pack.SuiteID = strings.TrimSpace(pack.SuiteID)
	if pack.SuiteID == "" && path != "" {
		pack.SuiteID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	pack.ProductSurface = strings.TrimSpace(strings.ToLower(pack.ProductSurface))
	if pack.ProductSurface == "" && path != "" {
		pack.ProductSurface = inferProductSurface(filepath.Base(path))
	}
	pack.Tenants.Primary = strings.TrimSpace(pack.Tenants.Primary)
	pack.Tenants.Shadow = strings.TrimSpace(pack.Tenants.Shadow)
	if pack.Thresholds == nil {
		pack.Thresholds = map[string]float64{}
	}
	for i := range pack.Cases {
		pack.Cases[i].ID = strings.TrimSpace(pack.Cases[i].ID)
		pack.Cases[i].Category = strings.TrimSpace(pack.Cases[i].Category)
		pack.Cases[i].Query = strings.TrimSpace(pack.Cases[i].Query)
		pack.Cases[i].ExpectedIntent = strings.TrimSpace(pack.Cases[i].ExpectedIntent)
		pack.Cases[i].ExpectedCapability = strings.TrimSpace(pack.Cases[i].ExpectedCapability)
		pack.Cases[i].ExpectedGuardrail = strings.TrimSpace(pack.Cases[i].ExpectedGuardrail)
		pack.Cases[i].ActionSafety = strings.TrimSpace(strings.ToLower(pack.Cases[i].ActionSafety))
	}
	return pack
}

func inferProductSurface(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	if strings.HasSuffix(name, "-golden") {
		return strings.TrimSuffix(name, "-golden")
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsAllJSONKeys(value any, keys []string) bool {
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	haystack := string(raw)
	for _, key := range keys {
		if !strings.Contains(haystack, key) {
			return false
		}
	}
	return true
}

func mentions(token, reply string, values ...any) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.Contains(reply, token) {
		return true
	}
	for _, value := range values {
		raw, err := json.Marshal(value)
		if err == nil && strings.Contains(string(raw), token) {
			return true
		}
	}
	return false
}

func actionSafetyOK(policy string, actions []Action) bool {
	switch strings.TrimSpace(strings.ToLower(policy)) {
	case "read_only":
		for _, action := range actions {
			if action.SideEffectType != "" && action.SideEffectType != "read" {
				return false
			}
		}
		return true
	case "requires_approval", "no_write_without_approval":
		for _, action := range actions {
			if action.SideEffectType != "" && action.SideEffectType != "read" && !action.ApprovalRequired {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func cloneThresholds(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
