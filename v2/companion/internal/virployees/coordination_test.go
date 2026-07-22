package virployees

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type scriptedCoordinationAnswerer struct {
	outputs []AnswerOutput
	calls   []AnswerInput
}

func (s *scriptedCoordinationAnswerer) Answer(_ context.Context, in AnswerInput) (AnswerOutput, error) {
	s.calls = append(s.calls, in)
	out := s.outputs[0]
	s.outputs = s.outputs[1:]
	return out, nil
}

func TestOrchestrationDecisionSchemaExposesCodesButNeverTargetIDs(t *testing.T) {
	targetID := uuid.New()
	schema := orchestrationDecisionSchema(map[string]any{"type": "object"}, []SpecialistRoute{{SpecialtyCode: "clinical.cardiology", TargetVirployeeID: targetID}})
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "clinical.cardiology") {
		t.Fatalf("schema must expose the allowlisted specialty code: %s", raw)
	}
	if strings.Contains(string(raw), targetID.String()) {
		t.Fatalf("schema leaked a target virployee id: %s", raw)
	}

	withoutRoutesSchema := orchestrationDecisionSchema(map[string]any{"type": "object"}, nil)
	withoutRoutes, _ := json.Marshal(withoutRoutesSchema)
	if strings.Contains(string(withoutRoutes), `"consult"`) {
		t.Fatalf("consult must not be selectable without an allowlisted route: %s", withoutRoutes)
	}
	properties := withoutRoutesSchema["properties"].(map[string]any)
	if _, exists := properties["consultations"]; exists {
		t.Fatalf("consultations schema must be omitted when no routes are allowlisted: %s", withoutRoutes)
	}
}

func TestAnswerDecisionRepairsOneInvalidStructuredResult(t *testing.T) {
	answerer := &scriptedCoordinationAnswerer{outputs: []AnswerOutput{
		{Answered: true, OutputJSON: json.RawMessage(`{"decision":"consult","consultations":[]}`)},
		{Answered: true, OutputJSON: json.RawMessage(`{"decision":"needs_human","escalation":{"reason_code":"clinical_uncertainty","urgency":"routine"}}`)},
	}}
	usecases := &UseCases{answerer: answerer}
	_, decision, err := usecases.answerDecisionWithRepair(context.Background(), AnswerInput{InputJSON: json.RawMessage(`{"case":"opaque"}`)}, 3)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if decision.Decision != "needs_human" || len(answerer.calls) != 2 {
		t.Fatalf("unexpected repaired decision=%+v calls=%d", decision, len(answerer.calls))
	}
	if !strings.Contains(string(answerer.calls[1].InputJSON), "schema_repair_instruction") {
		t.Fatalf("second call must carry a bounded repair instruction: %s", answerer.calls[1].InputJSON)
	}
}

func TestValidDecisionEnforcesBoundedFanoutAndEscalationReason(t *testing.T) {
	consultations := []ConsultationProposal{{SpecialtyCode: "a"}, {SpecialtyCode: "b"}}
	if validDecision(OrchestrationDecision{Decision: "consult", Consultations: consultations}, 1) {
		t.Fatal("fan-out above the policy maximum must be rejected")
	}
	if validDecision(OrchestrationDecision{Decision: "needs_human", Escalation: &EscalationProposal{}}, 3) {
		t.Fatal("human escalation without a reason code must be rejected")
	}
}

func TestRecordLLMUsageKeepsEachOrchestrationStageIdempotent(t *testing.T) {
	ledger := &fakeUsageLedger{}
	usecases := &UseCases{usageLedger: ledger}
	run := AssistRun{ID: uuid.New(), OrgID: "organization-a", ProductSurface: "producta"}
	output := AnswerOutput{InputTokens: 10, OutputTokens: 5, ModelID: "test-model"}

	usecases.recordLLMUsage(context.Background(), run, "selector", output)
	usecases.recordLLMUsage(context.Background(), run, "consult:"+uuid.NewString(), output)
	usecases.recordLLMUsage(context.Background(), run, "synthesis:"+uuid.NewString(), output)

	if len(ledger.records) != 3 {
		t.Fatalf("expected one usage record per LLM stage, got %d", len(ledger.records))
	}
	seen := map[string]bool{}
	for _, record := range ledger.records {
		if seen[record.IdempotencyKey] {
			t.Fatalf("duplicate stage idempotency key: %s", record.IdempotencyKey)
		}
		seen[record.IdempotencyKey] = true
	}
}

func TestHandoffCreationIsScopedToTheCurrentOwnerSupervisor(t *testing.T) {
	if !canCreateHandoff(CoordinationActor{ID: "supervisor-a", Role: "supervisor"}, "supervisor-a") {
		t.Fatal("the current owner's supervisor must be allowed to create a handoff")
	}
	if canCreateHandoff(CoordinationActor{ID: "supervisor-b", Role: "supervisor"}, "supervisor-a") {
		t.Fatal("an unrelated supervisor must not create a handoff")
	}
	if !canCreateHandoff(CoordinationActor{ID: "owner-a", Role: "owner"}, "supervisor-a") {
		t.Fatal("organization owner must be allowed to create a handoff")
	}
	if canCreateHandoff(CoordinationActor{ID: "service:producta", Role: "service"}, "supervisor-a") {
		t.Fatal("service principals must never create handoffs")
	}
}
