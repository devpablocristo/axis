package virployees

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/preparedactions"
	"github.com/google/uuid"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const (
	modeGoogleCalendar = "google_calendar"
	// externalCallTimeout bounds a single Google Calendar API call so a hung
	// backend cannot stall an execution indefinitely (the companion request ctx
	// carries no deadline of its own).
	externalCallTimeout = 20 * time.Second
	// defaultEventDurationMinutes matches the prepared-action validator's default
	// so an unset duration produces a consistent event length.
	defaultEventDurationMinutes = 60
)

// calendarEventCreator is the narrow slice of the Google Calendar API the
// executor needs. A fake stands in for it in tests, so the executor's logic is
// verifiable without credentials or network.
type calendarEventCreator interface {
	// CreateEvent inserts an event using deterministicID as its id so a retry is
	// idempotent — an already-existing LIVE event must be returned, not an error.
	// Returns the event id and its shareable link.
	CreateEvent(ctx context.Context, calendarID, deterministicID string, ev CalendarEvent) (eventID, htmlLink string, err error)
}

// CalendarEvent is the provider-agnostic shape the executor builds from a
// prepared action; only structural fields, no Axis internals.
type CalendarEvent struct {
	Summary   string
	Start     time.Time
	End       time.Time
	Timezone  string
	Attendees []string
}

// GoogleCalendarExecutor creates real events in Google Calendar. It implements
// ActionExecutorPort, so it inherits every safeguard ExecuteApprovedAction
// applies (binding re-verification, exactly-once BeginExecution, Nexus report)
// unchanged — the only new thing is the real external effect.
//
// SCOPE (PR1, single-tenant demo): every tenant's events are written to ONE
// configured calendar; tenantID is not yet mapped to a per-tenant calendar or
// credential. Multi-tenant isolation of the external target is deferred to the
// per-tenant secret_ref work — do NOT enable this in a shared multi-tenant
// deployment until each tenant resolves its own calendar/credentials.
type GoogleCalendarExecutor struct {
	client     calendarEventCreator
	calendarID string
}

func NewGoogleCalendarExecutor(client calendarEventCreator, calendarID string) *GoogleCalendarExecutor {
	if calendarID == "" {
		calendarID = "primary"
	}
	return &GoogleCalendarExecutor{client: client, calendarID: calendarID}
}

func (e *GoogleCalendarExecutor) Mode() string { return modeGoogleCalendar }

// ExternalEffects is true: Execute creates a real event in the customer's
// calendar, so the run trace records it as an external effect.
func (e *GoogleCalendarExecutor) ExternalEffects() bool { return true }

func (e *GoogleCalendarExecutor) Execute(ctx context.Context, tenantID string, virployeeID uuid.UUID, attempt ExecutionAttempt, action preparedactions.Action) (string, map[string]any, error) {
	start, err := action.StartsAt()
	if err != nil {
		return "", nil, fmt.Errorf("resolve start time: %w", err)
	}
	minutes := action.DurationMinutes
	if minutes <= 0 {
		minutes = defaultEventDurationMinutes
	}
	event := CalendarEvent{
		Summary:   action.Title,
		Start:     start,
		End:       start.Add(time.Duration(minutes) * time.Minute),
		Timezone:  action.Timezone,
		Attendees: action.Attendees,
	}
	// The idempotency key is a hex hash — valid as a Google event id (base32hex
	// charset, within the length bounds) — so a retry lands on the same event.
	eventID, htmlLink, err := e.client.CreateEvent(ctx, e.calendarID, attempt.IdempotencyKey, event)
	if err != nil {
		return "", nil, err
	}
	return eventID, map[string]any{
		"mode":          modeGoogleCalendar,
		"resource_id":   eventID,
		"resource_type": "calendar_event",
		"calendar_id":   e.calendarID,
		"html_link":     htmlLink,
	}, nil
}

// --- real Google Calendar client (ADC) ---

type googleCalendarClient struct {
	svc *calendar.Service
}

// NewGoogleCalendarClient builds a Calendar API client from Application Default
// Credentials (same mechanism as Vertex/Gemini — no key files in the app). It
// requests the least-privilege events scope; note that for user ("authorized_user")
// ADC the effective scopes come from `gcloud auth application-default login
// --scopes=...`, so that command must include calendar.events.
func NewGoogleCalendarClient(ctx context.Context) (calendarEventCreator, error) {
	svc, err := calendar.NewService(ctx, option.WithScopes(calendar.CalendarEventsScope))
	if err != nil {
		return nil, fmt.Errorf("build calendar service: %w", err)
	}
	return &googleCalendarClient{svc: svc}, nil
}

func (g *googleCalendarClient) CreateEvent(ctx context.Context, calendarID, deterministicID string, ev CalendarEvent) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, externalCallTimeout)
	defer cancel()

	event := &calendar.Event{
		Id:      deterministicID,
		Summary: ev.Summary,
		Start:   &calendar.EventDateTime{DateTime: ev.Start.Format(time.RFC3339), TimeZone: ev.Timezone},
		End:     &calendar.EventDateTime{DateTime: ev.End.Format(time.RFC3339), TimeZone: ev.Timezone},
	}
	for _, email := range ev.Attendees {
		event.Attendees = append(event.Attendees, &calendar.EventAttendee{Email: email})
	}
	// SendUpdates "all" so attendees actually receive the invitation — without it
	// they are added but never notified.
	created, err := g.svc.Events.Insert(calendarID, event).SendUpdates("all").Context(ctx).Do()
	if err != nil {
		return g.resolveConflict(ctx, calendarID, deterministicID, err)
	}
	return created.Id, created.HtmlLink, nil
}

// resolveConflict handles a 409 from Insert: an event with this id already
// exists (an idempotent retry). It confirms the event is LIVE before reporting
// success — a previously cancelled/deleted event keeps its id and would return
// via Get, so treating any 409 as success would falsely report a live event.
func (g *googleCalendarClient) resolveConflict(ctx context.Context, calendarID, id string, insertErr error) (string, string, error) {
	var apiErr *googleapi.Error
	if !errors.As(insertErr, &apiErr) || apiErr.Code != http.StatusConflict {
		// Do not echo the raw API error (it can contain request payload / PII);
		// the HTTP code is enough for the audit trace.
		return "", "", sanitizedAPIError("insert", insertErr)
	}
	existing, getErr := g.svc.Events.Get(calendarID, id).Context(ctx).Do()
	if getErr != nil {
		return "", "", sanitizedAPIError("get-after-conflict", getErr)
	}
	if existing.Status == "cancelled" {
		return "", "", fmt.Errorf("google calendar: event %s exists but is cancelled", id)
	}
	return existing.Id, existing.HtmlLink, nil
}

// sanitizedAPIError strips the response body from a Google API error so no
// request payload (e.g. attendee emails) leaks into the persisted attempt error
// or the run trace; only the HTTP status is surfaced.
func sanitizedAPIError(op string, err error) error {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return fmt.Errorf("google calendar %s failed (HTTP %d)", op, apiErr.Code)
	}
	return fmt.Errorf("google calendar %s failed", op)
}
