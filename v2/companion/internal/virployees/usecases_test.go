package virployees

import (
	"context"
	"errors"
	"testing"
	"time"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	jobroledomain "github.com/devpablocristo/companion-v2/internal/jobroles/usecases/domain"
	profiletemplatedomain "github.com/devpablocristo/companion-v2/internal/profiletemplates/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/executiongate"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/lifecycle/go/lifecycle"
	"github.com/google/uuid"
)

func TestUseCasesCreateAndListActive(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	supervisorID := "dev-user"
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              " Sales Assistant ",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		SupervisorUserID:  " " + supervisorID + " ",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Sales Assistant" || created.JobRoleID != jobRoleID || created.ProfileTemplateID != profileTemplateID || created.SupervisorUserID != supervisorID || created.Autonomy != domain.AutonomyA2 {
		t.Fatalf("unexpected create output: %+v", created)
	}

	active, err := uc.ListActive(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != created.ID {
		t.Fatalf("unexpected active list: %+v", active)
	}
}

func TestUseCasesCreateDefaultsAutonomyToA1AndValidatesJobRole(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	reader := &fakeJobRoleReader{}
	uc, err := NewUseCases(repo, reader)
	if err != nil {
		t.Fatal(err)
	}

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Autonomy != domain.AutonomyA1 {
		t.Fatalf("expected default autonomy A1, got %s", created.Autonomy)
	}
	if reader.lastTenant != "tenant-1" || reader.lastID != jobRoleID {
		t.Fatalf("expected job role validation, got tenant=%q id=%s", reader.lastTenant, reader.lastID)
	}
}

func TestUseCasesCreateRequiresProfileTemplateID(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:             "Ops",
		JobRoleID:        uuid.NewString(),
		SupervisorUserID: "dev-user",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing profile_template_id, got %v", err)
	}
}

func TestUseCasesCreateFailsWhenJobRoleIsNotActive(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	uc, err := NewUseCases(repo, &fakeJobRoleReader{err: domainerr.Conflict("job role is not active")})
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
	})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict for inactive job role, got %v", err)
	}
}

func TestUseCasesCreateValidatesProfileTemplate(t *testing.T) {
	repo := newFakeRepo()
	profileTemplateID := uuid.New()
	reader := &fakeProfileTemplateReader{}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(reader)

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: profileTemplateID.String(),
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ProfileTemplateID != profileTemplateID {
		t.Fatalf("expected profile_template_id to be persisted, got %+v", created.ProfileTemplateID)
	}
	if reader.lastTenant != "tenant-1" || reader.lastID != profileTemplateID || reader.lastAutonomy != domain.AutonomyA2 {
		t.Fatalf("expected profile template validation, got tenant=%q id=%s autonomy=%s", reader.lastTenant, reader.lastID, reader.lastAutonomy)
	}
}

func TestUseCasesCreateFailsWhenProfileTemplateRejectsAutonomy(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(&fakeProfileTemplateReader{err: domainerr.Validation("profile template max autonomy exceeded")})

	_, err = uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
		Autonomy:          "A3",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for incompatible profile template, got %v", err)
	}
}

func TestUseCasesUpdateRequiresProfileTemplateID(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: uuid.NewString(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.Update(context.Background(), "tenant-1", created.ID, domain.UpdateInput{
		Name:             "Ops",
		JobRoleID:        uuid.NewString(),
		SupervisorUserID: "dev-user",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for missing profile_template_id, got %v", err)
	}
}

func TestUseCasesUpdateChangesProfileTemplateReference(t *testing.T) {
	repo := newFakeRepo()
	reader := &fakeProfileTemplateReader{}
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(reader)
	firstTemplateID := uuid.New()
	secondTemplateID := uuid.New()
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: firstTemplateID.String(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := uc.Update(context.Background(), "tenant-1", created.ID, domain.UpdateInput{
		Name:              "Ops updated",
		JobRoleID:         uuid.NewString(),
		ProfileTemplateID: secondTemplateID.String(),
		SupervisorUserID:  "dev-user",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ProfileTemplateID != secondTemplateID {
		t.Fatalf("expected second profile template, got %s", updated.ProfileTemplateID)
	}
	if reader.lastID != secondTemplateID {
		t.Fatalf("expected reader to be called with second template, got %s", reader.lastID)
	}
}

func TestUseCasesLifecycle(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{Name: "Ops", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "dev-user"})
	if err != nil {
		t.Fatal(err)
	}

	if err := uc.Archive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	assertListLen(t, uc.ListActive, 0)
	assertListLen(t, uc.ListArchived, 1)

	if err := uc.Unarchive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	assertListLen(t, uc.ListActive, 1)

	if err := uc.Trash(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	assertListLen(t, uc.ListActive, 0)
	assertListLen(t, uc.ListTrash, 1)

	if err := uc.Restore(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertListLen(t, uc.ListActive, 1)

	if err := uc.Trash(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Trash again: %v", err)
	}
	if err := uc.Purge(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := uc.Get(context.Background(), "tenant-1", created.ID); !domainerr.IsNotFound(err) {
		t.Fatalf("expected not found after purge, got %v", err)
	}
}

func TestUseCasesUpdateArchivedOrTrashedFails(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{Name: "Ops", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "dev-user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Archive(context.Background(), "tenant-1", created.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Update(context.Background(), "tenant-1", created.ID, domain.UpdateInput{Name: "New", JobRoleID: uuid.NewString(), ProfileTemplateID: uuid.NewString(), SupervisorUserID: "dev-user"})
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict updating archived, got %v", err)
	}
}

func TestUseCasesRuntimeContextReturnsResolvedReferences(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	capabilityID := uuid.New()
	jobRoles := &fakeJobRoleReader{
		role: jobroledomain.JobRole{
			ID:       jobRoleID,
			TenantID: "tenant-1",
			Name:     "Receptionist",
			Mission:  "Welcome visitors",
		},
	}
	profiles := &fakeProfileTemplateReader{
		profile: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			TenantID:     "tenant-1",
			Name:         "Warm receptionist",
			SystemPrompt: "Be warm and concise.",
			MaxAutonomy:  domain.AutonomyA2,
		},
	}
	capabilities := &fakeCapabilityReader{
		rows: map[uuid.UUID]capabilitydomain.Capability{
			capabilityID: {
				ID:               capabilityID,
				TenantID:         "tenant-1",
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: domain.AutonomyA2,
			},
		},
	}
	uc, err := NewUseCases(repo, jobRoles)
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(profiles)
	uc.SetCapabilityValidator(capabilities)

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Sofia",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		CapabilityIDs:     []string{capabilityID.String()},
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := uc.RuntimeContext(context.Background(), "tenant-1", created.ID)
	if err != nil {
		t.Fatalf("RuntimeContext: %v", err)
	}
	if ctx.Virployee.ID != created.ID || ctx.JobRole.Name != "Receptionist" || ctx.ProfileTemplate.SystemPrompt != "Be warm and concise." {
		t.Fatalf("unexpected runtime context: %+v", ctx)
	}
	if len(ctx.Capabilities) != 1 || ctx.Capabilities[0].CapabilityKey != "calendar.events.create" {
		t.Fatalf("unexpected capabilities: %+v", ctx.Capabilities)
	}
}

func TestUseCasesRuntimeContextFailsWhenProfileTemplateNoLongerAllowsAutonomy(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	profiles := &fakeProfileTemplateReader{
		profile: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			TenantID:     "tenant-1",
			Name:         "Safe profile",
			SystemPrompt: "Stay safe.",
			MaxAutonomy:  domain.AutonomyA2,
		},
	}
	uc, err := NewUseCases(repo, &fakeJobRoleReader{
		role: jobroledomain.JobRole{ID: jobRoleID, TenantID: "tenant-1", Name: "Ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(profiles)

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	profiles.profile.MaxAutonomy = domain.AutonomyA1
	if _, err := uc.RuntimeContext(context.Background(), "tenant-1", created.ID); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for profile autonomy mismatch, got %v", err)
	}
}

func TestUseCasesRuntimeContextFailsWhenCapabilityRequiresMoreAutonomy(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	capabilityID := uuid.New()
	capabilities := &fakeCapabilityReader{
		rows: map[uuid.UUID]capabilitydomain.Capability{
			capabilityID: {
				ID:               capabilityID,
				TenantID:         "tenant-1",
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: domain.AutonomyA3,
			},
		},
	}
	uc, err := NewUseCases(repo, &fakeJobRoleReader{
		role: jobroledomain.JobRole{ID: jobRoleID, TenantID: "tenant-1", Name: "Ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(&fakeProfileTemplateReader{
		profile: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			TenantID:     "tenant-1",
			Name:         "Broad profile",
			SystemPrompt: "Work safely.",
			MaxAutonomy:  domain.AutonomyA3,
		},
	})
	uc.SetCapabilityValidator(capabilities)

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Ops",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		CapabilityIDs:     []string{capabilityID.String()},
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := uc.RuntimeContext(context.Background(), "tenant-1", created.ID); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for capability autonomy mismatch, got %v", err)
	}
}

func TestUseCasesDryRunAllowsMatchedCapability(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	capabilityID := uuid.New()
	uc, err := NewUseCases(repo, &fakeJobRoleReader{
		role: jobroledomain.JobRole{ID: jobRoleID, TenantID: "tenant-1", Name: "Receptionist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(&fakeProfileTemplateReader{
		profile: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			TenantID:     "tenant-1",
			Name:         "Receptionist profile",
			SystemPrompt: "Be warm.",
			MaxAutonomy:  domain.AutonomyA2,
		},
	})
	uc.SetCapabilityValidator(&fakeCapabilityReader{
		rows: map[uuid.UUID]capabilitydomain.Capability{
			capabilityID: {
				ID:               capabilityID,
				TenantID:         "tenant-1",
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: domain.AutonomyA2,
			},
		},
	})

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Sofia",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		CapabilityIDs:     []string{capabilityID.String()},
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := uc.DryRun(context.Background(), "tenant-1", created.ID, "Agendá una reunión para mañana")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if result.Decision != "allowed" {
		t.Fatalf("expected allowed, got %+v", result)
	}
	if result.RequiredCapability == nil || result.RequiredCapability.CapabilityKey != "calendar.events.create" || !result.RequiredCapability.Matched {
		t.Fatalf("unexpected required capability: %+v", result.RequiredCapability)
	}
	if result.RequiredAutonomy != domain.AutonomyA2 || result.VirployeeAutonomy != domain.AutonomyA2 {
		t.Fatalf("unexpected autonomy values: required=%s virployee=%s", result.RequiredAutonomy, result.VirployeeAutonomy)
	}
	if result.RuntimeContext.Virployee.ID != created.ID || len(result.RuntimeContext.Capabilities) != 1 {
		t.Fatalf("unexpected runtime context: %+v", result.RuntimeContext)
	}
	if result.Draft.Status != "needs_input" || result.Draft.Action != "calendar.events.create" {
		t.Fatalf("unexpected draft: %+v", result.Draft)
	}
	if len(repo.traces) != 1 || repo.traces[0].Operation != runtraces.OperationDryRun || repo.traces[0].DryRunDecision != "allowed" {
		t.Fatalf("expected dry run trace, got %+v", repo.traces)
	}
}

func TestUseCasesDryRunBlocksMissingCapability(t *testing.T) {
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	uc, err := NewUseCases(repo, &fakeJobRoleReader{
		role: jobroledomain.JobRole{ID: jobRoleID, TenantID: "tenant-1", Name: "Receptionist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(&fakeProfileTemplateReader{
		profile: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			TenantID:     "tenant-1",
			Name:         "Receptionist profile",
			SystemPrompt: "Be warm.",
			MaxAutonomy:  domain.AutonomyA2,
		},
	})
	uc.SetCapabilityValidator(&fakeCapabilityReader{rows: map[uuid.UUID]capabilitydomain.Capability{}})

	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Sofia",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		SupervisorUserID:  "dev-user",
		Autonomy:          "A2",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := uc.DryRun(context.Background(), "tenant-1", created.ID, "Agendá una reunión para mañana")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if result.Decision != "blocked" {
		t.Fatalf("expected blocked, got %+v", result)
	}
	if result.RequiredCapability == nil || result.RequiredCapability.CapabilityKey != "calendar.events.create" || result.RequiredCapability.Matched {
		t.Fatalf("unexpected required capability: %+v", result.RequiredCapability)
	}
	if result.RequiredAutonomy != domain.AutonomyA2 || result.Reason != "required capability is not assigned to the virployee" {
		t.Fatalf("unexpected blocked result: %+v", result)
	}
	if result.Draft.Status != "blocked" {
		t.Fatalf("expected blocked draft, got %+v", result.Draft)
	}
}

func TestUseCasesDryRunRejectsEmptyInput(t *testing.T) {
	repo := newFakeRepo()
	uc, err := NewUseCases(repo)
	if err != nil {
		t.Fatal(err)
	}

	_, err = uc.DryRun(context.Background(), "tenant-1", uuid.New(), " ")
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation for empty input, got %v", err)
	}
}

func TestUseCasesExecutionGateBlocksCreateBelowExecutionAutonomy(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA2)

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil)
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.DryRun.Draft.Status != "ready" {
		t.Fatalf("expected ready draft, got %+v", result.DryRun.Draft)
	}
	if result.Gate.Decision != "blocked" || result.Gate.RequiredExecutionAutonomy != domain.AutonomyA3 || result.Gate.VirployeeAutonomy != domain.AutonomyA2 {
		t.Fatalf("unexpected execution gate: %+v", result.Gate)
	}
}

func TestUseCasesExecutionGateFailsClosedWithoutGovernance(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil)
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.Gate.Decision != "blocked" || result.Gate.WillExecute {
		t.Fatalf("unexpected execution gate: %+v", result.Gate)
	}
	assertExecutionGateCheck(t, result.Gate.Checks, "governance_check", executiongate.CheckStatusBlocked)
	repo := uc.repo.(*fakeRepo)
	if len(repo.traces) != 1 || repo.traces[0].NexusResult == nil || repo.traces[0].NexusResult.Available {
		t.Fatalf("expected unavailable governance trace, got %+v", repo.traces)
	}
}

func TestUseCasesExecutionGateChecksGovernanceWhenLocalGatePasses(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	checker := &fakeGovernanceChecker{
		result: executiongate.GovernanceCheckResult{
			Decision:       "allow",
			RiskLevel:      "medium",
			Status:         "allowed",
			DecisionReason: "default medium risk action",
		},
	}
	uc.SetGovernanceChecker(checker)

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil)
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.Gate.Decision != "pass" {
		t.Fatalf("expected governance allow to pass, got %+v", result.Gate)
	}
	if checker.last.TenantID != "tenant-1" || checker.last.ActionType != "calendar.events.create" || checker.last.RequesterID != created.ID.String() {
		t.Fatalf("unexpected governance input: %+v", checker.last)
	}
	if checker.last.BindingHash == "" {
		t.Fatal("expected governance input binding hash")
	}
	assertExecutionGateCheck(t, result.Gate.Checks, "governance_check", executiongate.CheckStatusPass)
	repo := uc.repo.(*fakeRepo)
	if len(repo.traces) != 1 || repo.traces[0].Operation != runtraces.OperationExecutionGate || repo.traces[0].BindingHash == "" {
		t.Fatalf("expected execution gate trace with binding hash, got %+v", repo.traces)
	}
	if repo.traces[0].NexusResult == nil || repo.traces[0].NexusResult.Decision != "allow" {
		t.Fatalf("expected nexus result trace, got %+v", repo.traces[0].NexusResult)
	}
}

func TestUseCasesExecutionGateBlocksWhenGovernanceRequiresApproval(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.SetGovernanceChecker(&fakeGovernanceChecker{
		result: executiongate.GovernanceCheckResult{
			Decision:             "require_approval",
			RiskLevel:            "high",
			Status:               "pending_approval",
			DecisionReason:       "default high risk action",
			WouldRequireApproval: true,
			ApprovalID:           "approval-1",
			ApprovalStatus:       "pending",
		},
	})

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil)
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.Gate.Decision != "blocked" {
		t.Fatalf("expected governance to block, got %+v", result.Gate)
	}
	assertExecutionGateCheck(t, result.Gate.Checks, "governance_check", executiongate.CheckStatusBlocked)
	repo := uc.repo.(*fakeRepo)
	if len(repo.traces) != 1 || repo.traces[0].NexusResult == nil || repo.traces[0].NexusResult.ApprovalID != "approval-1" || repo.traces[0].NexusResult.ApprovalStatus != "pending" {
		t.Fatalf("expected approval metadata in trace, got %+v", repo.traces)
	}
}

func TestUseCasesExecutionGateBlocksWhenGovernanceDenies(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.SetGovernanceChecker(&fakeGovernanceChecker{
		result: executiongate.GovernanceCheckResult{
			Decision:       "deny",
			RiskLevel:      "medium",
			Status:         "denied",
			DecisionReason: "action type is disabled",
		},
	})

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil)
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.Gate.Decision != "blocked" {
		t.Fatalf("expected governance deny to block, got %+v", result.Gate)
	}
	assertExecutionGateCheck(t, result.Gate.Checks, "governance_check", executiongate.CheckStatusBlocked)
	repo := uc.repo.(*fakeRepo)
	if len(repo.traces) != 1 || repo.traces[0].NexusResult == nil || repo.traces[0].NexusResult.Decision != "deny" || repo.traces[0].NexusResult.Status != "denied" {
		t.Fatalf("expected deny nexus result in trace, got %+v", repo.traces)
	}
	if repo.traces[0].NexusResult.ApprovalID != "" || repo.traces[0].NexusResult.ApprovalStatus != "" {
		t.Fatalf("deny must not carry approval metadata, got %+v", repo.traces[0].NexusResult)
	}
}

func TestUseCasesExecutionGateBlocksWhenGovernanceUnavailable(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.SetGovernanceChecker(&fakeGovernanceChecker{err: errors.New("nexus unavailable")})

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil)
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.Gate.Decision != "blocked" {
		t.Fatalf("expected unavailable governance to block, got %+v", result.Gate)
	}
	assertExecutionGateCheck(t, result.Gate.Checks, "governance_check", executiongate.CheckStatusBlocked)
	repo := uc.repo.(*fakeRepo)
	if len(repo.traces) != 1 || repo.traces[0].NexusResult == nil || repo.traces[0].NexusResult.Available {
		t.Fatalf("expected unavailable nexus trace, got %+v", repo.traces)
	}
}

func TestUseCasesSimulateApprovedExecutionCreatesIdempotentTrace(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	approvalID := uuid.New()
	uc.SetGovernanceChecker(&fakeGovernanceChecker{
		result: executiongate.GovernanceCheckResult{
			Decision:             "require_approval",
			RiskLevel:            "high",
			Status:               "pending_approval",
			DecisionReason:       "default high risk action",
			WouldRequireApproval: true,
			ApprovalID:           approvalID.String(),
			ApprovalStatus:       "pending",
		},
	})
	if _, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil); err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	repo := uc.repo.(*fakeRepo)
	bindingHash := repo.traces[0].BindingHash
	uc.SetApprovalReader(&fakeApprovalReader{approval: executiongate.GovernanceApproval{
		ID:          approvalID.String(),
		RequesterID: created.ID.String(),
		BindingHash: bindingHash,
		Status:      "approved",
	}})

	trace, err := uc.SimulateApprovedExecution(context.Background(), "tenant-1", created.ID, approvalID)
	if err != nil {
		t.Fatalf("SimulateApprovedExecution: %v", err)
	}
	if trace.Operation != runtraces.OperationSimulatedExecution || trace.ExecutionResult == nil {
		t.Fatalf("expected simulated execution trace, got %+v", trace)
	}
	if trace.ExecutionResult.Status != "simulated_executed" || trace.ExecutionResult.ExternalEffects {
		t.Fatalf("unexpected execution result: %+v", trace.ExecutionResult)
	}
	if trace.BindingHash != bindingHash || trace.NexusResult == nil || trace.NexusResult.ApprovalStatus != "approved" {
		t.Fatalf("expected approved binding trace, got %+v", trace)
	}

	replayed, err := uc.SimulateApprovedExecution(context.Background(), "tenant-1", created.ID, approvalID)
	if err != nil {
		t.Fatalf("replay SimulateApprovedExecution: %v", err)
	}
	if replayed.ID != trace.ID {
		t.Fatalf("expected idempotent replay of trace %s, got %s", trace.ID, replayed.ID)
	}
	if len(repo.traces) != 2 {
		t.Fatalf("expected execution gate and one simulated trace, got %+v", repo.traces)
	}
}

func TestUseCasesSimulateApprovedExecutionRejectsPendingApproval(t *testing.T) {
	uc, created, approvalID, bindingHash := setupApprovedExecutionGateTrace(t)
	uc.SetApprovalReader(&fakeApprovalReader{approval: executiongate.GovernanceApproval{
		ID:          approvalID.String(),
		RequesterID: created.ID.String(),
		BindingHash: bindingHash,
		Status:      "pending",
	}})

	_, err := uc.SimulateApprovedExecution(context.Background(), "tenant-1", created.ID, approvalID)
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict for pending approval, got %v", err)
	}
}

func TestUseCasesSimulateApprovedExecutionRejectsBindingMismatch(t *testing.T) {
	uc, created, approvalID, _ := setupApprovedExecutionGateTrace(t)
	uc.SetApprovalReader(&fakeApprovalReader{approval: executiongate.GovernanceApproval{
		ID:          approvalID.String(),
		RequesterID: created.ID.String(),
		BindingHash: "different-binding",
		Status:      "approved",
	}})

	_, err := uc.SimulateApprovedExecution(context.Background(), "tenant-1", created.ID, approvalID)
	if !domainerr.IsConflict(err) {
		t.Fatalf("expected conflict for binding mismatch, got %v", err)
	}
}

func TestUseCasesExecutionGateUsesConfirmedDraft(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	uc.SetGovernanceChecker(&fakeGovernanceChecker{result: executiongate.GovernanceCheckResult{
		Decision: "allow",
		Status:   "allowed",
	}})

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión para mañana", &executiongate.ConfirmedDraft{
		Action: "calendar.events.create",
		Kind:   "calendar_event",
		Fields: []executiongate.ConfirmedDraftField{
			{Key: "title", Value: "Reunión"},
			{Key: "date", Value: "2026-07-12"},
			{Key: "time", Value: "15:00"},
			{Key: "timezone", Value: "America/Argentina/Buenos_Aires"},
			{Key: "duration_minutes", Value: "60"},
			{Key: "attendees", Value: "ana@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.DryRun.Draft.Status != "ready" || result.Gate.Decision != "pass" {
		t.Fatalf("expected confirmed draft to pass gate, got draft=%+v gate=%+v", result.DryRun.Draft, result.Gate)
	}
}

func TestUseCasesExecutionGateBlocksIncompleteConfirmedDraft(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)

	result, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión para mañana", &executiongate.ConfirmedDraft{
		Action: "calendar.events.create",
		Kind:   "calendar_event",
		Fields: []executiongate.ConfirmedDraftField{
			{Key: "title", Value: "Reunión"},
		},
	})
	if err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	if result.DryRun.Draft.Status != "needs_input" || result.Gate.Decision != "blocked" {
		t.Fatalf("expected incomplete confirmed draft to block gate, got draft=%+v gate=%+v", result.DryRun.Draft, result.Gate)
	}
}

func TestUseCasesExecutionGateRejectsConfirmedDraftActionMismatch(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)

	_, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión para mañana", &executiongate.ConfirmedDraft{
		Action: "calendar.events.read",
		Kind:   "calendar_event",
	})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestUseCasesListRunsReturnsLatestForVirployee(t *testing.T) {
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)

	if _, err := uc.DryRun(context.Background(), "tenant-1", created.ID, "Agendá una reunión para mañana"); err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if _, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil); err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}

	runs, err := uc.ListRuns(context.Background(), "tenant-1", created.ID, 20)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || runs[0].Operation != runtraces.OperationExecutionGate || runs[1].Operation != runtraces.OperationDryRun {
		t.Fatalf("unexpected runs: %+v", runs)
	}
}

func setupApprovedExecutionGateTrace(t *testing.T) (*UseCases, domain.Virployee, uuid.UUID, string) {
	t.Helper()
	uc, created := setupExecutionGateUseCase(t, domain.AutonomyA3)
	approvalID := uuid.New()
	uc.SetGovernanceChecker(&fakeGovernanceChecker{
		result: executiongate.GovernanceCheckResult{
			Decision:             "require_approval",
			RiskLevel:            "high",
			Status:               "pending_approval",
			DecisionReason:       "default high risk action",
			WouldRequireApproval: true,
			ApprovalID:           approvalID.String(),
			ApprovalStatus:       "pending",
		},
	})
	if _, err := uc.ExecutionGate(context.Background(), "tenant-1", created.ID, "Agendá una reunión mañana a las 15 con ana@example.com", nil); err != nil {
		t.Fatalf("ExecutionGate: %v", err)
	}
	repo := uc.repo.(*fakeRepo)
	return uc, created, approvalID, repo.traces[0].BindingHash
}

func setupExecutionGateUseCase(t *testing.T, autonomy domain.AutonomyLevel) (*UseCases, domain.Virployee) {
	t.Helper()
	repo := newFakeRepo()
	jobRoleID := uuid.New()
	profileTemplateID := uuid.New()
	capabilityID := uuid.New()
	uc, err := NewUseCases(repo, &fakeJobRoleReader{
		role: jobroledomain.JobRole{ID: jobRoleID, TenantID: "tenant-1", Name: "Receptionist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc.SetProfileTemplateReader(&fakeProfileTemplateReader{
		profile: profiletemplatedomain.ProfileTemplate{
			ID:           profileTemplateID,
			TenantID:     "tenant-1",
			Name:         "Receptionist profile",
			SystemPrompt: "Be warm.",
			MaxAutonomy:  autonomy,
		},
	})
	uc.SetCapabilityValidator(&fakeCapabilityReader{
		rows: map[uuid.UUID]capabilitydomain.Capability{
			capabilityID: {
				ID:               capabilityID,
				TenantID:         "tenant-1",
				CapabilityKey:    "calendar.events.create",
				Name:             "Create calendar events",
				RequiredAutonomy: domain.AutonomyA2,
			},
		},
	})
	created, err := uc.Create(context.Background(), "tenant-1", domain.CreateInput{
		Name:              "Sofia",
		JobRoleID:         jobRoleID.String(),
		ProfileTemplateID: profileTemplateID.String(),
		CapabilityIDs:     []string{capabilityID.String()},
		SupervisorUserID:  "dev-user",
		Autonomy:          string(autonomy),
	})
	if err != nil {
		t.Fatal(err)
	}
	return uc, created
}

func assertListLen(t *testing.T, fn func(context.Context, string) ([]domain.Virployee, error), want int) {
	t.Helper()
	got, err := fn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != want {
		t.Fatalf("expected %d rows, got %d: %+v", want, len(got), got)
	}
}

func assertExecutionGateCheck(t *testing.T, checks []executiongate.Check, key string, status executiongate.CheckStatus) {
	t.Helper()
	for _, check := range checks {
		if check.Key != key {
			continue
		}
		if check.Status != status {
			t.Fatalf("expected %s check %s, got %+v", key, status, check)
		}
		return
	}
	t.Fatalf("missing check %s in %+v", key, checks)
}

type fakeRepo struct {
	rows   map[uuid.UUID]domain.Virployee
	traces []runtraces.Trace
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: make(map[uuid.UUID]domain.Virployee)}
}

func (r *fakeRepo) Create(_ context.Context, _ string, input domain.NormalizedCreateInput) (domain.Virployee, error) {
	now := time.Now().UTC()
	row := domain.Virployee{
		ID:                uuid.New(),
		Name:              input.Name,
		JobRoleID:         input.JobRoleID,
		ProfileTemplateID: input.ProfileTemplateID,
		CapabilityIDs:     input.CapabilityIDs,
		Description:       input.Description,
		SupervisorUserID:  input.SupervisorUserID,
		Autonomy:          input.Autonomy,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	r.rows[row.ID] = row
	return row, nil
}

func (r *fakeRepo) List(_ context.Context, _ string, state domain.State) ([]domain.Virployee, error) {
	out := []domain.Virployee{}
	for _, row := range r.rows {
		if row.State() == state {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeRepo) Get(_ context.Context, _ string, id uuid.UUID) (domain.Virployee, error) {
	row, ok := r.rows[id]
	if !ok {
		return domain.Virployee{}, domainerr.NotFoundf("virployee", id.String())
	}
	return row, nil
}

func (r *fakeRepo) Update(_ context.Context, _ string, id uuid.UUID, input domain.NormalizedUpdateInput) (domain.Virployee, error) {
	row, ok := r.rows[id]
	if !ok {
		return domain.Virployee{}, domainerr.NotFoundf("virployee", id.String())
	}
	if row.State() != domain.StateActive {
		return domain.Virployee{}, domainerr.Conflict("virployee is not active")
	}
	row.Name = input.Name
	row.JobRoleID = input.JobRoleID
	row.ProfileTemplateID = input.ProfileTemplateID
	row.CapabilityIDs = input.CapabilityIDs
	row.Description = input.Description
	row.SupervisorUserID = input.SupervisorUserID
	row.Autonomy = input.Autonomy
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return row, nil
}

func (r *fakeRepo) Archive(_ context.Context, _ string, id uuid.UUID, at time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateActive {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = &at
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Unarchive(_ context.Context, _ string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateArchived {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Trash(_ context.Context, _ string, id uuid.UUID, at time.Time, purgeAfter *time.Time) error {
	row, ok := r.rows[id]
	if !ok || row.State() == domain.StateTrashed {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.ArchivedAt = nil
	row.TrashedAt = &at
	row.PurgeAfter = purgeAfter
	row.UpdatedAt = at
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Restore(_ context.Context, _ string, id uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.State() != domain.StateTrashed {
		return domainerr.NotFoundf("virployee", id.String())
	}
	row.TrashedAt = nil
	row.PurgeAfter = nil
	row.UpdatedAt = time.Now().UTC()
	r.rows[id] = row
	return nil
}

func (r *fakeRepo) Purge(_ context.Context, _ string, id uuid.UUID) error {
	if _, ok := r.rows[id]; !ok {
		return domainerr.NotFoundf("virployee", id.String())
	}
	delete(r.rows, id)
	return nil
}

func (r *fakeRepo) IsArchived(_ context.Context, _ string, id uuid.UUID) (bool, error) {
	row, ok := r.rows[id]
	if !ok {
		return false, domainerr.NotFoundf("virployee", id.String())
	}
	return row.State() == domain.StateArchived, nil
}

func (r *fakeRepo) State(_ context.Context, _ string, id uuid.UUID) (lifecycle.LifecycleState, error) {
	row, ok := r.rows[id]
	if !ok {
		return "", domainerr.NotFoundf("virployee", id.String())
	}
	return lifecycleState(row.State()), nil
}

func (r *fakeRepo) CreateRunTrace(_ context.Context, tenantID string, input runtraces.CreateInput) (runtraces.Trace, error) {
	now := time.Now().UTC()
	intent := input.Intent
	if intent == nil {
		intent = map[string]any{}
	}
	checks := input.GateChecks
	if checks == nil {
		checks = []runtraces.GateCheck{}
	}
	trace := runtraces.Trace{
		ID:              uuid.New(),
		TenantID:        tenantID,
		VirployeeID:     input.VirployeeID,
		Operation:       input.Operation,
		InputHash:       runtraces.HashString(input.Input),
		InputPreview:    runtraces.InputPreview(input.Input),
		Intent:          intent,
		CapabilityID:    input.CapabilityID,
		CapabilityKey:   input.CapabilityKey,
		DryRunDecision:  input.DryRunDecision,
		GateDecision:    input.GateDecision,
		GateChecks:      checks,
		NexusResult:     input.NexusResult,
		ExecutionResult: input.ExecutionResult,
		BindingHash:     input.BindingHash,
		CreatedAt:       now,
	}
	if input.InputHash != "" {
		trace.InputHash = input.InputHash
	}
	if input.InputPreview != "" {
		trace.InputPreview = input.InputPreview
	}
	r.traces = append(r.traces, trace)
	return trace, nil
}

func (r *fakeRepo) ListRunTraces(_ context.Context, tenantID string, virployeeID uuid.UUID, limit int) ([]runtraces.Trace, error) {
	out := []runtraces.Trace{}
	for i := len(r.traces) - 1; i >= 0; i-- {
		trace := r.traces[i]
		if trace.TenantID != tenantID || trace.VirployeeID != virployeeID {
			continue
		}
		out = append(out, trace)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *fakeRepo) FindExecutionGateTraceByApproval(_ context.Context, tenantID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error) {
	for i := len(r.traces) - 1; i >= 0; i-- {
		trace := r.traces[i]
		if trace.TenantID == tenantID &&
			trace.VirployeeID == virployeeID &&
			trace.Operation == runtraces.OperationExecutionGate &&
			trace.NexusResult != nil &&
			trace.NexusResult.ApprovalID == approvalID {
			return trace, nil
		}
	}
	return runtraces.Trace{}, domainerr.NotFound("run trace not found")
}

func (r *fakeRepo) FindSimulatedExecutionTraceByApproval(_ context.Context, tenantID string, virployeeID uuid.UUID, approvalID string) (runtraces.Trace, error) {
	for i := len(r.traces) - 1; i >= 0; i-- {
		trace := r.traces[i]
		if trace.TenantID == tenantID &&
			trace.VirployeeID == virployeeID &&
			trace.Operation == runtraces.OperationSimulatedExecution &&
			trace.ExecutionResult != nil &&
			trace.ExecutionResult.ApprovalID == approvalID {
			return trace, nil
		}
	}
	return runtraces.Trace{}, domainerr.NotFound("run trace not found")
}

func lifecycleState(state domain.State) lifecycle.LifecycleState {
	switch state {
	case domain.StateArchived:
		return lifecycle.StateArchived
	case domain.StateTrashed:
		return lifecycle.StateTrashed
	default:
		return lifecycle.StateActive
	}
}

type fakeJobRoleReader struct {
	lastTenant string
	lastID     uuid.UUID
	role       jobroledomain.JobRole
	err        error
}

func (r *fakeJobRoleReader) EnsureActive(_ context.Context, tenantID string, id uuid.UUID) error {
	r.lastTenant = tenantID
	r.lastID = id
	if r.err != nil {
		return r.err
	}
	return nil
}

func (r *fakeJobRoleReader) Get(_ context.Context, tenantID string, id uuid.UUID) (jobroledomain.JobRole, error) {
	if r.err != nil {
		return jobroledomain.JobRole{}, r.err
	}
	role := r.role
	if role.ID == uuid.Nil {
		role = jobroledomain.JobRole{ID: id, TenantID: tenantID, Name: "Job Role"}
	}
	if role.ID != id || role.TenantID != tenantID || role.State() == jobroledomain.StateTrashed {
		return jobroledomain.JobRole{}, domainerr.NotFoundf("job_role", id.String())
	}
	return role, nil
}

type fakeProfileTemplateReader struct {
	lastTenant   string
	lastID       uuid.UUID
	lastAutonomy domain.AutonomyLevel
	profile      profiletemplatedomain.ProfileTemplate
	err          error
}

func (v *fakeProfileTemplateReader) EnsureUsableByVirployee(_ context.Context, tenantID string, id uuid.UUID, autonomy domain.AutonomyLevel) error {
	v.lastTenant = tenantID
	v.lastID = id
	v.lastAutonomy = autonomy
	if v.err != nil {
		return v.err
	}
	return nil
}

func (v *fakeProfileTemplateReader) Get(_ context.Context, tenantID string, id uuid.UUID) (profiletemplatedomain.ProfileTemplate, error) {
	if v.err != nil {
		return profiletemplatedomain.ProfileTemplate{}, v.err
	}
	profile := v.profile
	if profile.ID == uuid.Nil {
		profile = profiletemplatedomain.ProfileTemplate{
			ID:           id,
			TenantID:     tenantID,
			Name:         "Profile",
			SystemPrompt: "Prompt.",
			MaxAutonomy:  domain.AutonomyA5,
		}
	}
	if profile.ID != id || profile.TenantID != tenantID || profile.State() == profiletemplatedomain.StateTrashed {
		return profiletemplatedomain.ProfileTemplate{}, domainerr.NotFoundf("profile_template", id.String())
	}
	return profile, nil
}

type fakeCapabilityReader struct {
	rows map[uuid.UUID]capabilitydomain.Capability
	err  error
}

func (r *fakeCapabilityReader) EnsureAssignable(context.Context, string, []uuid.UUID, domain.AutonomyLevel) error {
	return nil
}

func (r *fakeCapabilityReader) Get(_ context.Context, tenantID string, id uuid.UUID) (capabilitydomain.Capability, error) {
	if r.err != nil {
		return capabilitydomain.Capability{}, r.err
	}
	row, ok := r.rows[id]
	if !ok || row.TenantID != tenantID || row.State() == capabilitydomain.StateTrashed {
		return capabilitydomain.Capability{}, domainerr.NotFoundf("capability", id.String())
	}
	return row, nil
}

type fakeGovernanceChecker struct {
	result executiongate.GovernanceCheckResult
	err    error
	last   executiongate.GovernanceCheckInput
}

type fakeApprovalReader struct {
	approval executiongate.GovernanceApproval
	err      error
}

func (r *fakeApprovalReader) GetApproval(context.Context, string, uuid.UUID) (executiongate.GovernanceApproval, error) {
	if r.err != nil {
		return executiongate.GovernanceApproval{}, r.err
	}
	return r.approval, nil
}

func (c *fakeGovernanceChecker) Check(_ context.Context, input executiongate.GovernanceCheckInput) (executiongate.GovernanceCheckResult, error) {
	c.last = input
	if c.err != nil {
		return executiongate.GovernanceCheckResult{}, c.err
	}
	return c.result, nil
}
