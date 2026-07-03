package domain

import (
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestNormalizeCreateInputGeneratesSlug(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		Name:    " Sales Assistant ",
		Mission: " Helps sales teams ",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Name != "Sales Assistant" || got.Slug != "sales-assistant" || got.Mission != "Helps sales teams" {
		t.Fatalf("unexpected normalized identity: %+v", got)
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
