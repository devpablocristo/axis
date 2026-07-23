package executiongate

// This file is the compatibility decoder for pending v1 Calendar actions.
// New actions use PreparedActionV2 and never select behavior from these keys.

import (
	"errors"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

func legacyRequiredExecutionAutonomy(capabilityKey string) (virployeedomain.AutonomyLevel, bool) {
	switch capabilityKey {
	case "calendar.events.read":
		return virployeedomain.AutonomyA1, true
	case "calendar.events.create", "calendar.events.update", "calendar.events.delete":
		return virployeedomain.AutonomyA3, true
	default:
		return "", false
	}
}

func applyLegacyCalendarConfirmedDraft(result dryrun.Result, confirmed ConfirmedDraft) (dryrun.Result, error) {
	confirmed.Action = strings.TrimSpace(confirmed.Action)
	confirmed.Kind = strings.TrimSpace(confirmed.Kind)
	if confirmed.Action == "" {
		return dryrun.Result{}, errors.New("confirmed_draft.action is required")
	}
	if !result.Intent.Matched {
		return dryrun.Result{}, errors.New("confirmed_draft cannot be used when no intent was detected")
	}
	if confirmed.Action != result.Intent.CapabilityKey {
		return dryrun.Result{}, errors.New("confirmed_draft.action must match the detected intent")
	}
	if confirmed.Action != "calendar.events.create" {
		return dryrun.Result{}, errors.New("confirmed_draft is only supported for the legacy Calendar v1 action")
	}

	fields := confirmedFieldMap(confirmed.Fields)
	missing := []dryrun.DraftMissingField{}
	for _, required := range calendarEventCreateRequiredFields() {
		if strings.TrimSpace(fields[required.key]) == "" {
			missing = append(missing, dryrun.DraftMissingField{
				Key: required.key, Label: required.label, Reason: required.reason,
			})
		}
	}
	status := dryrun.DraftStatusReady
	if len(missing) > 0 {
		status = dryrun.DraftStatusNeedsInput
	}
	result.Draft = dryrun.Draft{
		Status: status, Action: "calendar.events.create", Kind: "calendar_event",
		Summary: "Prepare a calendar event draft",
		Fields:  confirmedDraftFields(fields), MissingFields: missing,
		Notes: []string{"No external action will be executed."},
	}
	return result, nil
}

func confirmedFieldMap(fields []ConfirmedDraftField) map[string]string {
	out := map[string]string{}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key != "" {
			out[key] = strings.TrimSpace(field.Value)
		}
	}
	return out
}

func confirmedDraftFields(fields map[string]string) []dryrun.DraftField {
	out := []dryrun.DraftField{}
	for _, required := range calendarEventCreateRequiredFields() {
		value := strings.TrimSpace(fields[required.key])
		if value != "" {
			out = append(out, dryrun.DraftField{
				Key: required.key, Label: required.label, Value: value, Source: "confirmed",
			})
		}
	}
	return out
}

func calendarEventCreateRequiredFields() []struct {
	key, label, reason string
} {
	return []struct{ key, label, reason string }{
		{key: "title", label: "Title", reason: "Title is required before preparing the event."},
		{key: "date", label: "Date", reason: "Date in YYYY-MM-DD format is required before preparing the event."},
		{key: "time", label: "Time", reason: "Time is required before preparing the event."},
		{key: "timezone", label: "Timezone", reason: "An IANA timezone is required before preparing the event."},
		{key: "duration_minutes", label: "Duration", reason: "Duration in minutes is required before preparing the event."},
		{key: "attendees", label: "Attendees", reason: "At least one attendee is required for a meeting."},
	}
}
