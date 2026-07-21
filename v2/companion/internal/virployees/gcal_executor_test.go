package virployees

import (
	"context"
	"errors"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

type fakeCalendarAPI struct {
	gotCalendarID  string
	gotIdemKey     string
	gotEvent       CalendarEvent
	result         CalendarInsertResult
	err            error
	calls          int
	deletedEventID string
	deleteCalls    int
	deleteErr      error
}

func (f *fakeCalendarAPI) InsertEvent(_ context.Context, calendarID, idempotencyKey string, event CalendarEvent) (CalendarInsertResult, error) {
	f.calls++
	f.gotCalendarID = calendarID
	f.gotIdemKey = idempotencyKey
	f.gotEvent = event
	return f.result, f.err
}

func (f *fakeCalendarAPI) DeleteEvent(_ context.Context, _ string, eventID string) error {
	f.deleteCalls++
	f.deletedEventID = eventID
	return f.deleteErr
}

func gcalAction() preparedactions.Action {
	return preparedactions.Action{
		SchemaVersion:   preparedactions.SchemaVersion,
		Action:          preparedactions.ActionCreate,
		Title:           "Q3 sync",
		Date:            "2026-08-01",
		Time:            "15:00",
		Timezone:        "America/Argentina/Buenos_Aires",
		DurationMinutes: 30,
		Attendees:       []string{"ana@example.com"},
	}
}

func TestGoogleCalendarExecutorInsertsAndReportsExternalEffects(t *testing.T) {
	api := &fakeCalendarAPI{result: CalendarInsertResult{EventID: "evt-123", HTMLLink: "https://calendar.google.com/event?eid=evt-123"}}
	exec := NewGoogleCalendarExecutor(api, "team@group.calendar.google.com")
	attempt := ExecutionAttempt{ID: uuid.New(), IdempotencyKey: "idem-abc"}

	outcome, err := exec.Execute(context.Background(), "tenant-1", uuid.New(), attempt, gcalAction())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Mode != "google_calendar" || !outcome.ExternalEffects {
		t.Fatalf("expected google_calendar mode with external effects, got %+v", outcome)
	}
	if outcome.ResourceID != "evt-123" {
		t.Fatalf("expected event id passthrough, got %q", outcome.ResourceID)
	}
	// G3.3: the idempotency key and calendar id must reach the API for dedupe.
	if api.gotIdemKey != "idem-abc" {
		t.Fatalf("executor must forward the idempotency key, got %q", api.gotIdemKey)
	}
	if api.gotCalendarID != "team@group.calendar.google.com" {
		t.Fatalf("executor must target the configured calendar, got %q", api.gotCalendarID)
	}
	if api.gotEvent.Title != "Q3 sync" || len(api.gotEvent.Attendees) != 1 {
		t.Fatalf("event fields not mapped: %+v", api.gotEvent)
	}
	// G3.2: the persisted result carries only non-sensitive presentational fields.
	allowed := map[string]bool{"mode": true, "operation": true, "resource_id": true, "resource_type": true, "html_link": true, "idempotent_replay": true}
	for k := range outcome.Result {
		if !allowed[k] {
			t.Fatalf("unexpected key %q in execution result — possible credential leak", k)
		}
	}
}

func TestGoogleCalendarExecutorRecordsModeEvenOnError(t *testing.T) {
	// Missing calendar id: still fails closed, but records the responsible executor.
	exec := NewGoogleCalendarExecutor(&fakeCalendarAPI{}, "")
	outcome, err := exec.Execute(context.Background(), "tenant-1", uuid.New(), ExecutionAttempt{IdempotencyKey: "k"}, gcalAction())
	if err == nil {
		t.Fatal("expected an error when calendar id is not configured")
	}
	if outcome.Mode != "google_calendar" || !outcome.ExternalEffects {
		t.Fatalf("a failed attempt must still record mode/external-effects, got %+v", outcome)
	}
}

func TestGoogleCalendarExecutorPropagatesAPIError(t *testing.T) {
	api := &fakeCalendarAPI{err: errors.New("boom")}
	exec := NewGoogleCalendarExecutor(api, "cal-1")
	_, err := exec.Execute(context.Background(), "tenant-1", uuid.New(), ExecutionAttempt{IdempotencyKey: "k"}, gcalAction())
	if err == nil {
		t.Fatal("expected the API error to propagate")
	}
}

func TestGoogleCalendarExecutorDeletesForCompensation(t *testing.T) {
	api := &fakeCalendarAPI{}
	exec := NewGoogleCalendarExecutor(api, "cal-1")
	action := preparedactions.Action{
		SchemaVersion: preparedactions.DeleteSchemaVersion,
		Action:        preparedactions.ActionDelete,
		EventID:       "evt-123",
	}
	outcome, err := exec.Execute(context.Background(), "tenant-1", uuid.New(), ExecutionAttempt{IdempotencyKey: "k"}, action)
	if err != nil {
		t.Fatalf("Execute(delete): %v", err)
	}
	if api.deleteCalls != 1 || api.deletedEventID != "evt-123" {
		t.Fatalf("expected DeleteEvent for evt-123, got calls=%d id=%q", api.deleteCalls, api.deletedEventID)
	}
	if outcome.Mode != "google_calendar" || !outcome.ExternalEffects || outcome.ResourceID != "evt-123" {
		t.Fatalf("unexpected delete outcome: %+v", outcome)
	}
	if outcome.Result["operation"] != "delete" {
		t.Fatalf("delete outcome must record operation=delete, got %v", outcome.Result["operation"])
	}
}

func TestGoogleCalendarExecutorDeleteRequiresEventID(t *testing.T) {
	exec := NewGoogleCalendarExecutor(&fakeCalendarAPI{}, "cal-1")
	action := preparedactions.Action{Action: preparedactions.ActionDelete}
	if _, err := exec.Execute(context.Background(), "t", uuid.New(), ExecutionAttempt{}, action); err == nil {
		t.Fatal("expected an error when the delete has no event id")
	}
}
