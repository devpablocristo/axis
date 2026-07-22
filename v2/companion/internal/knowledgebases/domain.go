package knowledgebases

import (
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	ScopeProfessional = "professional"
	ScopeVirployee    = "virployee"
	ScopeSubject      = "subject"
	ScopeCase         = "case"

	ClassificationProfessional  = "professional"
	ClassificationPrivate       = "private"
	ProfessionalArtifactSubject = "professional"
	KnowledgeProductSurface     = "knowledge_base"
)

var stableReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
var connectorTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type KnowledgeBase struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Classification string     `json:"classification"`
	State          string     `json:"state"`
	Version        int64      `json:"version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

type ArtifactScope struct {
	VirployeeID          uuid.UUID `json:"virployee_id"`
	ProductSurface       string    `json:"product_surface"`
	SubjectID            string    `json:"subject_id"`
	RepositoryGeneration string    `json:"repository_generation"`
	DocumentID           string    `json:"document_id"`
}

type Document struct {
	ID              uuid.UUID     `json:"id"`
	KnowledgeBaseID uuid.UUID     `json:"knowledge_base_id"`
	Title           string        `json:"title"`
	ArtifactScope   ArtifactScope `json:"artifact_scope"`
	SourceVersion   string        `json:"source_version"`
	SourceSHA256    string        `json:"source_sha256"`
	State           string        `json:"state"`
	Version         int64         `json:"version"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	ArchivedAt      *time.Time    `json:"archived_at,omitempty"`
}

type Binding struct {
	ID              uuid.UUID  `json:"id"`
	KnowledgeBaseID uuid.UUID  `json:"knowledge_base_id"`
	ScopeType       string     `json:"scope_type"`
	JobRoleID       *uuid.UUID `json:"job_role_id,omitempty"`
	VirployeeID     *uuid.UUID `json:"virployee_id,omitempty"`
	SubjectID       string     `json:"subject_id,omitempty"`
	CaseID          *uuid.UUID `json:"case_id,omitempty"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type VirployeeKnowledgeBase struct {
	KnowledgeBase KnowledgeBase `json:"knowledge_base"`
	Bindings      []Binding     `json:"bindings"`
}

type SetVirployeeKnowledgeBaseInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Enabled         bool   `json:"enabled"`
}

type CreateInput struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Classification string `json:"classification"`
}

type UpdateInput struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	ExpectedVersion int64  `json:"expected_version"`
}

type RegisterDocumentInput struct {
	Title         string        `json:"title"`
	ArtifactScope ArtifactScope `json:"artifact_scope"`
}

// IngestionTargetInput is the caller-controlled portion of an artifact scope.
// Product surface and repository generation are server-owned so a connector or
// upload cannot collide with an Assist repository or mutate an older version.
type IngestionTargetInput struct {
	VirployeeID string `json:"virployee_id"`
	SubjectID   string `json:"subject_id"`
	DocumentID  string `json:"document_id"`
}

type ConnectorSourceInput struct {
	Connector  string `json:"connector"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	ReadURL    string `json:"read_url"`
	SHA256     string `json:"sha256"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
}

// ConnectorIngestionInput is intentionally provider-neutral. Connectors issue
// an authorized, short-lived read_url, while only stable source identity and
// verified content provenance are retained.
type ConnectorIngestionInput struct {
	Title  string               `json:"title"`
	Target IngestionTargetInput `json:"target"`
	Source ConnectorSourceInput `json:"source"`
}

type UploadIngestionInput struct {
	Title       string
	Target      IngestionTargetInput
	Name        string
	ContentType string
	Content     io.Reader
}

type ingestionTarget struct {
	VirployeeID uuid.UUID
	SubjectID   string
	DocumentID  string
}

type BindingInput struct {
	ScopeType   string `json:"scope_type"`
	JobRoleID   string `json:"job_role_id,omitempty"`
	VirployeeID string `json:"virployee_id,omitempty"`
	SubjectID   string `json:"subject_id,omitempty"`
	CaseID      string `json:"case_id,omitempty"`
}

type ReplaceBindingsInput struct {
	ExpectedVersion int64          `json:"expected_version"`
	Bindings        []BindingInput `json:"bindings"`
}

type RetrievalScope struct {
	TenantID    string
	VirployeeID uuid.UUID
	SubjectID   string
	CaseID      uuid.UUID
}

// Citation is the canonical, Companion-produced source reference. Locator is
// derived from an indexed chunk; callers cannot inject it into the catalog.
type Citation struct {
	KnowledgeBaseID *uuid.UUID      `json:"knowledge_base_id,omitempty"`
	DocumentID      string          `json:"document_id"`
	SourceVersion   string          `json:"source_version"`
	SHA256          string          `json:"sha256"`
	Locator         json.RawMessage `json:"locator,omitempty"`
}

type Evidence struct {
	Parts     []artifacts.ContentPart
	Citations []Citation
}

func normalizeBase(in CreateInput) (CreateInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Classification = strings.ToLower(strings.TrimSpace(in.Classification))
	if in.Classification == "" {
		in.Classification = ClassificationPrivate
	}
	if in.Name == "" || len([]rune(in.Name)) > 200 {
		return in, domainerr.Validation("name is required and must not exceed 200 characters")
	}
	if len([]rune(in.Description)) > 2000 {
		return in, domainerr.Validation("description must not exceed 2000 characters")
	}
	if in.Classification != ClassificationProfessional && in.Classification != ClassificationPrivate {
		return in, domainerr.Validation("classification must be professional or private")
	}
	return in, nil
}

func normalizeArtifactScope(in ArtifactScope) (ArtifactScope, error) {
	in.ProductSurface = strings.TrimSpace(in.ProductSurface)
	in.SubjectID = strings.TrimSpace(in.SubjectID)
	in.RepositoryGeneration = strings.TrimSpace(in.RepositoryGeneration)
	in.DocumentID = strings.TrimSpace(in.DocumentID)
	if in.VirployeeID == uuid.Nil || in.ProductSurface == "" || in.SubjectID == "" || in.RepositoryGeneration == "" || in.DocumentID == "" {
		return in, domainerr.Validation("artifact_scope must include virployee_id, product_surface, subject_id, repository_generation, and document_id")
	}
	return in, nil
}

func normalizeIngestionTarget(in IngestionTargetInput) (ingestionTarget, error) {
	virployeeID, err := uuid.Parse(strings.TrimSpace(in.VirployeeID))
	if err != nil || virployeeID == uuid.Nil {
		return ingestionTarget{}, domainerr.Validation("target.virployee_id must be a valid UUID")
	}
	subjectID := strings.TrimSpace(in.SubjectID)
	if subjectID == "" || len(subjectID) > 200 {
		return ingestionTarget{}, domainerr.Validation("target.subject_id is required and must not exceed 200 characters")
	}
	documentID := strings.TrimSpace(in.DocumentID)
	if !stableReferencePattern.MatchString(documentID) {
		return ingestionTarget{}, domainerr.Validation("target.document_id must be a stable identifier using letters, numbers, dot, underscore, or dash")
	}
	return ingestionTarget{VirployeeID: virployeeID, SubjectID: subjectID, DocumentID: documentID}, nil
}

func normalizeConnectorIngestion(in ConnectorIngestionInput) (ConnectorIngestionInput, ingestionTarget, error) {
	target, err := normalizeIngestionTarget(in.Target)
	if err != nil {
		return in, ingestionTarget{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Source.Connector = strings.ToLower(strings.TrimSpace(in.Source.Connector))
	in.Source.ExternalID = strings.TrimSpace(in.Source.ExternalID)
	in.Source.Name = strings.TrimSpace(in.Source.Name)
	in.Source.ReadURL = strings.TrimSpace(in.Source.ReadURL)
	in.Source.SHA256 = strings.ToLower(strings.TrimSpace(in.Source.SHA256))
	in.Source.MIMEType = strings.ToLower(strings.TrimSpace(in.Source.MIMEType))
	if len([]rune(in.Title)) > 300 {
		return in, ingestionTarget{}, domainerr.Validation("title must not exceed 300 characters")
	}
	if !connectorTypePattern.MatchString(in.Source.Connector) {
		return in, ingestionTarget{}, domainerr.Validation("source.connector must be a stable lowercase connector identifier")
	}
	if in.Source.ExternalID == "" || len([]rune(in.Source.ExternalID)) > 500 || strings.ContainsAny(in.Source.ExternalID, "\r\n\x00") {
		return in, ingestionTarget{}, domainerr.Validation("source.external_id is required and must be a safe stable identifier")
	}
	if !safeArtifactName(in.Source.Name) {
		return in, ingestionTarget{}, domainerr.Validation("source.name is required and must not exceed 300 characters")
	}
	parsedURL, parseErr := url.Parse(in.Source.ReadURL)
	if parseErr != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.User != nil {
		return in, ingestionTarget{}, domainerr.Validation("source.read_url must be an authorized HTTP(S) URL without user info")
	}
	if len(in.Source.ReadURL) > 8192 {
		return in, ingestionTarget{}, domainerr.Validation("source.read_url is too long")
	}
	if !sha256Pattern.MatchString(in.Source.SHA256) {
		return in, ingestionTarget{}, domainerr.Validation("source.sha256 must be a lowercase SHA-256 digest")
	}
	if in.Source.MIMEType == "" || len(in.Source.MIMEType) > 200 {
		return in, ingestionTarget{}, domainerr.Validation("source.mime_type is required and must not exceed 200 characters")
	}
	if in.Source.SizeBytes <= 0 || in.Source.SizeBytes > artifacts.MaxArtifactBytes {
		return in, ingestionTarget{}, domainerr.Validation("source.size_bytes must be within the artifact size limit")
	}
	return in, target, nil
}

func normalizeUploadIngestion(in UploadIngestionInput) (UploadIngestionInput, ingestionTarget, error) {
	target, err := normalizeIngestionTarget(in.Target)
	if err != nil {
		return in, ingestionTarget{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Name = strings.TrimSpace(in.Name)
	in.ContentType = strings.TrimSpace(in.ContentType)
	if len([]rune(in.Title)) > 300 {
		return in, ingestionTarget{}, domainerr.Validation("title must not exceed 300 characters")
	}
	if !safeArtifactName(in.Name) {
		return in, ingestionTarget{}, domainerr.Validation("uploaded file name is required and must not exceed 300 characters")
	}
	if len(in.ContentType) > 200 {
		return in, ingestionTarget{}, domainerr.Validation("uploaded content type must not exceed 200 characters")
	}
	if in.Content == nil {
		return in, ingestionTarget{}, domainerr.Validation("file is required")
	}
	return in, target, nil
}

func safeArtifactName(value string) bool {
	return value != "" && value != "." && value != "/" && value != "\\" &&
		len([]rune(value)) <= 300 && !strings.ContainsAny(value, "\r\n\x00")
}

func normalizeBinding(in BindingInput) (Binding, error) {
	out := Binding{ID: uuid.New(), ScopeType: strings.ToLower(strings.TrimSpace(in.ScopeType)), SubjectID: strings.TrimSpace(in.SubjectID), Version: 1}
	parse := func(raw, field string) (*uuid.UUID, error) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, nil
		}
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, domainerr.Validation(field + " must be a valid UUID")
		}
		return &id, nil
	}
	var err error
	if out.JobRoleID, err = parse(in.JobRoleID, "job_role_id"); err != nil {
		return Binding{}, err
	}
	if out.VirployeeID, err = parse(in.VirployeeID, "virployee_id"); err != nil {
		return Binding{}, err
	}
	if out.CaseID, err = parse(in.CaseID, "case_id"); err != nil {
		return Binding{}, err
	}
	switch out.ScopeType {
	case ScopeProfessional:
		if out.JobRoleID == nil || out.VirployeeID != nil || out.SubjectID != "" || out.CaseID != nil {
			return Binding{}, domainerr.Validation("professional binding requires only job_role_id")
		}
	case ScopeVirployee:
		if out.JobRoleID != nil || out.VirployeeID == nil || out.SubjectID != "" || out.CaseID != nil {
			return Binding{}, domainerr.Validation("virployee binding requires only virployee_id")
		}
	case ScopeSubject:
		if out.JobRoleID != nil || out.VirployeeID == nil || out.SubjectID == "" || out.CaseID != nil {
			return Binding{}, domainerr.Validation("subject binding requires virployee_id and subject_id")
		}
		subjectID, err := uuid.Parse(out.SubjectID)
		if err != nil || subjectID == uuid.Nil {
			return Binding{}, domainerr.Validation("subject_id must reference a work subject UUID")
		}
		out.SubjectID = subjectID.String()
	case ScopeCase:
		if out.JobRoleID != nil || out.VirployeeID == nil || out.SubjectID == "" || out.CaseID == nil {
			return Binding{}, domainerr.Validation("case binding requires virployee_id, subject_id, and case_id")
		}
		subjectID, err := uuid.Parse(out.SubjectID)
		if err != nil || subjectID == uuid.Nil {
			return Binding{}, domainerr.Validation("subject_id must reference a work subject UUID")
		}
		out.SubjectID = subjectID.String()
	default:
		return Binding{}, domainerr.Validation("scope_type must be professional, virployee, subject, or case")
	}
	return out, nil
}

func validateBindingForClassification(classification string, binding Binding) error {
	switch classification {
	case ClassificationProfessional:
		if binding.ScopeType != ScopeProfessional && binding.ScopeType != ScopeVirployee {
			return domainerr.Validation("professional knowledge bases may bind only professional or virployee scope")
		}
	case ClassificationPrivate:
		if binding.ScopeType != ScopeSubject && binding.ScopeType != ScopeCase {
			return domainerr.Validation("private knowledge bases may bind only subject or case scope")
		}
	default:
		return domainerr.Conflict("knowledge base classification is invalid")
	}
	return nil
}

func validateBindingsForClassification(classification string, bindings []Binding) error {
	if classification == ClassificationPrivate && len(bindings) != 1 {
		return domainerr.Validation("a private knowledge base requires exactly one subject or case binding")
	}
	for _, binding := range bindings {
		if err := validateBindingForClassification(classification, binding); err != nil {
			return err
		}
	}
	return nil
}
