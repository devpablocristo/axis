package domain

import (
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestNormalizeCreateInput(t *testing.T) {
	supervisorID := uuid.New()
	got, err := NormalizeCreateInput(CreateInput{
		Name:             "  Sales Assistant ",
		Role:             " sales_assistant ",
		Description:      "  Helps  ",
		SupervisorUserID: " " + supervisorID.String() + " ",
		Autonomy:         " A2 ",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Name != "Sales Assistant" || got.Role != "sales_assistant" || got.Description != "Helps" || got.SupervisorUserID != supervisorID || got.Autonomy != AutonomyA2 {
		t.Fatalf("unexpected normalized input: %+v", got)
	}
}

func TestNormalizeCreateInputDefaultsAutonomy(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		Name:             "name",
		Role:             "role",
		SupervisorUserID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Autonomy != AutonomyA1 {
		t.Fatalf("expected default autonomy A1, got %s", got.Autonomy)
	}
}

func TestNormalizeCreateInputRequiresNameRoleAndSupervisor(t *testing.T) {
	if _, err := NormalizeCreateInput(CreateInput{Name: "", Role: "role", SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for name, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", Role: "", SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for role, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", Role: "role"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for supervisor_user_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", Role: "role", SupervisorUserID: "not-a-uuid"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid supervisor_user_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", Role: "role", SupervisorUserID: uuid.NewString(), Autonomy: "A9"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid autonomy, got %v", err)
	}
}

func TestVirployeeState(t *testing.T) {
	now := time.Now()
	if got := (Virployee{}).State(); got != StateActive {
		t.Fatalf("expected active, got %s", got)
	}
	if got := (Virployee{ArchivedAt: &now}).State(); got != StateArchived {
		t.Fatalf("expected archived, got %s", got)
	}
	if got := (Virployee{ArchivedAt: &now, TrashedAt: &now}).State(); got != StateTrashed {
		t.Fatalf("expected trashed, got %s", got)
	}
}
