package dryrun

import (
	"strings"
	"testing"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/google/uuid"
)

func TestEvaluateAllowsCalendarCreateWhenCapabilityIsAssigned(t *testing.T) {
	capabilityID := uuid.New()
	result := Evaluate("Agendá una reunión para mañana", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA2,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               capabilityID,
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: virployeedomain.AutonomyA2,
			},
		},
	})

	if result.Decision != DecisionAllowed {
		t.Fatalf("expected allowed, got %+v", result)
	}
	if result.RequiredCapability == nil || result.RequiredCapability.ID != capabilityID.String() || !result.RequiredCapability.Matched {
		t.Fatalf("unexpected required capability: %+v", result.RequiredCapability)
	}
	if result.RequiredAutonomy != virployeedomain.AutonomyA2 {
		t.Fatalf("expected A2, got %s", result.RequiredAutonomy)
	}
	if !result.Intent.Matched || result.Intent.CapabilityKey != "calendar.events.create" || result.Intent.Confidence != 0.9 {
		t.Fatalf("unexpected intent: %+v", result.Intent)
	}
	if got, want := strings.Join(result.Intent.MatchedBy, ","), "resource:reunion,action:agenda"; got != want {
		t.Fatalf("expected matched_by %s, got %s", want, got)
	}
	if result.Draft.Status != DraftStatusNeedsInput || result.Draft.Action != "calendar.events.create" || result.Draft.Kind != "calendar_event" {
		t.Fatalf("unexpected draft envelope: %+v", result.Draft)
	}
	if !hasDraftField(result.Draft, "title", "Reunión") || !hasDraftField(result.Draft, "date_hint", "mañana") {
		t.Fatalf("expected inferred title and date hint, got %+v", result.Draft.Fields)
	}
	if !hasMissingField(result.Draft, "time") || !hasMissingField(result.Draft, "attendees") {
		t.Fatalf("expected missing time and attendees, got %+v", result.Draft.MissingFields)
	}
}

func TestEvaluateCalendarCreateDraftReadyWhenRequiredFieldsArePresent(t *testing.T) {
	result := Evaluate(`Agendá una reunión "Demo Axis" mañana a las 15 con ana@example.com`, runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA2,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               uuid.New(),
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: virployeedomain.AutonomyA2,
			},
		},
	})

	if result.Decision != DecisionAllowed || result.Draft.Status != DraftStatusReady {
		t.Fatalf("expected ready allowed draft, got %+v", result)
	}
	if !hasDraftField(result.Draft, "title", "Demo Axis") || !hasDraftField(result.Draft, "attendees", "ana@example.com") {
		t.Fatalf("unexpected draft fields: %+v", result.Draft.Fields)
	}
	if len(result.Draft.MissingFields) != 0 {
		t.Fatalf("expected no missing fields, got %+v", result.Draft.MissingFields)
	}
}

func TestEvaluateCalendarReadIntent(t *testing.T) {
	result := Evaluate("Qué reuniones tengo mañana", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA1,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               uuid.New(),
				CapabilityKey:    "calendar.events.read",
				Name:             "Read calendar events",
				RequiredAutonomy: virployeedomain.AutonomyA1,
			},
		},
	})

	if result.Decision != DecisionAllowed || !result.Intent.Matched || result.Intent.CapabilityKey != "calendar.events.read" {
		t.Fatalf("expected read intent, got %+v", result)
	}
	if result.Draft.Kind != "calendar_event_query" || result.Draft.Status != DraftStatusReady {
		t.Fatalf("unexpected read draft: %+v", result.Draft)
	}
}

func TestEvaluateCalendarUpdateIntent(t *testing.T) {
	result := Evaluate("Reprogramá la reunión de mañana", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA2,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               uuid.New(),
				CapabilityKey:    "calendar.events.update",
				Name:             "Update calendar events",
				RequiredAutonomy: virployeedomain.AutonomyA2,
			},
		},
	})

	if result.Decision != DecisionAllowed || !result.Intent.Matched || result.Intent.CapabilityKey != "calendar.events.update" {
		t.Fatalf("expected update intent, got %+v", result)
	}
	if result.Draft.Kind != "calendar_event_update" || !hasMissingField(result.Draft, "event_reference") || !hasMissingField(result.Draft, "changes") {
		t.Fatalf("unexpected update draft: %+v", result.Draft)
	}
}

func TestEvaluateCalendarDeleteIntent(t *testing.T) {
	result := Evaluate("Cancelá la reunión de mañana", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA2,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               uuid.New(),
				CapabilityKey:    "calendar.events.delete",
				Name:             "Delete calendar events",
				RequiredAutonomy: virployeedomain.AutonomyA2,
			},
		},
	})

	if result.Decision != DecisionAllowed || !result.Intent.Matched || result.Intent.CapabilityKey != "calendar.events.delete" {
		t.Fatalf("expected delete intent, got %+v", result)
	}
	if result.Draft.Kind != "calendar_event_delete" || !hasMissingField(result.Draft, "event_reference") {
		t.Fatalf("unexpected delete draft: %+v", result.Draft)
	}
}

func TestEvaluateIgnoresUnassignedCapabilities(t *testing.T) {
	// Data-driven per tenant: with no capabilities assigned the catalog is
	// empty, so an action request is not recognized at all. The deterministic
	// matcher can never infer an intent for an unassigned capability.
	result := Evaluate("Agendá una reunión para mañana", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA2,
		},
	})

	if result.Intent.Matched {
		t.Fatalf("expected no intent for an unassigned capability, got %+v", result.Intent)
	}
	if result.RequiredCapability != nil {
		t.Fatalf("expected no required capability, got %+v", result.RequiredCapability)
	}
	if result.Decision != DecisionAllowed {
		t.Fatalf("expected conversational allowed, got %+v", result)
	}
	if result.Draft.Status != DraftStatusNotApplicable {
		t.Fatalf("expected not applicable draft, got %+v", result.Draft)
	}
}

func TestEvaluateNeverInfersUnassignedAction(t *testing.T) {
	// A virployee with only read assigned must never have a create intent
	// inferred, even from an explicit "agendá" request: the create capability
	// is invisible to the matcher unless assigned.
	result := Evaluate("Agendá una reunión para mañana", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA3,
		},
		Capabilities: []capabilitydomain.Capability{
			{
				ID:               uuid.New(),
				CapabilityKey:    "calendar.events.read",
				Name:             "Read calendar events",
				RequiredAutonomy: virployeedomain.AutonomyA1,
			},
		},
	})

	if result.Intent.CapabilityKey == "calendar.events.create" {
		t.Fatalf("create intent must not be inferred without the capability assigned, got %+v", result.Intent)
	}
	if result.RequiredCapability != nil && result.RequiredCapability.CapabilityKey == "calendar.events.create" {
		t.Fatalf("must not require an unassigned create capability, got %+v", result.RequiredCapability)
	}
}

func TestEvaluateConversationWhenNoCapabilityIsInferred(t *testing.T) {
	result := Evaluate("Hola, como estas?", runtimecontext.Context{
		Virployee: virployeedomain.Virployee{
			ID:       uuid.New(),
			Name:     "Sofia",
			Autonomy: virployeedomain.AutonomyA1,
		},
	})

	if result.Decision != DecisionAllowed {
		t.Fatalf("expected allowed, got %+v", result)
	}
	if result.RequiredCapability != nil {
		t.Fatalf("expected no required capability, got %+v", result.RequiredCapability)
	}
	if result.RequiredAutonomy != virployeedomain.AutonomyA0 {
		t.Fatalf("expected A0, got %s", result.RequiredAutonomy)
	}
	if result.Intent.Matched || result.Intent.Confidence != 0 {
		t.Fatalf("expected unmatched intent, got %+v", result.Intent)
	}
	if result.Draft.Status != DraftStatusNotApplicable {
		t.Fatalf("expected not applicable draft, got %+v", result.Draft)
	}
}

func hasDraftField(draft Draft, key string, value string) bool {
	for _, field := range draft.Fields {
		if field.Key == key && field.Value == value {
			return true
		}
	}
	return false
}

func hasMissingField(draft Draft, key string) bool {
	for _, field := range draft.MissingFields {
		if field.Key == key {
			return true
		}
	}
	return false
}
