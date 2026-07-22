package governancepolicies

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvaluatorDenyPrecedesAllowRegardlessOfPriority(t *testing.T) {
	evaluator := NewEvaluator(nil)
	versions := []Version{
		policyVersion(EffectAllow, StateActive, 1, "true"),
		policyVersion(EffectDeny, StateActive, 900, "true"),
	}
	result, err := evaluator.Evaluate(context.Background(), "organization-1", versions, safeInput("medium"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Decision != EffectDeny || !result.Matched {
		t.Fatalf("deny must prevail: %+v", result)
	}
}

func TestEvaluatorShadowRecordsWithoutChangingDecision(t *testing.T) {
	recorder := &evaluationRecorder{}
	evaluator := NewEvaluator(recorder)
	emptySnapshot, err := evaluator.Evaluate(context.Background(), "organization-1", nil, safeInput("low"))
	if err != nil {
		t.Fatalf("Evaluate empty policy set: %v", err)
	}
	result, err := evaluator.Evaluate(context.Background(), "organization-1", []Version{policyVersion(EffectDeny, StateShadow, 1, "true")}, safeInput("low"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Matched || result.Decision != "" || len(result.Matches) != 1 {
		t.Fatalf("shadow changed the enforced result: %+v", result)
	}
	if len(recorder.items) != 1 || recorder.items[0].Mode != "shadow" || !recorder.items[0].Matched {
		t.Fatalf("shadow evaluation not recorded: %+v", recorder.items)
	}
	if result.PolicySnapshotHash != emptySnapshot.PolicySnapshotHash {
		t.Fatalf("shadow policy changed the enforced authority snapshot: %q != %q", result.PolicySnapshotHash, emptySnapshot.PolicySnapshotHash)
	}
}

func TestEvaluatorCannotLowerRiskOrBypassHighRiskApproval(t *testing.T) {
	version := policyVersion(EffectAllow, StateActive, 1, "true")
	version.RiskOverride = "low"
	result, err := NewEvaluator(nil).Evaluate(context.Background(), "organization-1", []Version{version}, safeInput("high"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.EffectiveRisk != "high" || result.Decision != EffectRequireApproval {
		t.Fatalf("risk was lowered or approval bypassed: %+v", result)
	}
}

func TestEvaluatorEnforcedCELFailureCloses(t *testing.T) {
	version := policyVersion(EffectAllow, StateActive, 1, `resource.missing.startsWith("x")`)
	if _, err := NewEvaluator(nil).Evaluate(context.Background(), "organization-1", []Version{version}, safeInput("low")); err == nil {
		t.Fatal("expected enforced CEL evaluation to fail closed")
	}
}

func TestEvaluatorRejectsInvalidCEL(t *testing.T) {
	if err := NewEvaluator(nil).Validate("action.type =="); err == nil {
		t.Fatal("expected invalid CEL to be rejected")
	}
}

func policyVersion(effect, state string, priority int, expression string) Version {
	return Version{ID: uuid.New(), PolicyID: uuid.New(), Version: 1, State: state, ActionTypePattern: "*", Expression: expression,
		Effect: effect, Priority: priority, ContentHash: uuid.NewString()}
}

func safeInput(risk string) SafeInput {
	return SafeInput{ActionType: "calendar.events.delete", TargetSystem: "calendar", ResourceType: "event", ResourceReference: "event-1",
		RiskClass: risk, RequesterType: "virployee", RequesterID: "virployee-1", AuthorityHashes: map[string]string{}, Now: time.Now().UTC()}
}

type evaluationRecorder struct{ items []Evaluation }

func (r *evaluationRecorder) RecordEvaluation(_ context.Context, item Evaluation) error {
	r.items = append(r.items, item)
	return nil
}
