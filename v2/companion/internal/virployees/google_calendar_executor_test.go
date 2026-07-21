package virployees

import (
	"context"
	"errors"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

type fakeCalendarClient struct {
	calls            int
	gotCalendarID    string
	gotDeterministic string
	gotEvent         CalendarEvent
	eventID          string
	htmlLink         string
	err              error
}

func (f *fakeCalendarClient) CreateEvent(_ context.Context, calendarID, deterministicID string, ev CalendarEvent) (string, string, error) {
	f.calls++
	f.gotCalendarID = calendarID
	f.gotDeterministic = deterministicID
	f.gotEvent = ev
	if f.err != nil {
		return "", "", f.err
	}
	return f.eventID, f.htmlLink, nil
}

func sampleAction() preparedactions.Action {
	return preparedactions.Action{
		SchemaVersion:   preparedactions.SchemaVersion,
		Action:          preparedactions.ActionCreate,
		Title:           "Reunión Q3",
		Date:            "2026-07-25",
		Time:            "15:00",
		Timezone:        "America/Argentina/Buenos_Aires",
		DurationMinutes: 60,
		Attendees:       []string{"ana@example.com"},
	}
}

func TestGoogleCalendarExecutorCreatesEvent(t *testing.T) {
	client := &fakeCalendarClient{eventID: "evt-123", htmlLink: "https://cal/evt-123"}
	exec := NewGoogleCalendarExecutor(client, "primary")

	if exec.Mode() != "google_calendar" || !exec.ExternalEffects() {
		t.Fatalf("expected google_calendar mode with external effects, got %q/%v", exec.Mode(), exec.ExternalEffects())
	}

	attempt := ExecutionAttempt{IdempotencyKey: "idem-test-key"}
	resourceID, result, err := exec.Execute(context.Background(), "tenant-1", uuid.New(), attempt, sampleAction())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resourceID != "evt-123" {
		t.Fatalf("expected the created event id, got %q", resourceID)
	}
	if client.calls != 1 {
		t.Fatalf("expected one insert, got %d", client.calls)
	}
	// Idempotency: the deterministic id passed to Google IS the attempt's key, so
	// a retry lands on the same event.
	if client.gotDeterministic != "idem-test-key" {
		t.Fatalf("expected the idempotency key as the event id, got %q", client.gotDeterministic)
	}
	if client.gotCalendarID != "primary" {
		t.Fatalf("expected the configured calendar, got %q", client.gotCalendarID)
	}
	if client.gotEvent.Summary != "Reunión Q3" || len(client.gotEvent.Attendees) != 1 {
		t.Fatalf("unexpected event built: %+v", client.gotEvent)
	}
	// End must be start + duration.
	if got := client.gotEvent.End.Sub(client.gotEvent.Start).Minutes(); got != 60 {
		t.Fatalf("expected a 60-minute event, got %v", got)
	}
	if result["mode"] != "google_calendar" || result["html_link"] != "https://cal/evt-123" {
		t.Fatalf("unexpected result metadata: %+v", result)
	}
}

func TestGoogleCalendarExecutorDefaultsCalendarID(t *testing.T) {
	exec := NewGoogleCalendarExecutor(&fakeCalendarClient{eventID: "e"}, "")
	if exec.calendarID != "primary" {
		t.Fatalf("expected empty calendar id to default to primary, got %q", exec.calendarID)
	}
}

func TestGoogleCalendarExecutorPropagatesError(t *testing.T) {
	client := &fakeCalendarClient{err: errors.New("calendar api down")}
	exec := NewGoogleCalendarExecutor(client, "primary")
	if _, _, err := exec.Execute(context.Background(), "t", uuid.New(), ExecutionAttempt{IdempotencyKey: "k"}, sampleAction()); err == nil {
		t.Fatal("expected the calendar error to propagate (so the attempt is marked failed)")
	}
}

func TestGoogleCalendarExecutorRejectsBadTime(t *testing.T) {
	client := &fakeCalendarClient{eventID: "e"}
	exec := NewGoogleCalendarExecutor(client, "primary")
	action := sampleAction()
	action.Timezone = "Not/AZone"
	if _, _, err := exec.Execute(context.Background(), "t", uuid.New(), ExecutionAttempt{IdempotencyKey: "k"}, action); err == nil {
		t.Fatal("expected an error for an unresolvable start time")
	}
	if client.calls != 0 {
		t.Fatal("must not call the calendar API when the time cannot be resolved")
	}
}
