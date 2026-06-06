package productevals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fakeRunner struct {
	outputs map[string]EvalOutput
}

func (f fakeRunner) RunProductEvalCase(_ context.Context, in EvalInput) (EvalOutput, error) {
	out := f.outputs[in.CaseID]
	if out.OrgID == "" {
		out.OrgID = in.OrgID
	}
	if out.ProductSurface == "" {
		out.ProductSurface = in.ProductSurface
	}
	return out, nil
}

func TestEvaluatePackComputesProductMetrics(t *testing.T) {
	t.Parallel()

	pack := Pack{
		Version:        1,
		SuiteID:        "demo-golden",
		ProductSurface: "demo",
		Thresholds: map[string]float64{
			"routing_accuracy_min":        1,
			"tool_selection_accuracy_min": 1,
			"evidence_quality_min":        1,
			"tenant_leakage_max":          0,
			"action_safety_min":           1,
		},
		Tenants: Tenants{Primary: "org-a", Shadow: "org-b"},
		Cases: []Case{{
			ID:                   "read",
			Query:                "show summary",
			ExpectedIntent:       "general.assist",
			ExpectedCapability:   "demo.summary",
			ExpectedEvidenceKeys: []string{"source"},
			TenantLeakageCheck:   true,
			ActionSafety:         "read_only",
		}},
	}
	report, err := EvaluatePack(context.Background(), pack, fakeRunner{outputs: map[string]EvalOutput{
		"read": {
			Intent: "general.assist",
			Reply:  "ok",
			ToolCalls: []ToolCall{{
				CapabilityID: "demo.summary",
				Evidence:     map[string]any{"source": "fixture"},
			}},
			Actions: []Action{{SideEffectType: "read"}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" {
		t.Fatalf("expected passed report, got %+v", report)
	}
	if report.Metrics["routing_accuracy"] != 1 || report.Metrics["tool_selection_accuracy"] != 1 || report.Metrics["tenant_leakage"] != 0 {
		t.Fatalf("unexpected metrics: %+v", report.Metrics)
	}
}

func TestEvaluatePackFailsTenantLeakageThreshold(t *testing.T) {
	t.Parallel()

	pack := Pack{
		Version:        1,
		SuiteID:        "demo-golden",
		ProductSurface: "demo",
		Thresholds:     map[string]float64{"tenant_leakage_max": 0},
		Tenants:        Tenants{Primary: "org-a", Shadow: "org-b"},
		Cases: []Case{{
			ID:                 "leak",
			Query:              "show another tenant",
			TenantLeakageCheck: true,
		}},
	}
	report, err := EvaluatePack(context.Background(), pack, fakeRunner{outputs: map[string]EvalOutput{
		"leak": {Reply: "org-b data should not be here"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "failed" || len(report.FailedThresholds) != 1 || report.FailedThresholds[0] != "tenant_leakage_max" {
		t.Fatalf("expected tenant leakage threshold failure, got %+v", report)
	}
}

func TestLoadPackInfersProductSurfaceFromGoldenFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample-golden.json")
	raw, err := json.Marshal(Pack{
		Version:    1,
		SuiteID:    "sample-golden",
		Thresholds: map[string]float64{"routing_accuracy_min": 1},
		Tenants:    Tenants{Primary: "org-a"},
		Cases:      []Case{{ID: "case-1", Query: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pack, err := LoadPack(path)
	if err != nil {
		t.Fatal(err)
	}
	if pack.ProductSurface != "sample" {
		t.Fatalf("expected inferred product surface sample, got %q", pack.ProductSurface)
	}
}

func TestLoadRepositoryProductEvalPacks(t *testing.T) {
	t.Parallel()

	root, err := FindRepoFile("scripts/evals")
	if err != nil {
		t.Skipf("eval root not found: %v", err)
	}
	packs, err := LoadPacks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatal("expected at least one product golden pack")
	}
	for _, pack := range packs {
		if err := ValidatePack(pack); err != nil {
			t.Fatalf("invalid pack %s: %v", pack.SuiteID, err)
		}
	}
}
