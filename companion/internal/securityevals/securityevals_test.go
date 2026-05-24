package securityevals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devpablocristo/companion/internal/runtime"
)

type suite struct {
	Version    int                `json:"version"`
	Thresholds map[string]float64 `json:"thresholds"`
	Cases      []caseSpec         `json:"cases"`
}

type caseSpec struct {
	ID                string `json:"id"`
	Category          string `json:"category"`
	Input             string `json:"input"`
	ExpectedFinding   string `json:"expected_finding"`
	ExpectedGuardrail string `json:"expected_guardrail"`
}

func TestSecurityAdversarialSuite(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(findFile(t, "scripts/evals/security-adversarial.json"))
	if err != nil {
		t.Fatalf("read security eval suite: %v", err)
	}
	var s suite
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse security eval suite: %v", err)
	}
	if s.Version != 1 {
		t.Fatalf("unsupported security eval version: %d", s.Version)
	}
	if len(s.Cases) == 0 {
		t.Fatal("security eval suite must contain cases")
	}
	misses := 0
	guardrailMisses := 0
	for _, c := range s.Cases {
		t.Run(c.ID, func(t *testing.T) {
			findings := runtime.DetectAdversarialContent(c.Input)
			if !hasFinding(findings, c.ExpectedFinding) {
				misses++
				t.Fatalf("expected finding %q, got %+v", c.ExpectedFinding, findings)
			}
			event := runtime.CheckPromptInjection(c.Input)
			if c.ExpectedGuardrail != "" {
				if event == nil || event.Type != c.ExpectedGuardrail {
					guardrailMisses++
					t.Fatalf("expected guardrail %q, got %+v", c.ExpectedGuardrail, event)
				}
			}
		})
	}
	if float64(misses) > s.Thresholds["missed_findings_max"] {
		t.Fatalf("missed findings %d exceeds threshold %.0f", misses, s.Thresholds["missed_findings_max"])
	}
	if float64(guardrailMisses) > s.Thresholds["false_negative_guardrails_max"] {
		t.Fatalf("guardrail misses %d exceeds threshold %.0f", guardrailMisses, s.Thresholds["false_negative_guardrails_max"])
	}
}

func hasFinding(findings []runtime.ThreatFinding, want string) bool {
	for _, finding := range findings {
		if finding.Type == want {
			return true
		}
	}
	return false
}

func findFile(t *testing.T, rel string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("file %q not found from %s", rel, cwd)
	return ""
}
