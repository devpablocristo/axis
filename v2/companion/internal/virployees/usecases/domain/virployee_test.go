package domain

import (
	"testing"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestNormalizeCreateInput(t *testing.T) {
	supervisorID := "dev-user"
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	employerID := uuid.New()
	got, err := NormalizeCreateInput(CreateInput{
		Name:              "  Sales Assistant ",
		JobRoleID:         " " + jobRoleID.String() + " ",
		ProfileTemplateID: " " + profileTemplateID.String() + " ",
		Description:       "  Helps  ",
		SupervisorUserID:  " " + supervisorID + " ",
		Autonomy:          " A2 ",
		EmployerSubjectID: " " + employerID.String() + " ",
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Name != "Sales Assistant" || got.JobRoleID != jobRoleID || got.ProfileTemplateID != profileTemplateID || got.Description != "Helps" || got.SupervisorUserID != supervisorID || got.Autonomy != AutonomyA2 {
		t.Fatalf("unexpected normalized input: %+v", got)
	}
	if got.EmployerSubjectID != employerID {
		t.Fatalf("expected employer %s, got %s", employerID, got.EmployerSubjectID)
	}
}

func TestNormalizeCreateInputDefaultsAutonomy(t *testing.T) {
	got, err := NormalizeCreateInput(CreateInput{
		Name:              "name",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if got.Autonomy != AutonomyA1 {
		t.Fatalf("expected default autonomy A1, got %s", got.Autonomy)
	}
	if got.GroundingMode != GroundingSourcesOnly {
		t.Fatalf("expected new virployees to default to sources_only, got %q", got.GroundingMode)
	}
}

func TestNormalizeUpdateInputPreservesGroundingWhenOmittedAndValidatesExplicitMode(t *testing.T) {
	base := UpdateInput{Name: "Agent", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "user-1"}
	got, err := NormalizeUpdateInput(base)
	if err != nil {
		t.Fatalf("NormalizeUpdateInput: %v", err)
	}
	if got.GroundingMode != "" {
		t.Fatalf("omitted update grounding mode must preserve storage value, got %q", got.GroundingMode)
	}
	base.GroundingMode = "unsupported"
	if _, err := NormalizeUpdateInput(base); !domainerr.IsValidation(err) {
		t.Fatalf("expected grounding validation, got %v", err)
	}
}

func TestNormalizeCreateInputRequiresNameJobRoleAndSupervisor(t *testing.T) {
	jobRoleID := uuid.NewString()
	profileTemplateID := uuid.NewString()
	if _, err := NormalizeCreateInput(CreateInput{Name: "", JobRoleID: jobRoleID, ProfileTemplateID: profileTemplateID, SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for name, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: "", ProfileTemplateID: profileTemplateID, SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for job_role_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: "not-a-uuid", ProfileTemplateID: profileTemplateID, SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid job_role_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for profile_template_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, ProfileTemplateID: "not-a-uuid", SupervisorUserID: uuid.NewString()}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid profile_template_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, ProfileTemplateID: profileTemplateID}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for supervisor_user_id, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, ProfileTemplateID: profileTemplateID, SupervisorUserID: "dev-user", Autonomy: "A9"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid autonomy, got %v", err)
	}
	if _, err := NormalizeCreateInput(CreateInput{Name: "name", JobRoleID: jobRoleID, ProfileTemplateID: profileTemplateID, SupervisorUserID: "dev-user", EmployerSubjectID: "not-a-uuid"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for invalid employer_subject_id, got %v", err)
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
