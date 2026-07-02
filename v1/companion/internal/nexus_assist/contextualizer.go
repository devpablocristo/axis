package nexus_assist

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion/internal/nexusclient"
	coreai "github.com/devpablocristo/platform/kernels/ai/go"
)

// ErrRequestForbidden indica que el request pertenece a otro org que el del
// caller. Companion llama a Nexus con una service key cross_org, así que Nexus
// no acota el request al caller: la pertenencia se hace cumplir acá (fail-closed).
var ErrRequestForbidden = errors.New("nexus request not accessible for this org")

// Contextualizer arma summaries en lenguaje natural para approvers humanos.
// Lee el request en Nexus, lo pasa por Gemini y devuelve un summary breve.
//
// La console de Nexus invoca este endpoint como secondary call al renderizar
// una approval card. Si Companion no responde (timeout / down), la console
// muestra el request sin summary — Companion no es dependencia hard de Nexus.
type Contextualizer struct {
	nexus *nexusclient.Client
	llm   coreai.Provider
}

// NewContextualizer crea un Contextualizer con Gemini obligatorio.
func NewContextualizer(nexus *nexusclient.Client, llm coreai.Provider) *Contextualizer {
	return &Contextualizer{nexus: nexus, llm: llm}
}

// Explain devuelve un summary natural-language para el request_id dado.
// Degraded queda false en el camino soportado; si Gemini falla se devuelve error.
func (c *Contextualizer) Explain(ctx context.Context, requestID, callerOrgID string, allowCrossOrg bool) (summary string, degraded bool, err error) {
	if requestID == "" {
		return "", false, fmt.Errorf("request_id is required")
	}
	if c.llm == nil {
		return "", false, fmt.Errorf("gemini provider is required")
	}
	req, st, err := c.nexus.GetRequest(ctx, requestID)
	if err != nil {
		return "", false, fmt.Errorf("get request: %w", err)
	}
	if st == 404 {
		return "", false, fmt.Errorf("request not found")
	}
	// Fail-closed cross-tenant guard: enforce the request belongs to the caller's
	// org BEFORE handing it to the LLM. Skipped only for cross-org callers / dev
	// (no auth context), which the handler decides.
	if !allowCrossOrg && strings.TrimSpace(req.OrgID) != strings.TrimSpace(callerOrgID) {
		return "", false, ErrRequestForbidden
	}
	resp, err := c.llm.Chat(ctx, coreai.ChatRequest{
		SystemPrompt: contextualizerSystemPrompt,
		Messages:     []coreai.Message{{Role: "user", Content: buildUserMessage(req)}},
		MaxTokens:    300,
	})
	if err != nil {
		return "", false, fmt.Errorf("gemini contextualizer: %w", err)
	}
	if resp.Text == "" {
		return "", false, fmt.Errorf("gemini contextualizer returned empty summary")
	}
	return resp.Text, false, nil
}

func buildUserMessage(r nexusclient.RequestSummary) string {
	return fmt.Sprintf(
		"Requester: %s (%s)\nAcción: %s\nTarget: %s / %s\nMotivo: %s\nRisk: %s\nDecisión: %s (%s)",
		r.RequesterID, r.RequesterType,
		r.ActionType,
		r.TargetSystem, r.TargetResource,
		r.Reason,
		r.RiskLevel,
		r.Decision, r.DecisionReason,
	)
}

const contextualizerSystemPrompt = `Sos un asistente que ayuda a aprobadores humanos a decidir rápido sobre requests de nexus.

Formato:
- Quién pide y qué pide (1 línea)
- Por qué se frenó (risk level + razón)
- Recomendación breve

Máximo 4 líneas. Español. Sin formato markdown.`
