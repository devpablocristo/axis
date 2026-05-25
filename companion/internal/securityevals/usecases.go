package securityevals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/runtime"
)

type Suite struct {
	Version    int                `json:"version"`
	Thresholds map[string]float64 `json:"thresholds"`
	Cases      []Case             `json:"cases"`
}

type Case struct {
	ID                string `json:"id"`
	Category          string `json:"category"`
	Input             string `json:"input"`
	ExpectedFinding   string `json:"expected_finding"`
	ExpectedGuardrail string `json:"expected_guardrail"`
}

type CaseResult struct {
	ID              string                  `json:"id"`
	Category        string                  `json:"category"`
	Passed          bool                    `json:"passed"`
	Findings        []runtime.ThreatFinding `json:"findings"`
	GuardrailType   string                  `json:"guardrail_type,omitempty"`
	ExpectedFinding string                  `json:"expected_finding,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

type Report struct {
	ID        uuid.UUID      `json:"id,omitempty"`
	OrgID     string         `json:"org_id,omitempty"`
	Suite     string         `json:"suite"`
	Status    string         `json:"status"`
	Score     float64        `json:"score"`
	Threshold float64        `json:"threshold"`
	Results   []CaseResult   `json:"results,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	Raw       map[string]any `json:"report_json,omitempty"`
}

type Repository interface {
	SaveReport(ctx context.Context, report Report) (Report, error)
	ListReports(ctx context.Context, orgID, suite string, limit int) ([]Report, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) ListSuites(ctx context.Context) ([]map[string]any, error) {
	suite, err := LoadAdversarialSuite()
	if err != nil {
		return nil, err
	}
	return []map[string]any{{
		"id":         "security-adversarial",
		"version":    suite.Version,
		"cases":      len(suite.Cases),
		"thresholds": suite.Thresholds,
	}}, nil
}

func (u *Usecases) RunSuite(ctx context.Context, orgID, suiteID, createdBy string) (Report, error) {
	if strings.TrimSpace(suiteID) == "" {
		suiteID = "security-adversarial"
	}
	if suiteID != "security-adversarial" {
		return Report{}, fmt.Errorf("unknown security eval suite %q", suiteID)
	}
	suite, err := LoadAdversarialSuite()
	if err != nil {
		return Report{}, err
	}
	report := EvaluateSuite(suite)
	report.OrgID = strings.TrimSpace(orgID)
	report.Suite = suiteID
	report.CreatedBy = strings.TrimSpace(createdBy)
	if u.repo == nil {
		return report, nil
	}
	return u.repo.SaveReport(ctx, report)
}

func (u *Usecases) ListReports(ctx context.Context, orgID, suite string, limit int) ([]Report, error) {
	if u.repo == nil {
		return nil, fmt.Errorf("security eval repository is not configured")
	}
	return u.repo.ListReports(ctx, orgID, suite, limit)
}

func EvaluateSuite(s Suite) Report {
	results := make([]CaseResult, 0, len(s.Cases))
	passed := 0
	for _, c := range s.Cases {
		findings := runtime.DetectAdversarialContent(c.Input)
		event := runtime.CheckPromptInjection(c.Input)
		result := CaseResult{
			ID:              c.ID,
			Category:        c.Category,
			Findings:        findings,
			ExpectedFinding: c.ExpectedFinding,
		}
		if event != nil {
			result.GuardrailType = event.Type
		}
		hasExpectedFinding := reportHasFinding(findings, c.ExpectedFinding)
		hasExpectedGuardrail := c.ExpectedGuardrail == "" || (event != nil && event.Type == c.ExpectedGuardrail)
		result.Passed = hasExpectedFinding && hasExpectedGuardrail
		if !result.Passed {
			result.Error = "expected finding or guardrail was not detected"
		} else {
			passed++
		}
		results = append(results, result)
	}
	score := 1.0
	if len(results) > 0 {
		score = float64(passed) / float64(len(results))
	}
	threshold := 1.0
	if value, ok := s.Thresholds["score_min"]; ok && value > 0 {
		threshold = value
	}
	status := "passed"
	if score < threshold {
		status = "failed"
	}
	raw := map[string]any{
		"version":    s.Version,
		"thresholds": s.Thresholds,
		"results":    results,
	}
	return Report{Suite: "security-adversarial", Status: status, Score: score, Threshold: threshold, Results: results, Raw: raw}
}

func LoadAdversarialSuite() (Suite, error) {
	path, err := findSuiteFile("scripts/evals/security-adversarial.json")
	if err != nil {
		return Suite{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, fmt.Errorf("read security eval suite: %w", err)
	}
	var suite Suite
	if err := json.Unmarshal(raw, &suite); err != nil {
		return Suite{}, fmt.Errorf("parse security eval suite: %w", err)
	}
	if suite.Version != 1 {
		return Suite{}, fmt.Errorf("unsupported security eval suite version %d", suite.Version)
	}
	if len(suite.Cases) == 0 {
		return Suite{}, fmt.Errorf("security eval suite has no cases")
	}
	return suite, nil
}

func reportHasFinding(findings []runtime.ThreatFinding, want string) bool {
	for _, finding := range findings {
		if finding.Type == want {
			return true
		}
	}
	return false
}

func findSuiteFile(rel string) (string, error) {
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
	return "", fmt.Errorf("security eval suite %q not found from %s", rel, cwd)
}
