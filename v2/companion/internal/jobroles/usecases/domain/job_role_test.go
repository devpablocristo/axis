package domain

import (
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestNormalizeCreateInputGeneratesSlug(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		Name:             " Sales Assistant ",
		Mission:          " Helps sales teams ",
		Responsibilities: []Responsibility{{Title: " Contact leads ", Description: " Promptly ", ExpectedOutcome: " Qualified leads ", Priority: 1}},
		SuccessCriteria:  []SuccessCriterion{{Title: " Conversion ", Description: " Monthly ", TargetValue: "20%", Priority: 2}},
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Name != "Sales Assistant" || got.Slug != "sales-assistant" || got.Mission != "Helps sales teams" {
		t.Fatalf("unexpected normalized identity: %+v", got)
	}
	if got.Responsibilities[0].Title != "Contact leads" || got.Responsibilities[0].ExpectedOutcome != "Qualified leads" || got.SuccessCriteria[0].Title != "Conversion" {
		t.Fatalf("unexpected normalized professional definition: %+v", got)
	}
}

func TestNormalizeCreateInputValidatesProfessionalDefinition(t *testing.T) {
	if _, err := NormalizeCreateInput(CreateInput{Name: "Doctor", Responsibilities: []Responsibility{{Title: " "}}}); !domainerr.IsValidation(err) {
		t.Fatalf("expected responsibility title validation, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "Doctor", SuccessCriteria: []SuccessCriterion{{Title: "Safety", Priority: -1}}}); !domainerr.IsValidation(err) {
		t.Fatalf("expected criterion priority validation, got %v", err)
	}
}

func TestNormalizeCreateInputValidatesCoreFields(t *testing.T) {
	if _, err := NormalizeCreateInput(CreateInput{Name: ""}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for name, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "Ops", Slug: "!!!"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for slug, got %v", err)
	}
}

func TestJobRoleState(t *testing.T) {
	now := time.Now()
	if got := (JobRole{}).State(); got != StateActive {
		t.Fatalf("expected active, got %s", got)
	}
	if got := (JobRole{ArchivedAt: &now}).State(); got != StateArchived {
		t.Fatalf("expected archived, got %s", got)
	}
	if got := (JobRole{ArchivedAt: &now, TrashedAt: &now}).State(); got != StateTrashed {
		t.Fatalf("expected trashed, got %s", got)
	}
}
