// Package governance_assist provee asistencia AI sobre el governance plane.
// Su rol es estricto: leer datos de Nexus y armar artefactos para humanos
// (proposals + summaries). Nunca decide ni ejecuta governance.
//
// Vive en Companion porque Nexus debe ser AI-independent. Companion lo expone
// vía /companion/v1/governance-assist/* para que la console de Nexus o un
// admin lo invoquen on-demand.
package governance_assist

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	coreai "github.com/devpablocristo/platform/kernels/ai/go"
	"github.com/devpablocristo/platform/kernels/governance/go/governanceclient"
)

// pattern representa un patrón de aprobación detectado en históricos.
type pattern struct {
	ActionType   string
	Total        int
	Approved     int
	ApprovalRate float64
}

// requestRow es la forma mínima del JSON que devuelve GET /v1/requests para
// poder agregar patrones. Solo leemos campos que precisamos; el resto se
// ignora (forward-compatibility con cambios al API contract de Nexus).
type requestRow struct {
	ActionType string `json:"action_type"`
	Decision   string `json:"decision"`
	Status     string `json:"status"`
	OrgID      string `json:"org_id"`
}

// proposalCandidate es el body que POSTeamos a /v1/learning/proposals.
type proposalCandidate struct {
	OrgID               string  `json:"org_id,omitempty"`
	ProposedName        string  `json:"proposed_name"`
	ProposedDescription string  `json:"proposed_description"`
	ProposedExpression  string  `json:"proposed_expression"`
	ProposedEffect      string  `json:"proposed_effect"`
	ProposedActionType  *string `json:"proposed_action_type,omitempty"`
	ProposedPriority    int     `json:"proposed_priority"`
	PatternSummary      string  `json:"pattern_summary"`
	Confidence          float64 `json:"confidence"`
	SampleSize          int     `json:"sample_size"`
	TimeWindow          string  `json:"time_window"`
}

// llmFields agrupa los campos que Gemini produce.
type llmFields struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
	Effect      string `json:"effect"`
	Summary     string `json:"summary"`
	Priority    int    `json:"priority"`
}

const (
	defaultMinSampleSize  = 50
	defaultMinApprovalRat = 0.90
	defaultListLimit      = 10000
	defaultMaxTokens      = 500
)

// Proposer detecta patrones en Nexus, los enriquece con LLM y POSTea propuestas.
type Proposer struct {
	governance *governanceclient.Client
	llm        coreai.Provider
}

// NewProposer crea un Proposer con Gemini obligatorio.
func NewProposer(governance *governanceclient.Client, llm coreai.Provider) *Proposer {
	return &Proposer{governance: governance, llm: llm}
}

// AnalyzeAndPropose lee histórico de Nexus, detecta patrones, genera propuestas
// con Gemini y las POSTea de vuelta a Nexus.
// Devuelve cuántos patrones se detectaron y cuántas propuestas se aceptaron.
func (p *Proposer) AnalyzeAndPropose(ctx context.Context, orgID string) (analyzed, submitted int, errs []string, err error) {
	patterns, err := p.detectPatterns(ctx, orgID, defaultMinSampleSize, defaultMinApprovalRat)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("detect patterns: %w", err)
	}
	for _, pat := range patterns {
		candidate, err := p.buildCandidate(ctx, orgID, pat)
		if err != nil {
			errs = append(errs, fmt.Sprintf("build %s: %v", pat.ActionType, err))
			continue
		}
		if err := p.submitToNexus(ctx, candidate); err != nil {
			errs = append(errs, fmt.Sprintf("submit %s: %v", pat.ActionType, err))
			continue
		}
		submitted++
	}
	return len(patterns), submitted, errs, nil
}

func (p *Proposer) detectPatterns(ctx context.Context, orgID string, minSampleSize int, minApprovalRate float64) ([]pattern, error) {
	st, raw, err := p.governance.ListRequests(ctx, fmt.Sprintf("limit=%d", defaultListLimit))
	if err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	if st >= 400 {
		return nil, fmt.Errorf("list requests: status %d body %s", st, governanceclient.ParseErrorBody(raw))
	}
	var envelope struct {
		Data []requestRow `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode requests: %w", err)
	}

	type stats struct{ total, approved int }
	byAction := make(map[string]*stats)
	for _, r := range envelope.Data {
		if orgID != "" && r.OrgID != orgID {
			continue
		}
		if r.Decision != "require_approval" {
			continue
		}
		s, ok := byAction[r.ActionType]
		if !ok {
			s = &stats{}
			byAction[r.ActionType] = s
		}
		s.total++
		switch r.Status {
		case "approved", "executed":
			s.approved++
		}
	}

	var out []pattern
	for actionType, s := range byAction {
		if s.total < minSampleSize {
			continue
		}
		rate := float64(s.approved) / float64(s.total)
		if rate >= minApprovalRate {
			out = append(out, pattern{
				ActionType:   actionType,
				Total:        s.total,
				Approved:     s.approved,
				ApprovalRate: rate,
			})
		}
	}
	return out, nil
}

func (p *Proposer) buildCandidate(ctx context.Context, orgID string, pat pattern) (proposalCandidate, error) {
	gen, err := p.askGemini(ctx, pat)
	if err != nil {
		return proposalCandidate{}, err
	}
	if gen.Priority <= 0 {
		gen.Priority = 100
	}
	actionType := pat.ActionType
	return proposalCandidate{
		OrgID:               orgID,
		ProposedName:        gen.Name,
		ProposedDescription: gen.Description,
		ProposedExpression:  gen.Expression,
		ProposedEffect:      gen.Effect,
		ProposedActionType:  &actionType,
		ProposedPriority:    gen.Priority,
		PatternSummary:      gen.Summary,
		Confidence:          pat.ApprovalRate,
		SampleSize:          pat.Total,
		TimeWindow:          "all",
	}, nil
}

func (p *Proposer) askGemini(ctx context.Context, pat pattern) (llmFields, error) {
	if p.llm == nil {
		return llmFields{}, fmt.Errorf("Gemini provider is required")
	}
	userMsg := fmt.Sprintf(
		"Patrón detectado:\n- action_type: %s\n- aprobadas: %d de %d (%.1f%%)\n\nGenerá una propuesta de política CEL.",
		pat.ActionType, pat.Approved, pat.Total, pat.ApprovalRate*100,
	)
	resp, err := p.llm.Chat(ctx, coreai.ChatRequest{
		SystemPrompt: proposerSystemPrompt,
		Messages:     []coreai.Message{{Role: "user", Content: userMsg}},
		MaxTokens:    defaultMaxTokens,
	})
	if err != nil || resp.Text == "" {
		if err != nil {
			return llmFields{}, fmt.Errorf("Gemini proposer: %w", err)
		}
		return llmFields{}, fmt.Errorf("Gemini proposer returned empty response")
	}
	var fields llmFields
	if err := json.Unmarshal([]byte(resp.Text), &fields); err != nil {
		slog.Warn("governance-assist proposer: Gemini returned non-JSON", "err", err)
		return llmFields{}, fmt.Errorf("Gemini proposer returned non-JSON: %w", err)
	}
	if fields.Name == "" || fields.Description == "" || fields.Expression == "" || fields.Effect == "" || fields.Summary == "" {
		return llmFields{}, fmt.Errorf("Gemini proposer returned incomplete policy proposal")
	}
	return fields, nil
}

func (p *Proposer) submitToNexus(ctx context.Context, candidate proposalCandidate) error {
	st, raw, err := p.governance.SubmitProposal(ctx, candidate)
	if err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("status %d: %s", st, governanceclient.ParseErrorBody(raw))
	}
	return nil
}

const proposerSystemPrompt = `Sos un experto en gobernanza. Analizás patrones de aprobación históricos y proponés políticas CEL para automatizar decisiones repetitivas.

Respondé SOLO con un JSON válido con esta estructura:
{
  "name": "nombre-kebab-case de la política",
  "description": "descripción concisa en inglés",
  "expression": "expresión CEL válida (ej: request.action_type == 'deploy')",
  "effect": "allow | deny | require_approval",
  "summary": "resumen en español del patrón y por qué se recomienda esta política",
  "priority": 100
}

Reglas:
- La expresión CEL debe usar variables del namespace request (action_type, target_system, requester_type) o time (hour, day_of_week).
- Si la tasa de aprobación es ≥ 95%, recomendar effect "allow".
- Si es entre 80-95%, "allow" con expresión más restrictiva (ej: agregar horario o target_system).
- Si es < 80%, "require_approval".
- priority: 100 por defecto, menor para políticas más específicas.`
