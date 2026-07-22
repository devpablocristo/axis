package virployees

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
)

// CalendarEvent is the executor-facing shape of an event to create, decoupled
// from any SDK. The real Google adapter and the test fake both consume it.
type CalendarEvent struct {
	Title           string
	StartsAt        time.Time
	Timezone        string
	DurationMinutes int
	Attendees       []string
}

// CalendarInsertResult is what a calendar backend returns after inserting an event.
type CalendarInsertResult struct {
	EventID  string
	HTMLLink string
	// AlreadyExisted is true when the idempotency key matched an existing event,
	// so the insert was a safe retry (a no-op), not a duplicate.
	AlreadyExisted bool
}

// CalendarAPI is the narrow port the Google Calendar executor depends on.
// Implementations MUST use idempotencyKey to dedupe retries so a partial failure
// followed by a retry never creates two events (G3.3), MUST treat a delete of an
// already-gone event as success (idempotent compensation), and MUST never log or
// return credential material (G3.2).
type CalendarAPI interface {
	InsertEvent(ctx context.Context, calendarID, idempotencyKey string, event CalendarEvent) (CalendarInsertResult, error)
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
}

// GoogleCalendarExecutor creates real Google Calendar events for approved
// calendar.events.create actions. It is wired only when EXECUTION_MODE includes
// "google_calendar". The effects are real, so every outcome reports
// ExternalEffects=true — this is what flags the run trace as a real-world write.
type GoogleCalendarExecutor struct {
	api        CalendarAPI
	calendarID string
}

func NewGoogleCalendarExecutor(api CalendarAPI, calendarID string) *GoogleCalendarExecutor {
	return &GoogleCalendarExecutor{api: api, calendarID: calendarID}
}

func (e *GoogleCalendarExecutor) Execute(ctx context.Context, orgID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (ExecutionOutcome, error) {
	// Mode is set even on the error paths so a failed attempt still records which
	// executor was responsible (never left as an empty mode).
	outcome := ExecutionOutcome{Mode: "google_calendar", ExternalEffects: true}
	if e.api == nil {
		return outcome, fmt.Errorf("google calendar api is not configured")
	}
	if strings.TrimSpace(e.calendarID) == "" {
		return outcome, fmt.Errorf("google calendar id is not configured")
	}
	switch action.Action {
	case preparedactions.ActionCreate:
		return e.create(ctx, attempt, action, outcome)
	case preparedactions.ActionDelete:
		return e.delete(ctx, action, outcome)
	default:
		return outcome, fmt.Errorf("unsupported action for google calendar executor: %s", action.Action)
	}
}

func (e *GoogleCalendarExecutor) create(ctx context.Context, attempt ExecutionAttempt, action preparedactions.Action, outcome ExecutionOutcome) (ExecutionOutcome, error) {
	startsAt, err := action.StartsAt()
	if err != nil {
		return outcome, err
	}
	res, err := e.api.InsertEvent(ctx, e.calendarID, attempt.IdempotencyKey, CalendarEvent{
		Title:           action.Title,
		StartsAt:        startsAt,
		Timezone:        action.Timezone,
		DurationMinutes: action.DurationMinutes,
		Attendees:       action.Attendees,
	})
	if err != nil {
		return outcome, err
	}
	outcome.ResourceID = res.EventID
	// Only non-sensitive, presentational fields reach the persisted result/trace:
	// no credentials, no secret refs (G3.2).
	outcome.Result = map[string]any{
		"mode":              "google_calendar",
		"operation":         "create",
		"resource_id":       res.EventID,
		"resource_type":     "calendar_event",
		"html_link":         res.HTMLLink,
		"idempotent_replay": res.AlreadyExisted,
	}
	return outcome, nil
}

// delete is the compensating action (rollback). It runs through the same governed
// path as any other execution, carrying its own binding hash (G3.5), and is
// idempotent: deleting an already-gone event succeeds.
func (e *GoogleCalendarExecutor) delete(ctx context.Context, action preparedactions.Action, outcome ExecutionOutcome) (ExecutionOutcome, error) {
	eventID := strings.TrimSpace(action.EventID)
	if eventID == "" {
		return outcome, fmt.Errorf("event id is required to delete a calendar event")
	}
	if err := e.api.DeleteEvent(ctx, e.calendarID, eventID); err != nil {
		return outcome, err
	}
	outcome.ResourceID = eventID
	outcome.Result = map[string]any{
		"mode":          "google_calendar",
		"operation":     "delete",
		"resource_id":   eventID,
		"resource_type": "calendar_event",
	}
	return outcome, nil
}
