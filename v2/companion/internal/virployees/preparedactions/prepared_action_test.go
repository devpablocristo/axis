package preparedactions

import (
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
)

func TestFromDraftNormalizesExecutableCalendarAction(t *testing.T) {
	action, err := FromDraft(dryrun.Draft{Action: ActionCreate, Fields: []dryrun.DraftField{
		{Key: "title", Value: " Planning "},
		{Key: "date", Value: "2026-07-12"},
		{Key: "time", Value: "15:30"},
		{Key: "timezone", Value: "America/Argentina/Buenos_Aires"},
		{Key: "duration_minutes", Value: "45"},
		{Key: "attendees", Value: "B@example.com, a@example.com; a@example.com"},
	}})
	if err != nil {
		t.Fatalf("FromDraft: %v", err)
	}
	if action.Title != "Planning" || action.DurationMinutes != 45 || len(action.Attendees) != 2 || action.Attendees[0] != "a@example.com" {
		t.Fatalf("unexpected normalized action: %+v", action)
	}
	if _, err := action.StartsAt(); err != nil {
		t.Fatalf("StartsAt: %v", err)
	}
}

func TestPayloadHashChangesWithApprovedFields(t *testing.T) {
	base := Action{SchemaVersion: SchemaVersion, Action: ActionCreate, Title: "Planning", Date: "2026-07-12", Time: "15:30", Timezone: "UTC", DurationMinutes: 60, Attendees: []string{"a@example.com"}}
	first, _ := base.PayloadHash()
	base.Time = "16:30"
	second, _ := base.PayloadHash()
	if first == second {
		t.Fatal("expected approved field change to alter payload hash")
	}
}

func TestFromDraftRejectsAmbiguousSchedule(t *testing.T) {
	_, err := FromDraft(dryrun.Draft{Action: ActionCreate, Fields: []dryrun.DraftField{
		{Key: "title", Value: "Planning"}, {Key: "date", Value: "tomorrow"}, {Key: "time", Value: "15:00"},
		{Key: "timezone", Value: "UTC"}, {Key: "duration_minutes", Value: "60"}, {Key: "attendees", Value: "a@example.com"},
	}})
	if err == nil {
		t.Fatal("expected ambiguous date to be rejected")
	}
}
