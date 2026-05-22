package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// goldenCase es una entrada del archivo scripts/evals/ponti-golden.json.
type goldenCase struct {
	ID                   string   `json:"id"`
	Query                string   `json:"query"`
	ExpectedIntent       string   `json:"expected_intent"`
	ExpectedCapability   string   `json:"expected_capability"`
	ExpectedArgsContains []string `json:"expected_args_contains"`
	ExpectedGuardrail    string   `json:"expected_guardrail"`
	TenantLeakageCheck   bool     `json:"tenant_leakage_check"`
}

type goldenSuite struct {
	Version    int                `json:"version"`
	Thresholds map[string]float64 `json:"thresholds"`
	Tenants    struct {
		Primary string `json:"primary"`
		Shadow  string `json:"shadow"`
	} `json:"tenants"`
	Cases []goldenCase `json:"cases"`
}

// TestEvals_PontiGolden corre la suite de golden queries del piloto Ponti.
//
// Esta versión verifica:
//   - Intent classification (determinístico, no requiere LLM).
//   - Guardrails de prompt injection se disparan.
//   - El JSON file está bien formado.
//
// La verificación de tool selection real (qué capability invoca el LLM) y de
// tenant_leakage real (mediante respuestas del LLM citando datos de otro
// tenant) requiere una corrida con LLM real y entorno con datos sembrados;
// queda como fase 2 del piloto. Acá se asegura el piso reproducible.
func TestEvals_PontiGolden(t *testing.T) {
	t.Parallel()

	path, err := findRepoFile("scripts/evals/ponti-golden.json")
	if err != nil {
		t.Skipf("golden file not found, skipping eval (run from repo root): %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	var suite goldenSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("parse golden file: %v", err)
	}
	if suite.Version != 1 {
		t.Fatalf("unsupported golden suite version: %d", suite.Version)
	}
	if len(suite.Cases) == 0 {
		t.Fatal("golden suite must contain at least one case")
	}

	// Verificar que todos los thresholds críticos están declarados.
	for _, key := range []string{"routing_accuracy_min", "tool_selection_accuracy_min", "tenant_leakage_max"} {
		if _, ok := suite.Thresholds[key]; !ok {
			t.Errorf("threshold %q is not declared in golden suite", key)
		}
	}

	toolkit := &ToolKit{Handlers: make(map[string]ToolHandler)}
	provider := &fakeLLMProvider{}
	repo := &inMemoryTraceRepo{}
	orch := NewOrchestrator(provider, toolkit, ContextPorts{})
	orch.SetTraceRepository(repo)

	totalRouted := 0
	correctRouted := 0
	guardrailTriggered := 0

	for _, c := range suite.Cases {
		t.Run(c.ID, func(t *testing.T) {
			// Cada caso usa una respuesta fija "ok" del LLM. El objetivo no es
			// validar la respuesta del LLM (eso requiere LLM real) sino que el
			// pipeline de runtime dispare correctamente intent + guardrails.
			provider.responses = []ChatResponse{{Text: "ok"}}
			provider.callCount = 0

			result, err := orch.Run(context.Background(), RunInput{
				UserID:         "u-eval",
				OrgID:          suite.Tenants.Primary,
				Message:        c.Query,
				ProductSurface: "ponti",
				TaskID:         func() *uuid.UUID { id := uuid.New(); return &id }(),
			})
			if err != nil {
				t.Fatalf("orchestrator run: %v", err)
			}

			// Caso de guardrail esperado: el LLM no debe haber sido invocado.
			if c.ExpectedGuardrail != "" {
				if len(result.Trace.GuardrailEvents) == 0 {
					t.Fatalf("expected guardrail %q to trigger, got none", c.ExpectedGuardrail)
				}
				found := false
				for _, e := range result.Trace.GuardrailEvents {
					if e.Type == c.ExpectedGuardrail {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected guardrail type %q, got %+v", c.ExpectedGuardrail, result.Trace.GuardrailEvents)
				}
				guardrailTriggered++
				return
			}

			// Routing: intent clasificado debe matchear.
			totalRouted++
			if c.ExpectedIntent != "" && result.Trace.Intent == c.ExpectedIntent {
				correctRouted++
			} else if c.ExpectedIntent == "" {
				correctRouted++
			} else {
				t.Logf("routing mismatch: query=%q expected_intent=%q got=%q",
					c.Query, c.ExpectedIntent, result.Trace.Intent)
			}

			// Identity chain: tenant del trace debe ser el primary, no el shadow.
			if result.Trace.IdentityChain.Tenant != suite.Tenants.Primary {
				t.Errorf("identity chain tenant mismatch: want %q got %q",
					suite.Tenants.Primary, result.Trace.IdentityChain.Tenant)
			}

			// Tenant leakage check estructural: el reply no debe contener
			// el ID del shadow tenant. Es un sanity check por si en el futuro
			// el orchestrator empieza a citar otros tenants en su prompt.
			if c.TenantLeakageCheck {
				if strings.Contains(result.Reply, suite.Tenants.Shadow) {
					t.Fatalf("tenant leakage: reply mentions shadow tenant %q", suite.Tenants.Shadow)
				}
			}
		})
	}

	if totalRouted == 0 {
		return
	}
	accuracy := float64(correctRouted) / float64(totalRouted)
	min := suite.Thresholds["routing_accuracy_min"]
	if accuracy < min {
		t.Errorf("routing_accuracy=%.2f below threshold %.2f (correct=%d total=%d)",
			accuracy, min, correctRouted, totalRouted)
	}
	t.Logf("eval summary: routing_accuracy=%.2f (correct=%d/%d), guardrails_triggered=%d",
		accuracy, correctRouted, totalRouted, guardrailTriggered)
}

// findRepoFile busca un archivo relativo a la raíz del repo subiendo
// directorios desde el cwd. Permite que el test funcione tanto desde
// scripts/ como desde internal/runtime/.
func findRepoFile(rel string) (string, error) {
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
