package preparedactions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/virployees/dryrun"
)

func TestFromDeleteDraftBuildsCompensatingAction(t *testing.T) {
	action, err := FromDeleteDraft(dryrun.Draft{Action: ActionDelete, Fields: []dryrun.DraftField{
		{Key: "event_reference", Value: " evt-abc "},
	}})
	if err != nil {
		t.Fatalf("FromDeleteDraft: %v", err)
	}
	if action.Action != ActionDelete || action.SchemaVersion != DeleteSchemaVersion || action.EventID != "evt-abc" {
		t.Fatalf("unexpected delete action: %+v", action)
	}
}

func TestFromDeleteDraftRequiresEventReference(t *testing.T) {
	if _, err := FromDeleteDraft(dryrun.Draft{Action: ActionDelete}); err == nil {
		t.Fatal("expected an error when event_reference is missing")
	}
}

func TestFromReadyDraftDispatchesAndIgnoresNonExecutable(t *testing.T) {
	create, err := FromReadyDraft(dryrun.Draft{Action: ActionCreate, Fields: []dryrun.DraftField{
		{Key: "title", Value: "x"}, {Key: "date", Value: "2026-07-12"}, {Key: "time", Value: "15:30"},
		{Key: "timezone", Value: "UTC"}, {Key: "attendees", Value: "a@example.com"},
	}})
	if err != nil || create == nil || create.Action != ActionCreate {
		t.Fatalf("create dispatch failed: %+v err=%v", create, err)
	}
	del, err := FromReadyDraft(dryrun.Draft{Action: ActionDelete, Fields: []dryrun.DraftField{{Key: "event_reference", Value: "evt"}}})
	if err != nil || del == nil || del.Action != ActionDelete {
		t.Fatalf("delete dispatch failed: %+v err=%v", del, err)
	}
	// Non-executable actions (e.g. read) produce no prepared action or binding.
	other, err := FromReadyDraft(dryrun.Draft{Action: "calendar.events.read"})
	if err != nil || other != nil {
		t.Fatalf("read must not produce a prepared action, got %+v err=%v", other, err)
	}
}

// G3.5: a compensation (delete) has a different payload than the create, so it
// necessarily gets its own binding hash — the create's approval can't authorize it.
func TestDeleteHasItsOwnBindingDistinctFromCreate(t *testing.T) {
	create := Action{SchemaVersion: SchemaVersion, Action: ActionCreate, Title: "Sync", Date: "2026-07-12", Time: "15:30", Timezone: "UTC", DurationMinutes: 60, Attendees: []string{"a@example.com"}}
	del := Action{SchemaVersion: DeleteSchemaVersion, Action: ActionDelete, EventID: "evt-1"}
	createHash, _ := create.PayloadHash()
	deleteHash, _ := del.PayloadHash()
	if createHash == deleteHash {
		t.Fatal("delete payload hash must differ from create (own binding)")
	}
}

// Adding EventID must not change how a create action serializes: its payload hash,
// and therefore its binding hash, stays stable (omitempty).
func TestCreateActionSerializationUnaffectedByEventIDField(t *testing.T) {
	create := Action{SchemaVersion: SchemaVersion, Action: ActionCreate, Title: "Sync", Date: "2026-07-12", Time: "15:30", Timezone: "UTC", DurationMinutes: 60, Attendees: []string{"a@example.com"}}
	raw, _ := json.Marshal(create)
	if strings.Contains(string(raw), "event_id") {
		t.Fatalf("create action must not serialize an event_id field, got %s", raw)
	}
}

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

func TestPayloadHashBindsPrincipalContext(t *testing.T) {
	base := Action{SchemaVersion: SchemaVersion, Action: ActionCreate, Title: "Planning", Date: "2026-07-12", Time: "15:30", Timezone: "UTC", DurationMinutes: 60, Attendees: []string{"a@example.com"}}
	legacy, _ := json.Marshal(base)
	if strings.Contains(string(legacy), "principal_") {
		t.Fatalf("legacy action without principal must keep its serialization: %s", legacy)
	}
	base.PrincipalType, base.PrincipalID = "person", "patient-a"
	first, _ := base.PayloadHash()
	base.PrincipalID = "patient-b"
	second, _ := base.PayloadHash()
	if first == second {
		t.Fatal("changing the represented principal must invalidate the prepared action hash")
	}
}

func TestPayloadHashBindsMCPContext(t *testing.T) {
	base := Action{SchemaVersion: SchemaVersion, Action: ActionCreate, Title: "Consulta", Date: "2026-08-10", Time: "10:00", Timezone: "UTC", DurationMinutes: 30, Attendees: []string{"a@example.com"}}
	first, _ := base.PayloadHash()
	base.MCPContext = &MCPContextBinding{
		TenantID: "tenant-1", ActorID: "actor-1", VirployeeID: "virployee-1", SubjectID: "subject-1",
		AssignmentID: "assignment-1", AssignmentVersion: 1, CapabilityKey: ActionCreate,
		CapabilityVersion: "1.0.0", ManifestHash: "manifest", PolicyVersion: 2,
		AuthorityHash: "authority", ContextHash: "context", PayloadHash: "payload", IdempotencyHash: "idem",
	}
	second, _ := base.PayloadHash()
	base.MCPContext.AssignmentVersion = 2
	third, _ := base.PayloadHash()
	if first == second || second == third {
		t.Fatal("MCP context and assignment revision must change the prepared action hash")
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
