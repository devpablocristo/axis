package virployees

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	calendarEventsScope = "https://www.googleapis.com/auth/calendar.events"
	calendarBaseURL     = "https://www.googleapis.com/calendar/v3"
)

// googleCalendarAPI talks to the Google Calendar REST API directly using an
// OAuth2 token source from Application Default Credentials. It deliberately avoids
// the large generated SDK to keep the dependency surface small and the request
// shape (idempotent event id) fully under our control.
//
// Credentials come from the ambient ADC chain — a service account attached to the
// workload in production, or GOOGLE_APPLICATION_CREDENTIALS locally — so no key
// material ever passes through Axis config, memory, or run traces (G3.2, ADR 0002).
type googleCalendarAPI struct {
	httpClient *http.Client
	baseURL    string
}

// NewGoogleCalendarAPI builds a CalendarAPI backed by ADC for the configured
// service account. It resolves credentials once at wiring time; a failure here is
// fail-closed (the executor is not registered).
func NewGoogleCalendarAPI(ctx context.Context) (CalendarAPI, error) {
	creds, err := google.FindDefaultCredentials(ctx, calendarEventsScope)
	if err != nil {
		return nil, fmt.Errorf("resolve google credentials: %w", err)
	}
	client := oauth2.NewClient(ctx, creds.TokenSource)
	// Keep OTel propagation consistent with the rest of companion's egress.
	client.Transport = otelhttp.NewTransport(client.Transport)
	client.Timeout = 20 * time.Second
	return &googleCalendarAPI{httpClient: client, baseURL: calendarBaseURL}, nil
}

func (a *googleCalendarAPI) InsertEvent(ctx context.Context, calendarID, idempotencyKey string, ev CalendarEvent) (CalendarInsertResult, error) {
	eventID := calendarEventID(idempotencyKey)
	end := ev.StartsAt.Add(time.Duration(ev.DurationMinutes) * time.Minute)

	payload := map[string]any{
		// A client-supplied id makes the insert idempotent: a repeated POST with the
		// same id returns 409, which we treat as a safe replay (G3.3).
		"id":      eventID,
		"summary": ev.Title,
		"start":   map[string]string{"dateTime": ev.StartsAt.Format(time.RFC3339), "timeZone": ev.Timezone},
		"end":     map[string]string{"dateTime": end.Format(time.RFC3339), "timeZone": ev.Timezone},
	}
	if attendees := attendeePayload(ev.Attendees); len(attendees) > 0 {
		payload["attendees"] = attendees
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CalendarInsertResult{}, fmt.Errorf("encode calendar event: %w", err)
	}

	// QueryEscape (not PathEscape) so the "@" in email-style calendar ids becomes
	// %40, matching what the Google client libraries send.
	endpoint := fmt.Sprintf("%s/calendars/%s/events", a.baseURL, url.QueryEscape(calendarID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return CalendarInsertResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return CalendarInsertResult{}, fmt.Errorf("call calendar api: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var out struct {
			ID       string `json:"id"`
			HTMLLink string `json:"htmlLink"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return CalendarInsertResult{}, fmt.Errorf("decode calendar response: %w", err)
		}
		return CalendarInsertResult{EventID: out.ID, HTMLLink: out.HTMLLink}, nil
	case http.StatusConflict:
		// Duplicate id: the event already exists — an idempotent replay, not an error.
		return CalendarInsertResult{EventID: eventID, AlreadyExisted: true}, nil
	default:
		// Surface only the status and Google's error message, never request auth.
		return CalendarInsertResult{}, fmt.Errorf("calendar insert failed: %s: %s", resp.Status, googleErrorMessage(respBody))
	}
}

func attendeePayload(emails []string) []map[string]string {
	out := make([]map[string]string, 0, len(emails))
	for _, e := range emails {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, map[string]string{"email": e})
		}
	}
	return out
}

// calendarEventID maps an idempotency key to a valid Google Calendar event id:
// lowercase base32hex (characters a–v and 0–9), length 5–1024. SHA-256 hex keys
// already satisfy this; we sanitize defensively and pad short inputs.
func calendarEventID(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'v') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	id := b.String()
	if len(id) < 5 {
		id = "axis0" + id
	}
	if len(id) > 1024 {
		id = id[:1024]
	}
	return id
}

func googleErrorMessage(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return "unknown error"
}
