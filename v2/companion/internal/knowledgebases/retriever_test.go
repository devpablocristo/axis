package knowledgebases

import (
	"context"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/google/uuid"
)

type documentResolverStub struct{ documents []Document }

func (s documentResolverStub) ResolvedDocuments(context.Context, RetrievalScope) ([]Document, error) {
	return s.documents, nil
}

type artifactRetrieverStub struct{ hits []artifacts.RetrievalHit }

func (s artifactRetrieverStub) Retrieve(context.Context, artifacts.RetrievalQuery) ([]artifacts.RetrievalHit, error) {
	return s.hits, nil
}

func TestRetrieverDropsUnregisteredSiblingDocuments(t *testing.T) {
	virployeeID, baseID, documentID := uuid.New(), uuid.New(), uuid.New()
	document := Document{
		ID: documentID, KnowledgeBaseID: baseID, Title: "Clinical guide", SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SourceVersion: "v1",
		ArtifactScope: ArtifactScope{VirployeeID: virployeeID, ProductSurface: "producta", SubjectID: "patient-a", RepositoryGeneration: "g1", DocumentID: "source-allowed"},
	}
	r, err := NewRetriever(documentResolverStub{documents: []Document{document}}, artifactRetrieverStub{hits: []artifacts.RetrievalHit{
		{Chunk: artifacts.Chunk{ID: "bad", Text: "private sibling", DocumentID: "source-other", SHA256: document.SourceSHA256, SourceVersion: "v1"}, Score: 0.99},
		{Chunk: artifacts.Chunk{ID: "ok", Text: "approved evidence", DocumentID: "source-allowed", SHA256: document.SourceSHA256, SourceVersion: "v1"}, Score: 0.80},
	}})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := r.Retrieve(context.Background(), RetrievalScope{OrgID: "organization-a", VirployeeID: virployeeID, SubjectID: "patient-a"}, "question", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(evidence.Parts) != 1 || evidence.Parts[0].Text != "approved evidence" || evidence.Parts[0].DocumentID != documentID.String() {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
	if len(evidence.Citations) != 1 || evidence.Citations[0].KnowledgeBaseID == nil || *evidence.Citations[0].KnowledgeBaseID != baseID {
		t.Fatalf("unexpected citations: %#v", evidence.Citations)
	}
}
