package workforcerouting

import (
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestNormalizeRelationshipsRequiresOnePrimaryEmployer(t *testing.T) {
	employerID := uuid.New()
	patientID := uuid.New()
	got, err := NormalizeRelationships([]RelationshipInput{
		{SubjectID: employerID.String(), Type: " works_for ", IsPrimary: true},
		{SubjectID: patientID.String(), Type: "serves"},
	})
	if err != nil {
		t.Fatalf("NormalizeRelationships: %v", err)
	}
	if len(got) != 2 || got[0].RelationshipType != RelationshipWorksFor || got[0].SubjectID != employerID {
		t.Fatalf("unexpected normalized relationships: %+v", got)
	}

	if _, err := NormalizeRelationships([]RelationshipInput{{SubjectID: patientID.String(), Type: "serves"}}); !domainerr.IsValidation(err) {
		t.Fatalf("expected missing primary employer validation, got %v", err)
	}
	if _, err := NormalizeRelationships([]RelationshipInput{
		{SubjectID: employerID.String(), Type: "works_for", IsPrimary: true},
		{SubjectID: patientID.String(), Type: "reports_to", IsPrimary: true},
	}); !domainerr.IsValidation(err) {
		t.Fatalf("expected invalid primary type validation, got %v", err)
	}
}

func TestNormalizeRoutingInputs(t *testing.T) {
	poolID := uuid.New()
	subjectID := uuid.New()
	got, err := NormalizeResolveInput(ResolveInput{PoolID: poolID.String(), SubjectID: subjectID.String(), CapabilityKey: "medmory.timeline.read", ActorID: " "})
	if err != nil {
		t.Fatalf("NormalizeResolveInput: %v", err)
	}
	if got.PoolID != poolID || got.SubjectID != subjectID || got.CapabilityKey != "clinical.timeline.build" || got.ActorID != "system" {
		t.Fatalf("unexpected resolve input: %+v", got)
	}
	if _, err := NormalizePoolMemberInput(UpsertPoolMemberInput{MaxActiveSubjects: 0}); !domainerr.IsValidation(err) {
		t.Fatalf("expected capacity validation, got %v", err)
	}
	if _, err := NormalizeReassignInput(ReassignInput{VirployeeID: uuid.NewString(), ExpectedVersion: 0, Reason: "load balance"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected assignment version validation, got %v", err)
	}
	if _, err := NormalizeReassignInput(ReassignInput{VirployeeID: uuid.NewString(), ExpectedVersion: 1, Reason: "patient John requested it"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected safe reason code validation, got %v", err)
	}
}

func TestNormalizeWorkSubjectModelsCasesAlongsideAssistCaseRuntime(t *testing.T) {
	caseSubject, err := NormalizeWorkSubjectInput(CreateWorkSubjectInput{Kind: "case", DisplayName: "Treatment 2026"})
	if err != nil || caseSubject.Kind != SubjectKindCase {
		t.Fatalf("expected case work subject, got %+v err=%v", caseSubject, err)
	}
	got, err := NormalizeWorkSubjectInput(CreateWorkSubjectInput{Kind: " patient ", DisplayName: " Patient 17 ", ExternalRef: " ehr-17 "})
	if err != nil {
		t.Fatalf("NormalizeWorkSubjectInput: %v", err)
	}
	if got.Kind != SubjectKindPatient || got.DisplayName != "Patient 17" || got.ExternalRef != "ehr-17" {
		t.Fatalf("unexpected subject normalization: %+v", got)
	}
}
