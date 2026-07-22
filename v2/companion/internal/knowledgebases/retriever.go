package knowledgebases

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/google/uuid"
)

type DocumentResolver interface {
	ResolvedDocuments(context.Context, RetrievalScope) ([]Document, error)
}

type Retriever struct {
	documents DocumentResolver
	artifacts artifacts.ArtifactRetrieverPort
}

func NewRetriever(documents DocumentResolver, retriever artifacts.ArtifactRetrieverPort) (*Retriever, error) {
	if documents == nil || retriever == nil {
		return nil, errors.New("knowledge document resolver and artifact retriever are required")
	}
	return &Retriever{documents: documents, artifacts: retriever}, nil
}

type sourceScope struct {
	VirployeeID          uuid.UUID
	ProductSurface       string
	SubjectID            string
	RepositoryGeneration string
}

type candidate struct {
	document Document
	hit      artifacts.RetrievalHit
}

func (r *Retriever) Retrieve(ctx context.Context, scope RetrievalScope, query string, limit int) (Evidence, error) {
	scope.TenantID = strings.TrimSpace(scope.TenantID)
	scope.SubjectID = strings.TrimSpace(scope.SubjectID)
	query = strings.TrimSpace(query)
	if scope.TenantID == "" || scope.VirployeeID == uuid.Nil || query == "" {
		return Evidence{}, errors.New("knowledge retrieval tenant, virployee, and query are required")
	}
	if limit <= 0 || limit > 20 {
		limit = 12
	}
	documents, err := r.documents.ResolvedDocuments(ctx, scope)
	if err != nil || len(documents) == 0 {
		return Evidence{}, err
	}
	grouped := make(map[sourceScope][]Document)
	for _, document := range documents {
		key := sourceScope{
			VirployeeID: document.ArtifactScope.VirployeeID, ProductSurface: document.ArtifactScope.ProductSurface,
			SubjectID: document.ArtifactScope.SubjectID, RepositoryGeneration: document.ArtifactScope.RepositoryGeneration,
		}
		grouped[key] = append(grouped[key], document)
	}
	candidates := make([]candidate, 0)
	for key, scopedDocuments := range grouped {
		hits, err := r.artifacts.Retrieve(ctx, artifacts.RetrievalQuery{
			Scope: artifacts.Scope{
				TenantID: scope.TenantID, VirployeeID: key.VirployeeID, ProductSurface: key.ProductSurface,
				SubjectID: key.SubjectID, RepositoryGeneration: key.RepositoryGeneration,
			},
			Text: query, Limit: 50,
		})
		if err != nil {
			return Evidence{}, err
		}
		for _, hit := range hits {
			for _, document := range scopedDocuments {
				// The artifact retriever ranks a whole generation.  Filter again
				// after retrieval so an unregistered sibling document can never
				// enter grounded context.
				if hit.Chunk.DocumentID != document.ArtifactScope.DocumentID ||
					hit.Chunk.SHA256 != document.SourceSHA256 ||
					hit.Chunk.SourceVersion != document.SourceVersion {
					continue
				}
				candidates = append(candidates, candidate{document: document, hit: hit})
				break
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].hit.Score > candidates[j].hit.Score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := Evidence{Parts: make([]artifacts.ContentPart, 0, len(candidates)), Citations: make([]Citation, 0, len(candidates))}
	for _, item := range candidates {
		locator, _ := json.Marshal(item.hit.Chunk.Locator)
		out.Parts = append(out.Parts, artifacts.ContentPart{
			Kind: artifacts.PartText, Text: item.hit.Chunk.Text, MIMEType: item.hit.Chunk.MIMEType,
			Name: item.document.Title, SHA256: item.document.SourceSHA256,
			DocumentID: item.document.ID.String(), Locator: item.hit.Chunk.Locator,
		})
		baseID := item.document.KnowledgeBaseID
		out.Citations = append(out.Citations, Citation{
			KnowledgeBaseID: &baseID, DocumentID: item.document.ID.String(),
			SourceVersion: item.document.SourceVersion, SHA256: item.document.SourceSHA256, Locator: locator,
		})
	}
	return out, nil
}
