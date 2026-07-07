package domain

import (
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestNormalizeCreateInputDefaultsRiskAndEnabled(t *testing.T) {
	out, err := NormalizeCreateInput(CreateInput{
		ActionTypeKey: "calendar.events.create",
		Name:          "Create event",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput() error = %v", err)
	}
	if out.RiskClass != RiskClassLow {
		t.Fatalf("risk class = %q, want %q", out.RiskClass, RiskClassLow)
	}
	if !out.Enabled {
		t.Fatal("enabled should default to true")
	}
}

func TestNormalizeCreateInputRejectsInvalidKey(t *testing.T) {
	_, err := NormalizeCreateInput(CreateInput{
		ActionTypeKey: "Calendar Events Create",
		Name:          "Create event",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestNormalizeCreateInputRejectsInvalidRisk(t *testing.T) {
	_, err := NormalizeCreateInput(CreateInput{
		ActionTypeKey: "calendar.events.create",
		Name:          "Create event",
		RiskClass:     "critical",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
}
