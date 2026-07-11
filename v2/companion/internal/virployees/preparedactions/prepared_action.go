package preparedactions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
)

const (
	SchemaVersion = "calendar.event.create.v1"
	ActionCreate  = "calendar.events.create"
)

type Action struct {
	SchemaVersion   string   `json:"schema_version"`
	Action          string   `json:"action"`
	Title           string   `json:"title"`
	Date            string   `json:"date"`
	Time            string   `json:"time"`
	Timezone        string   `json:"timezone"`
	DurationMinutes int      `json:"duration_minutes"`
	Attendees       []string `json:"attendees"`
}

func FromDraft(draft dryrun.Draft) (Action, error) {
	if strings.TrimSpace(draft.Action) != ActionCreate {
		return Action{}, fmt.Errorf("prepared action is only supported for %s", ActionCreate)
	}
	fields := make(map[string]string, len(draft.Fields))
	for _, field := range draft.Fields {
		fields[strings.TrimSpace(field.Key)] = strings.TrimSpace(field.Value)
	}
	duration := 60
	if value := fields["duration_minutes"]; value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Action{}, fmt.Errorf("duration_minutes must be a number")
		}
		duration = parsed
	}
	action := Action{
		SchemaVersion:   SchemaVersion,
		Action:          ActionCreate,
		Title:           fields["title"],
		Date:            fields["date"],
		Time:            fields["time"],
		Timezone:        fields["timezone"],
		DurationMinutes: duration,
		Attendees:       normalizeAttendees(fields["attendees"]),
	}
	if action.Title == "" {
		return Action{}, fmt.Errorf("title is required")
	}
	if _, err := time.Parse("2006-01-02", action.Date); err != nil {
		return Action{}, fmt.Errorf("date must use YYYY-MM-DD")
	}
	if _, err := time.Parse("15:04", action.Time); err != nil {
		return Action{}, fmt.Errorf("time must use HH:MM in 24-hour format")
	}
	if action.Timezone == "" {
		return Action{}, fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(action.Timezone); err != nil {
		return Action{}, fmt.Errorf("timezone must be a valid IANA timezone")
	}
	if action.DurationMinutes < 5 || action.DurationMinutes > 1440 {
		return Action{}, fmt.Errorf("duration_minutes must be between 5 and 1440")
	}
	if len(action.Attendees) == 0 {
		return Action{}, fmt.Errorf("at least one valid attendee is required")
	}
	return action, nil
}

func (a Action) StartsAt() (time.Time, error) {
	location, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation("2006-01-02 15:04", a.Date+" "+a.Time, location)
}

func (a Action) PayloadHash() (string, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeAttendees(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		email := strings.ToLower(strings.TrimSpace(part))
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	sort.Strings(out)
	return out
}
