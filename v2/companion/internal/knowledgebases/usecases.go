package knowledgebases

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type ArtifactIngestorPort interface {
	Ingest(context.Context, artifacts.IngestRequest) (artifacts.IngestResult, error)
	IngestUpload(context.Context, artifacts.UploadRequest) (artifacts.IngestResult, error)
}

type UseCases struct {
	repo     *Repository
	ingestor ArtifactIngestorPort
}

func NewUseCases(repo *Repository) *UseCases { return &UseCases{repo: repo} }

func (u *UseCases) SetArtifactIngestor(ingestor ArtifactIngestorPort) { u.ingestor = ingestor }

func authorize(organization, role string) (string, error) {
	organization = strings.TrimSpace(organization)
	if organization == "" {
		return "", domainerr.Validation("organization context is required")
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "admin":
		return organization, nil
	default:
		return "", domainerr.Forbidden("knowledge base management requires an owner or admin")
	}
}

func authorizeRead(organization, role string) (string, error) {
	organization = strings.TrimSpace(organization)
	if organization == "" {
		return "", domainerr.Validation("organization context is required")
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "admin", "member":
		return organization, nil
	default:
		return "", domainerr.Forbidden("knowledge base access requires an organization member")
	}
}

func (u *UseCases) Create(ctx context.Context, organization, role string, in CreateInput) (KnowledgeBase, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return KnowledgeBase{}, err
	}
	in, err = normalizeBase(in)
	if err != nil {
		return KnowledgeBase{}, err
	}
	return u.repo.Create(ctx, organization, in)
}

func (u *UseCases) Get(ctx context.Context, organization, role string, id uuid.UUID) (KnowledgeBase, error) {
	organization, err := authorizeRead(organization, role)
	if err != nil {
		return KnowledgeBase{}, err
	}
	return u.repo.Get(ctx, organization, id)
}

func (u *UseCases) List(ctx context.Context, organization, role, state string) ([]KnowledgeBase, error) {
	organization, err := authorizeRead(organization, role)
	if err != nil {
		return nil, err
	}
	return u.repo.List(ctx, organization, state)
}

func (u *UseCases) Update(ctx context.Context, organization, role string, id uuid.UUID, in UpdateInput) (KnowledgeBase, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return KnowledgeBase{}, err
	}
	normalized, err := normalizeBase(CreateInput{Name: in.Name, Description: in.Description})
	if err != nil {
		return KnowledgeBase{}, err
	}
	if in.ExpectedVersion <= 0 {
		return KnowledgeBase{}, domainerr.Validation("expected_version is required")
	}
	in.Name, in.Description = normalized.Name, normalized.Description
	return u.repo.Update(ctx, organization, id, in)
}

func (u *UseCases) Lifecycle(ctx context.Context, organization, role string, id uuid.UUID, action string, expectedVersion int64) (KnowledgeBase, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return KnowledgeBase{}, err
	}
	if expectedVersion <= 0 {
		return KnowledgeBase{}, domainerr.Validation("expected_version is required")
	}
	return u.repo.Lifecycle(ctx, organization, id, action, expectedVersion)
}

func (u *UseCases) RegisterDocument(ctx context.Context, organization, role string, baseID uuid.UUID, in RegisterDocumentInput) (Document, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return Document{}, err
	}
	in.ArtifactScope, err = normalizeArtifactScope(in.ArtifactScope)
	if err != nil {
		return Document{}, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if len([]rune(in.Title)) > 300 {
		return Document{}, domainerr.Validation("title must not exceed 300 characters")
	}
	return u.repo.RegisterDocument(ctx, organization, baseID, in)
}

func (u *UseCases) IngestConnector(ctx context.Context, organization, role string, baseID uuid.UUID, in ConnectorIngestionInput) (Document, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return Document{}, err
	}
	in, target, err := normalizeConnectorIngestion(in)
	if err != nil {
		return Document{}, err
	}
	if u.ingestor == nil || u.repo == nil {
		return Document{}, domainerr.Unavailable("knowledge ingestion pipeline is unavailable")
	}
	scope := newIngestionScope(organization, baseID, target)
	artifactScope := artifactScopeFrom(scope, target.DocumentID)
	if err := u.repo.ValidateIngestionScope(ctx, organization, baseID, artifactScope); err != nil {
		return Document{}, err
	}
	result, err := u.ingestor.Ingest(ctx, artifacts.IngestRequest{
		Scope:           scope,
		RequireIndexing: true,
		Artifacts: []artifacts.Manifest{{
			DocumentID: target.DocumentID,
			Name:       in.Source.Name,
			SourceRef:  in.Source.Connector + ":" + in.Source.ExternalID,
			ReadURL:    in.Source.ReadURL,
			SHA256:     in.Source.SHA256,
			MIMEType:   in.Source.MIMEType,
			SizeBytes:  in.Source.SizeBytes,
			Required:   true,
		}},
	})
	if err != nil {
		return Document{}, domainerr.Unavailable("knowledge connector ingestion failed")
	}
	return u.registerIngestedDocument(ctx, organization, baseID, in.Title, scope, target.DocumentID, result)
}

func (u *UseCases) IngestUpload(ctx context.Context, organization, role string, baseID uuid.UUID, in UploadIngestionInput) (Document, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return Document{}, err
	}
	if strings.TrimSpace(in.Target.DocumentID) == "" {
		in.Target.DocumentID = uuid.NewString()
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name != "" {
		in.Name = filepath.Base(in.Name)
	}
	in, target, err := normalizeUploadIngestion(in)
	if err != nil {
		return Document{}, err
	}
	if u.ingestor == nil || u.repo == nil {
		return Document{}, domainerr.Unavailable("knowledge ingestion pipeline is unavailable")
	}
	scope := newIngestionScope(organization, baseID, target)
	artifactScope := artifactScopeFrom(scope, target.DocumentID)
	if err := u.repo.ValidateIngestionScope(ctx, organization, baseID, artifactScope); err != nil {
		return Document{}, err
	}
	result, err := u.ingestor.IngestUpload(ctx, artifacts.UploadRequest{
		Scope:           scope,
		RequireIndexing: true,
		Manifest: artifacts.Manifest{
			DocumentID: target.DocumentID,
			Name:       in.Name,
			SourceRef:  "upload:" + target.DocumentID,
			MIMEType:   in.ContentType,
			Required:   true,
		},
		Content: in.Content, ContentType: in.ContentType,
	})
	if err != nil {
		if errors.Is(err, artifacts.ErrArtifactTooLarge) {
			return Document{}, err
		}
		return Document{}, domainerr.Unavailable("knowledge upload ingestion failed")
	}
	return u.registerIngestedDocument(ctx, organization, baseID, in.Title, scope, target.DocumentID, result)
}

func newIngestionScope(organization string, baseID uuid.UUID, target ingestionTarget) artifacts.Scope {
	return artifacts.Scope{
		OrgID: organization, VirployeeID: target.VirployeeID, ProductSurface: KnowledgeProductSurface,
		SubjectID: target.SubjectID, RepositoryGeneration: "kb-" + baseID.String() + "-" + uuid.NewString(),
	}
}

func artifactScopeFrom(scope artifacts.Scope, documentID string) ArtifactScope {
	return ArtifactScope{
		VirployeeID: scope.VirployeeID, ProductSurface: scope.ProductSurface, SubjectID: scope.SubjectID,
		RepositoryGeneration: scope.RepositoryGeneration, DocumentID: documentID,
	}
}

func (u *UseCases) registerIngestedDocument(ctx context.Context, organization string, baseID uuid.UUID, title string, scope artifacts.Scope, documentID string, result artifacts.IngestResult) (Document, error) {
	if len(result.Records) != 1 {
		return Document{}, domainerr.Unavailable("knowledge ingestion did not produce one verified artifact")
	}
	record := result.Records[0]
	if record.Status != artifacts.StatusIndexed || record.Scope != scope || record.Manifest.DocumentID != documentID || !sha256Pattern.MatchString(record.Manifest.SHA256) || record.Manifest.SizeBytes <= 0 {
		return Document{}, domainerr.Unavailable("knowledge ingestion did not complete verified indexing")
	}
	return u.repo.RegisterDocument(ctx, organization, baseID, RegisterDocumentInput{
		Title: title, ArtifactScope: artifactScopeFrom(scope, documentID),
	})
}

func (u *UseCases) ListDocuments(ctx context.Context, organization, role string, baseID uuid.UUID, state string) ([]Document, error) {
	organization, err := authorizeRead(organization, role)
	if err != nil {
		return nil, err
	}
	return u.repo.ListDocuments(ctx, organization, baseID, state)
}

func (u *UseCases) ArchiveDocument(ctx context.Context, organization, role string, baseID, documentID uuid.UUID, expectedVersion int64) (Document, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return Document{}, err
	}
	if expectedVersion <= 0 {
		return Document{}, domainerr.Validation("expected_version is required")
	}
	return u.repo.ArchiveDocument(ctx, organization, baseID, documentID, expectedVersion)
}

func (u *UseCases) ListBindings(ctx context.Context, organization, role string, baseID uuid.UUID) ([]Binding, error) {
	organization, err := authorizeRead(organization, role)
	if err != nil {
		return nil, err
	}
	return u.repo.ListBindings(ctx, organization, baseID)
}

func (u *UseCases) ReplaceBindings(ctx context.Context, organization, role string, baseID uuid.UUID, in ReplaceBindingsInput) ([]Binding, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return nil, err
	}
	if in.ExpectedVersion <= 0 {
		return nil, domainerr.Validation("expected_version is required")
	}
	if len(in.Bindings) > 100 {
		return nil, domainerr.Validation("a knowledge base may not have more than 100 bindings")
	}
	bindings := make([]Binding, 0, len(in.Bindings))
	for _, raw := range in.Bindings {
		binding, err := normalizeBinding(raw)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return u.repo.ReplaceBindings(ctx, organization, baseID, in.ExpectedVersion, bindings)
}

func (u *UseCases) ListForVirployee(ctx context.Context, organization, role string, virployeeID uuid.UUID) ([]VirployeeKnowledgeBase, error) {
	organization, err := authorizeRead(organization, role)
	if err != nil {
		return nil, err
	}
	if virployeeID == uuid.Nil {
		return nil, domainerr.Validation("virployee_id is required")
	}
	return u.repo.ListForVirployee(ctx, organization, virployeeID)
}

func (u *UseCases) SetForVirployee(ctx context.Context, organization, role string, virployeeID uuid.UUID, in SetVirployeeKnowledgeBaseInput) ([]VirployeeKnowledgeBase, error) {
	organization, err := authorize(organization, role)
	if err != nil {
		return nil, err
	}
	baseID, err := uuid.Parse(strings.TrimSpace(in.KnowledgeBaseID))
	if err != nil || baseID == uuid.Nil {
		return nil, domainerr.Validation("knowledge_base_id must be a valid UUID")
	}
	if virployeeID == uuid.Nil || in.ExpectedVersion <= 0 {
		return nil, domainerr.Validation("virployee_id and expected_version are required")
	}
	if err := u.repo.SetForVirployee(ctx, organization, virployeeID, baseID, in.ExpectedVersion, in.Enabled); err != nil {
		return nil, err
	}
	return u.repo.ListForVirployee(ctx, organization, virployeeID)
}
