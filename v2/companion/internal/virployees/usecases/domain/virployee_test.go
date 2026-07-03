package domain

import (
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestNormalizeCreateInput(t *testing.T) {
	supervisorID := uuid.New()
	jobRoleID := uuid.New()
	got, err := NormalizeCreateInput(CreateInput{
		Name:             "  Sales Assistant ",
		JobRoleID:        " " + jobRoleID.String() + " ",
		Description:      "  Helps  ",
		SupervisorUserID: " " + supervisorID.String() + " ",
		Autonomy:         " A2 ",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Name != "Sales Assistant" || got.JobRoleID != jobRoleID || got.Description != "Helps" || got.SupervisorUserID != supervisorID || got.Autonomy != AutonomyA2 {
		t.Fatalf("unexpected normalized input: %+v", got)
	}
}

func TestNormalizeCreateInputDefaultsAutonomy(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		Name:             "name",
		JobRoleID:        uuid.NewString(),
		SupervisorUserID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Autonomy != AutonomyA1 {
		t.Fatalf("expected default autonomy A1, got %s", got.Autonomy)
	}
}

func TestNormalizeCreateInputRequiresNameJobRoleAndSupervisor(t *testing.T) {
	jobRoleID := uuid.NewString()
	if _, err := NormalizeCreateInput(CreateInput{Name: "", JobRoleID: jobRoleID, SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for name, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: "", SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for job_role_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: "not-a-uuid", SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid job_role_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for supervisor_user_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, SupervisorUserID: "not-a-uuid"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid supervisor_user_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, SupervisorUserID: uuid.NewString(), Autonomy: "A9"}); !domainerr.IsValidation(err) {
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
