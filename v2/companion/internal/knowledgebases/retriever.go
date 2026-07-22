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
	page, err := r.Search(ctx, scope, query, 0, limit)
	if err != nil {
		return Evidence{}, err
	}
	out := Evidence{Parts: make([]artifacts.ContentPart, 0, len(page.Matches)), Citations: make([]Citation, 0, len(page.Matches))}
	for _, match := range page.Matches {
		out.Parts = append(out.Parts, match.Part)
		out.Citations = append(out.Citations, match.Citation)
	}
	return out, nil
}

// Search preserves retrieval scores and provides stable offset pagination over
// an immutable repository generation. It filters catalog documents before and
// after vector retrieval so sibling sources cannot cross an authorization
// boundary.
func (r *Retriever) Search(ctx context.Context, scope RetrievalScope, query string, offset, limit int) (SearchPage, error) {
	scope.OrgID = strings.TrimSpace(scope.OrgID)
	scope.SubjectID = strings.TrimSpace(scope.SubjectID)
	scope.ProductSurface = strings.TrimSpace(scope.ProductSurface)
	scope.RepositoryGeneration = strings.TrimSpace(scope.RepositoryGeneration)
	query = strings.TrimSpace(query)
	if scope.OrgID == "" || scope.VirployeeID == uuid.Nil || query == "" {
		return SearchPage{}, errors.New("knowledge retrieval organization, virployee, and query are required")
	}
	if offset < 0 {
		return SearchPage{}, errors.New("knowledge retrieval offset cannot be negative")
	}
	if limit <= 0 || limit > 200 {
		limit = 12
	}
	documents, err := r.documents.ResolvedDocuments(ctx, scope)
	if err != nil || len(documents) == 0 {
		return SearchPage{}, err
	}
	grouped := make(map[sourceScope][]Document)
	for _, document := range documents {
		if scope.ProductSurface != "" && document.ArtifactScope.VirployeeID != scope.VirployeeID {
			continue
		}
		if scope.ProductSurface != "" && document.ArtifactScope.ProductSurface != scope.ProductSurface {
			continue
		}
		if scope.ProductSurface != "" && document.ArtifactScope.SubjectID != scope.SubjectID {
			continue
		}
		if scope.RepositoryGeneration != "" && document.ArtifactScope.RepositoryGeneration != scope.RepositoryGeneration {
			continue
		}
		key := sourceScope{
			VirployeeID: document.ArtifactScope.VirployeeID, ProductSurface: document.ArtifactScope.ProductSurface,
			SubjectID: document.ArtifactScope.SubjectID, RepositoryGeneration: document.ArtifactScope.RepositoryGeneration,
		}
		grouped[key] = append(grouped[key], document)
	}
	candidates := make([]candidate, 0)
	retrievalLimit := offset + limit + 1
	if retrievalLimit > 201 {
		retrievalLimit = 201
	}
	truncated := false
	for key, scopedDocuments := range grouped {
		hits, err := r.artifacts.Retrieve(ctx, artifacts.RetrievalQuery{
			Scope: artifacts.Scope{
				OrgID: scope.OrgID, VirployeeID: key.VirployeeID, ProductSurface: key.ProductSurface,
				SubjectID: key.SubjectID, RepositoryGeneration: key.RepositoryGeneration,
			},
			Text: query, Limit: retrievalLimit,
		})
		if err != nil {
			return SearchPage{}, err
		}
		truncated = truncated || len(hits) == retrievalLimit
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
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].hit.Score != candidates[j].hit.Score {
			return candidates[i].hit.Score > candidates[j].hit.Score
		}
		if candidates[i].document.ID != candidates[j].document.ID {
			return candidates[i].document.ID.String() < candidates[j].document.ID.String()
		}
		return candidates[i].hit.Chunk.ID < candidates[j].hit.Chunk.ID
	})
	if offset >= len(candidates) {
		return SearchPage{Matches: []SearchMatch{}, Truncated: truncated}, nil
	}
	end := offset + limit
	hasMore := end < len(candidates)
	if end > len(candidates) {
		end = len(candidates)
	}
	candidates = candidates[offset:end]
	out := SearchPage{Matches: make([]SearchMatch, 0, len(candidates)), HasMore: hasMore, Truncated: truncated}
	for _, item := range candidates {
		locator, _ := json.Marshal(item.hit.Chunk.Locator)
		part := artifacts.ContentPart{
			Kind: artifacts.PartText, Text: item.hit.Chunk.Text, MIMEType: item.hit.Chunk.MIMEType,
			Name: item.document.Title, SHA256: item.document.SourceSHA256,
			DocumentID: item.document.ID.String(), Locator: item.hit.Chunk.Locator,
		}
		baseID := item.document.KnowledgeBaseID
		citation := Citation{
			KnowledgeBaseID: &baseID, DocumentID: item.document.ID.String(),
			SourceVersion: item.document.SourceVersion, SHA256: item.document.SourceSHA256, Locator: locator,
		}
		out.Matches = append(out.Matches, SearchMatch{Part: part, Citation: citation, Score: item.hit.Score})
	}
	return out, nil
}
