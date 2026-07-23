package domain

import (
	"testing"
	"time"

	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestNormalizeCreateInput(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		CapabilityKey:    "soporte.ticket.responder",
		Name:             " Respond tickets ",
		Description:      " Draft support replies ",
		RequiredAutonomy: "A2",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.CapabilityKey != "soporte.ticket.responder" || got.Name != "Respond tickets" || got.Description != "Draft support replies" || got.RequiredAutonomy != virployeedomain.AutonomyA2 {
		t.Fatalf("unexpected normalized input: %+v", got)
	}
}

func TestNormalizeCreateInputValidatesKey(t *testing.T) {
	for _, key := range []string{
		"billing.read",
		"calendar.events.read.today",
		"Calendar.Events.Read",
		"support.ticket.draft_reply",
		"crm.contact.read2",
		"calendario.eventos.leér",
	} {
		if _, err := NormalizeCreateInput(CreateInput{CapabilityKey: key, Name: "Read", RequiredAutonomy: "A0"}); !domainerr.IsValidation(err) {
			t.Fatalf("expected validation for key %q, got %v", key, err)
		}
	}
}

func TestNormalizeCreateInputAllowsUUIDOnlyIdentity(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		Name: "Gestionar campos de granos", RequiredAutonomy: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CapabilityKey != "" {
		t.Fatalf("an omitted compatibility alias must stay omitted, got %q", got.CapabilityKey)
	}
}

func TestNormalizeCreateInputAllowsEnye(t *testing.T) {
	if _, err := NormalizeCreateInput(CreateInput{CapabilityKey: "niñez.casos.leer", Name: "Read cases", RequiredAutonomy: "A0"}); err != nil {
		t.Fatalf("expected ñ to be valid, got %v", err)
	}
}

func TestNormalizeCreateInputValidatesCoreFields(t *testing.T) {
	if _, err := NormalizeCreateInput(CreateInput{CapabilityKey: "billing.invoice.read", Name: "", RequiredAutonomy: "A0"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for name, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{CapabilityKey: "billing.invoice.read", Name: "Read", RequiredAutonomy: ""}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for required_autonomy, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{CapabilityKey: "billing.invoice.read", Name: "Read", RequiredAutonomy: "A9"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid required_autonomy, got %v", err)
	}
}

func TestCapabilityState(t *testing.T) {
	now := time.Now()
	if got := (Capability{}).State(); got != StateActive {
		t.Fatalf("expected active, got %s", got)
	}
	if got := (Capability{ArchivedAt: &now}).State(); got != StateArchived {
		t.Fatalf("expected archived, got %s", got)
	}
	if got := (Capability{ArchivedAt: &now, TrashedAt: &now}).State(); got != StateTrashed {
		t.Fatalf("expected trashed, got %s", got)
	}
}
