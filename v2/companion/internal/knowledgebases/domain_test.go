package knowledgebases

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func TestNormalizeBindingRequiresExactScopeShape(t *testing.T) {
	virployeeID := uuid.NewString()
	caseID := uuid.NewString()
	subjectID := uuid.NewString()
	got, err := normalizeBinding(BindingInput{ScopeType: ScopeCase, VirployeeID: virployeeID, SubjectID: " " + subjectID + " ", CaseID: caseID})
	if err != nil {
		t.Fatalf("normalizeBinding: %v", err)
	}
	if got.SubjectID != subjectID || got.VirployeeID == nil || got.CaseID == nil {
		t.Fatalf("unexpected normalized binding: %#v", got)
	}
	if _, err := normalizeBinding(BindingInput{ScopeType: ScopeSubject, VirployeeID: virployeeID}); !domainerr.IsValidation(err) {
		t.Fatalf("expected missing subject_id validation, got %v", err)
	}
	if _, err := normalizeBinding(BindingInput{ScopeType: ScopeSubject, VirployeeID: virployeeID, SubjectID: "patient-a"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected non-work-subject identifier validation, got %v", err)
	}
	if _, err := normalizeBinding(BindingInput{ScopeType: ScopeProfessional, JobRoleID: uuid.NewString(), SubjectID: "patient-a"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected professional scope validation, got %v", err)
	}
}

func TestNormalizeConnectorIngestionRequiresImmutableAuthorizedManifest(t *testing.T) {
	input := ConnectorIngestionInput{
		Title:  "Protocol",
		Target: IngestionTargetInput{VirployeeID: uuid.NewString(), SubjectID: ProfessionalArtifactSubject, DocumentID: "protocol-v7"},
		Source: ConnectorSourceInput{
			Connector: "google_drive", ExternalID: "file-123", Name: "protocol.txt",
			ReadURL: "https://files.example.test/signed/protocol", SHA256: strings.Repeat("a", 64),
			MIMEType: "text/plain", SizeBytes: 42,
		},
	}
	normalized, target, err := normalizeConnectorIngestion(input)
	if err != nil {
		t.Fatalf("normalizeConnectorIngestion: %v", err)
	}
	if target.DocumentID != "protocol-v7" || normalized.Source.Connector != "google_drive" {
		t.Fatalf("unexpected normalized connector ingestion: input=%+v target=%+v", normalized, target)
	}

	badURL := input
	badURL.Source.ReadURL = "ftp://files.example.test/protocol"
	if _, _, err := normalizeConnectorIngestion(badURL); !domainerr.IsValidation(err) {
		t.Fatalf("expected non-HTTP read URL rejection, got %v", err)
	}
	badChecksum := input
	badChecksum.Source.SHA256 = strings.Repeat("z", 64)
	if _, _, err := normalizeConnectorIngestion(badChecksum); !domainerr.IsValidation(err) {
		t.Fatalf("expected non-canonical checksum rejection, got %v", err)
	}
	badDocumentID := input
	badDocumentID.Target.DocumentID = "../../patient-secret"
	if _, _, err := normalizeConnectorIngestion(badDocumentID); !domainerr.IsValidation(err) {
		t.Fatalf("expected unsafe document identity rejection, got %v", err)
	}
}

func TestNormalizeUploadIngestionRequiresTargetAndFile(t *testing.T) {
	input := UploadIngestionInput{
		Target: IngestionTargetInput{VirployeeID: uuid.NewString(), SubjectID: uuid.NewString(), DocumentID: "patient-document"},
		Name:   "result.txt", ContentType: "text/plain", Content: bytes.NewBufferString("result"),
	}
	if _, _, err := normalizeUploadIngestion(input); err != nil {
		t.Fatalf("normalizeUploadIngestion: %v", err)
	}
	input.Content = nil
	if _, _, err := normalizeUploadIngestion(input); !domainerr.IsValidation(err) {
		t.Fatalf("expected missing file rejection, got %v", err)
	}
}

func TestIngestionUseCasesFailClosedWithoutConfiguredPipeline(t *testing.T) {
	usecases := NewUseCases(nil)
	input := ConnectorIngestionInput{
		Target: IngestionTargetInput{VirployeeID: uuid.NewString(), SubjectID: ProfessionalArtifactSubject, DocumentID: "protocol-v7"},
		Source: ConnectorSourceInput{
			Connector: "box", ExternalID: "file-123", Name: "protocol.txt",
			ReadURL: "https://files.example.test/signed/protocol", SHA256: strings.Repeat("a", 64),
			MIMEType: "text/plain", SizeBytes: 42,
		},
	}
	if _, err := usecases.IngestConnector(t.Context(), "tenant-a", "member", uuid.New(), input); !domainerr.IsForbidden(err) {
		t.Fatalf("expected owner/admin authorization, got %v", err)
	}
	if _, err := usecases.IngestConnector(t.Context(), "tenant-a", "owner", uuid.New(), input); !domainerr.IsKind(err, domainerr.KindUnavailable) {
		t.Fatalf("expected fail-closed unavailable pipeline, got %v", err)
	}
}

func TestNormalizeArtifactScopeRequiresCompleteIndexedIdentity(t *testing.T) {
	_, err := normalizeArtifactScope(ArtifactScope{VirployeeID: uuid.New(), ProductSurface: "medmory", SubjectID: "patient-a"})
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation, got %v", err)
	}
}

func TestKnowledgeBaseClassificationDefaultsPrivateAndSeparatesBindingScopes(t *testing.T) {
	base, err := normalizeBase(CreateInput{Name: "Patient repository"})
	if err != nil || base.Classification != ClassificationPrivate {
		t.Fatalf("safe default must be private: base=%+v err=%v", base, err)
	}
	if _, err := normalizeBase(CreateInput{Name: "Invalid", Classification: "shared"}); !domainerr.IsValidation(err) {
		t.Fatalf("expected invalid classification rejection, got %v", err)
	}
	if err := validateBindingForClassification(ClassificationPrivate, Binding{ScopeType: ScopeProfessional}); !domainerr.IsValidation(err) {
		t.Fatalf("private KB must reject professional binding: %v", err)
	}
	if err := validateBindingForClassification(ClassificationProfessional, Binding{ScopeType: ScopeSubject}); !domainerr.IsValidation(err) {
		t.Fatalf("professional KB must reject subject binding: %v", err)
	}
	if err := validateBindingForClassification(ClassificationPrivate, Binding{ScopeType: ScopeCase}); err != nil {
		t.Fatalf("private KB should accept case binding: %v", err)
	}
}

func TestPrivateKnowledgeBaseRequiresOneExclusiveScope(t *testing.T) {
	subject := Binding{ScopeType: ScopeSubject, VirployeeID: ptrUUID(uuid.New()), SubjectID: uuid.NewString()}
	caseBinding := Binding{ScopeType: ScopeCase, VirployeeID: ptrUUID(uuid.New()), SubjectID: subject.SubjectID, CaseID: ptrUUID(uuid.New())}
	if err := validateBindingsForClassification(ClassificationPrivate, nil); !domainerr.IsValidation(err) {
		t.Fatalf("expected an unbound private KB to be rejected on binding replacement, got %v", err)
	}
	if err := validateBindingsForClassification(ClassificationPrivate, []Binding{subject, caseBinding}); !domainerr.IsValidation(err) {
		t.Fatalf("expected mixed subject/case scope rejection, got %v", err)
	}
	secondCase := caseBinding
	secondCase.CaseID = ptrUUID(uuid.New())
	if err := validateBindingsForClassification(ClassificationPrivate, []Binding{caseBinding, secondCase}); !domainerr.IsValidation(err) {
		t.Fatalf("expected multiple case scope rejection, got %v", err)
	}
	if err := validateBindingsForClassification(ClassificationPrivate, []Binding{caseBinding}); err != nil {
		t.Fatalf("one exact case scope must be accepted: %v", err)
	}
}

func ptrUUID(value uuid.UUID) *uuid.UUID { return &value }
