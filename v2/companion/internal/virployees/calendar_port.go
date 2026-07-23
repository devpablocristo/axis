package virployees

import (
	"context"
	"time"
)

// CalendarEvent and CalendarAPI are retained only for the explicitly enabled
// v1 compatibility adapter. New executors use ActionExecutorV2Port through an
// organization-scoped connector binding.
type CalendarEvent struct {
	Title           string
	StartsAt        time.Time
	Timezone        string
	DurationMinutes int
	Attendees       []string
}

type CalendarInsertResult struct {
	EventID        string
	HTMLLink       string
	AlreadyExisted bool
}

type CalendarAPI interface {
	InsertEvent(context.Context, string, string, CalendarEvent) (CalendarInsertResult, error)
	DeleteEvent(context.Context, string, string) error
}
