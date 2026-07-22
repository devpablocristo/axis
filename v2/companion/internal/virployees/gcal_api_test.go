package virployees

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCalendarEventID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abcdef0123456789", "abcdef0123456789"}, // hex passes through unchanged
		{"AB", "axis0ab"},                        // short + lowercased + padded
		{"xyz", "axis0"},                         // x/y/z are outside base32hex (>v) → stripped
	}
	for _, tc := range cases {
		if got := calendarEventID(tc.in); got != tc.want {
			t.Fatalf("calendarEventID(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if len(calendarEventID(tc.in)) < 5 {
			t.Fatalf("event id must be at least 5 chars for %q", tc.in)
		}
	}
}

func TestGoogleCalendarCredentialsRejectExternalConfiguration(t *testing.T) {
	_, err := NewGoogleCalendarAPIFromJSON(context.Background(), []byte(`{"type":"external_account","credential_source":{"url":"https://attacker.invalid/token"}}`))
	if err == nil || !strings.Contains(err.Error(), "only complete service-account credentials") {
		t.Fatalf("externally sourced credentials must be rejected, got %v", err)
	}
}

func newTestCalendarAPI(handler http.HandlerFunc) (*googleCalendarAPI, *httptest.Server) {
	srv := httptest.NewServer(handler)
	return &googleCalendarAPI{httpClient: srv.Client(), baseURL: srv.URL}, srv
}

func TestGoogleCalendarAPIInsertEventMapsRequestAndResponse(t *testing.T) {
	var gotBody map[string]any
	var gotURI string
	api, srv := newTestCalendarAPI(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"axis0evt","htmlLink":"https://cal/evt"}`))
	})
	defer srv.Close()

	start := time.Date(2026, 8, 1, 15, 0, 0, 0, time.UTC)
	res, err := api.InsertEvent(context.Background(), "team@group.calendar.google.com", "idem-key", CalendarEvent{
		Title: "Q3 sync", StartsAt: start, Timezone: "UTC", DurationMinutes: 30, Attendees: []string{"ana@example.com"},
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if res.EventID != "axis0evt" || res.HTMLLink != "https://cal/evt" || res.AlreadyExisted {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !strings.Contains(gotURI, "team%40group.calendar.google.com") {
		t.Fatalf("calendar id must be url-escaped in the request, got %q", gotURI)
	}
	if gotBody["id"] != calendarEventID("idem-key") {
		t.Fatalf("event id must be the idempotent id, got %v", gotBody["id"])
	}
	if gotBody["summary"] != "Q3 sync" {
		t.Fatalf("summary not mapped: %v", gotBody["summary"])
	}
	start0 := gotBody["start"].(map[string]any)
	if start0["timeZone"] != "UTC" || start0["dateTime"] == "" {
		t.Fatalf("start not mapped: %v", start0)
	}
	att := gotBody["attendees"].([]any)
	if len(att) != 1 || att[0].(map[string]any)["email"] != "ana@example.com" {
		t.Fatalf("attendees not mapped: %v", att)
	}
}

func TestGoogleCalendarAPIConflictIsIdempotentReplay(t *testing.T) {
	api, srv := newTestCalendarAPI(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"message":"duplicate"}}`))
	})
	defer srv.Close()

	res, err := api.InsertEvent(context.Background(), "cal", "idem-key", CalendarEvent{Title: "x", Timezone: "UTC", DurationMinutes: 30})
	if err != nil {
		t.Fatalf("409 must be a silent idempotent replay, got error: %v", err)
	}
	if !res.AlreadyExisted || res.EventID != calendarEventID("idem-key") {
		t.Fatalf("expected idempotent replay result, got %+v", res)
	}
}

func TestGoogleCalendarAPISurfacesErrorStatusAndMessage(t *testing.T) {
	api, srv := newTestCalendarAPI(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"caller does not have permission"}}`))
	})
	defer srv.Close()

	_, err := api.InsertEvent(context.Background(), "cal", "k", CalendarEvent{Title: "x", Timezone: "UTC", DurationMinutes: 30})
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected a surfaced permission error, got %v", err)
	}
}

func TestGoogleCalendarAPIDeleteEvent(t *testing.T) {
	t.Run("204 no content succeeds and hits the right path/method", func(t *testing.T) {
		var method, uri string
		api, srv := newTestCalendarAPI(func(w http.ResponseWriter, r *http.Request) {
			method, uri = r.Method, r.RequestURI
			w.WriteHeader(http.StatusNoContent)
		})
		defer srv.Close()
		if err := api.DeleteEvent(context.Background(), "team@group.calendar.google.com", "evt-1"); err != nil {
			t.Fatalf("DeleteEvent: %v", err)
		}
		if method != http.MethodDelete || !strings.Contains(uri, "team%40group.calendar.google.com/events/evt-1") {
			t.Fatalf("unexpected delete request: %s %s", method, uri)
		}
	})

	t.Run("404 is an idempotent success (already gone)", func(t *testing.T) {
		api, srv := newTestCalendarAPI(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
		})
		defer srv.Close()
		if err := api.DeleteEvent(context.Background(), "cal", "gone"); err != nil {
			t.Fatalf("404 on delete must be a silent success, got %v", err)
		}
	})

	t.Run("other errors surface", func(t *testing.T) {
		api, srv := newTestCalendarAPI(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		})
		defer srv.Close()
		if err := api.DeleteEvent(context.Background(), "cal", "x"); err == nil {
			t.Fatal("expected a surfaced error on 403")
		}
	})
}
