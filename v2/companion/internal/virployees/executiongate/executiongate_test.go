package executiongate

import (
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

func TestEvaluateBlocksWithoutIntent(t *testing.T) {
	result := Evaluate(dryrun.Result{
		Input:             "hola",
		Decision:          dryrun.DecisionAllowed,
		VirployeeAutonomy: virployeedomain.AutonomyA2,
		Draft:             dryrun.Draft{Status: dryrun.DraftStatusNotApplicable},
	})

	if result.Gate.Decision != DecisionBlocked {
		t.Fatalf("expected blocked, got %+v", result.Gate)
	}
	assertCheck(t, result.Gate.Checks, "intent_matched", CheckStatusBlocked)
	if result.Gate.WillExecute {
		t.Fatal("gate must never execute")
	}
}

func TestEvaluateBlocksCreateWithIncompleteDraft(t *testing.T) {
	result := Evaluate(readyDryRun("calendar.events.create", virployeedomain.AutonomyA3, dryrun.DraftStatusNeedsInput))

	if result.Gate.Decision != DecisionBlocked {
		t.Fatalf("expected blocked, got %+v", result.Gate)
	}
	if result.Gate.RequiredExecutionAutonomy != virployeedomain.AutonomyA3 {
		t.Fatalf("expected A3 execution autonomy, got %s", result.Gate.RequiredExecutionAutonomy)
	}
	assertCheck(t, result.Gate.Checks, "draft_ready", CheckStatusBlocked)
}

func TestEvaluateBlocksCreateBelowExecutionAutonomy(t *testing.T) {
	result := Evaluate(readyDryRun("calendar.events.create", virployeedomain.AutonomyA2, dryrun.DraftStatusReady))

	if result.Gate.Decision != DecisionBlocked {
		t.Fatalf("expected blocked, got %+v", result.Gate)
	}
	assertCheck(t, result.Gate.Checks, "execution_autonomy", CheckStatusBlocked)
}

func TestEvaluatePassesCreateAtExecutionAutonomy(t *testing.T) {
	result := Evaluate(readyDryRun("calendar.events.create", virployeedomain.AutonomyA3, dryrun.DraftStatusReady))

	if result.Gate.Decision != DecisionPass {
		t.Fatalf("expected pass, got %+v", result.Gate)
	}
	if result.Gate.WillExecute {
		t.Fatal("gate must never execute")
	}
	assertCheck(t, result.Gate.Checks, "execution_autonomy", CheckStatusPass)
}

func TestEvaluatePassesReadAtRecommendationAutonomy(t *testing.T) {
	result := Evaluate(readyDryRun("calendar.events.read", virployeedomain.AutonomyA1, dryrun.DraftStatusReady))

	if result.Gate.Decision != DecisionPass {
		t.Fatalf("expected pass, got %+v", result.Gate)
	}
	if result.Gate.RequiredExecutionAutonomy != virployeedomain.AutonomyA1 {
		t.Fatalf("expected A1 execution autonomy, got %s", result.Gate.RequiredExecutionAutonomy)
	}
}

func TestApplyConfirmedDraftMarksCompleteCalendarDraftReady(t *testing.T) {
	result, err := ApplyConfirmedDraft(readyDryRun("calendar.events.create", virployeedomain.AutonomyA2, dryrun.DraftStatusNeedsInput), ConfirmedDraft{
		Action: "calendar.events.create",
		Kind:   "calendar_event",
		Fields: []ConfirmedDraftField{
			{Key: "title", Value: "Reunión"},
			{Key: "date_hint", Value: "mañana"},
			{Key: "time", Value: "15:00"},
			{Key: "attendees", Value: "ana@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyConfirmedDraft: %v", err)
	}
	if result.Draft.Status != dryrun.DraftStatusReady || len(result.Draft.MissingFields) != 0 {
		t.Fatalf("expected ready confirmed draft, got %+v", result.Draft)
	}
	if len(result.Draft.Fields) != 4 || result.Draft.Fields[0].Source != "confirmed" {
		t.Fatalf("unexpected confirmed fields: %+v", result.Draft.Fields)
	}
}

func TestApplyConfirmedDraftKeepsIncompleteCalendarDraftNeedsInput(t *testing.T) {
	result, err := ApplyConfirmedDraft(readyDryRun("calendar.events.create", virployeedomain.AutonomyA2, dryrun.DraftStatusNeedsInput), ConfirmedDraft{
		Action: "calendar.events.create",
		Kind:   "calendar_event",
		Fields: []ConfirmedDraftField{
			{Key: "title", Value: "Reunión"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyConfirmedDraft: %v", err)
	}
	if result.Draft.Status != dryrun.DraftStatusNeedsInput || len(result.Draft.MissingFields) != 3 {
		t.Fatalf("expected incomplete confirmed draft, got %+v", result.Draft)
	}
}

func TestApplyConfirmedDraftRejectsActionMismatch(t *testing.T) {
	_, err := ApplyConfirmedDraft(readyDryRun("calendar.events.create", virployeedomain.AutonomyA2, dryrun.DraftStatusReady), ConfirmedDraft{
		Action: "calendar.events.read",
	})
	if err == nil {
		t.Fatal("expected action mismatch error")
	}
}

func readyDryRun(capabilityKey string, virployeeAutonomy virployeedomain.AutonomyLevel, draftStatus dryrun.DraftStatus) dryrun.Result {
	return dryrun.Result{
		Input: "test input",
		Intent: dryrun.Intent{
			Matched:       true,
			CapabilityKey: capabilityKey,
		},
		RequiredCapability: &dryrun.RequiredCapability{
			CapabilityKey: capabilityKey,
			Matched:       true,
		},
		VirployeeAutonomy: virployeeAutonomy,
		Decision:          dryrun.DecisionAllowed,
		Reason:            "allowed",
		Draft:             dryrun.Draft{Status: draftStatus},
	}
}

func assertCheck(t *testing.T, checks []Check, key string, status CheckStatus) {
	t.Helper()
	for _, check := range checks {
		if check.Key == key {
			if check.Status != status {
				t.Fatalf("expected %s check %s, got %+v", key, status, check)
			}
			return
		}
	}
	t.Fatalf("missing check %s in %+v", key, checks)
}
