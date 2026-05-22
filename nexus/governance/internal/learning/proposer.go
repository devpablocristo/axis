package learning

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	learningdomain "github.com/devpablocristo/nexus/governance/internal/learning/usecases/domain"
)

// PolicyProposer genera propuestas de políticas a partir de patrones detectados.
//
// Nexus es AI-independent por contrato arquitectónico: sólo dispone de un
// proposer determinístico (StubProposer) que arma propuestas a partir de
// templates. La asistencia con LLM vive en Companion, que detecta patrones,
// genera la propuesta enriquecida y la POSTea a /v1/learning/proposals.
type PolicyProposer interface {
	GenerateProposal(ctx context.Context, pattern Pattern) (*learningdomain.PolicyProposal, error)
}

// StubProposer genera propuestas sin usar IA (basado en templates).
type StubProposer struct{}

func NewStubProposer() *StubProposer {
	return &StubProposer{}
}

func (s *StubProposer) GenerateProposal(_ context.Context, pattern Pattern) (*learningdomain.PolicyProposal, error) {
	return buildProposal(pattern, stubGenerate(pattern)), nil
}

// generatedFields contiene los campos de una propuesta antes de envolverla en
// PolicyProposal. Lo deja como struct por si en el futuro se agregan más
// generators determinísticos (ej. policy-pack import).
type generatedFields struct {
	Name        string
	Description string
	Expression  string
	Effect      string
	Summary     string
	Priority    int
}

func stubGenerate(pattern Pattern) generatedFields {
	return generatedFields{
		Name: fmt.Sprintf("auto-approve-%s", pattern.ActionType),
		Description: fmt.Sprintf(
			"Auto-approve %s — historically approved %.0f%% of the time (%d/%d)",
			pattern.ActionType, pattern.ApprovalRate*100, pattern.Approved, pattern.Total,
		),
		Expression: fmt.Sprintf("request.action_type == '%s'", pattern.ActionType),
		Effect:     "allow",
		Summary: fmt.Sprintf(
			"En los últimos %s, %.0f%% de las requests '%s' fueron aprobadas (%d de %d).",
			pattern.TimeWindow, pattern.ApprovalRate*100, pattern.ActionType, pattern.Approved, pattern.Total,
		),
		Priority: 100,
	}
}

func buildProposal(pattern Pattern, gen generatedFields) *learningdomain.PolicyProposal {
	return &learningdomain.PolicyProposal{
		ID:                  uuid.New(),
		ProposedName:        gen.Name,
		ProposedDescription: gen.Description,
		ProposedExpression:  gen.Expression,
		ProposedEffect:      gen.Effect,
		ProposedActionType:  &pattern.ActionType,
		ProposedPriority:    gen.Priority,
		PatternSummary:      gen.Summary,
		Confidence:          pattern.ApprovalRate,
		SampleSize:          pattern.Total,
		TimeWindow:          pattern.TimeWindow,
		Status:              learningdomain.ProposalStatusPending,
		CreatedAt:           time.Now().UTC(),
	}
}
