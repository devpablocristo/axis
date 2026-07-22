package artifacts

import (
	"context"
	"fmt"
	"strings"
)

// CorpusReader reconstructs a reusable runtime corpus from already-staged
// originals and the tenant-scoped index. It never uses the product read URL and
// never repeats staging, malware scanning or indexing.
type CorpusReader struct {
	catalog   ArtifactCatalogPort
	retriever ArtifactRetrieverPort
}

func NewCorpusReader(catalog ArtifactCatalogPort, retriever ArtifactRetrieverPort) *CorpusReader {
	return &CorpusReader{catalog: catalog, retriever: retriever}
}

func (r *CorpusReader) Load(ctx context.Context, scope Scope, query string, limit int) ([]ContentPart, error) {
	if r == nil || r.catalog == nil {
		return nil, nil
	}
	records, err := r.catalog.ListGeneration(ctx, scope)
	if err != nil {
		return nil, err
	}
	parts := make([]ContentPart, 0, len(records)+1)
	for _, record := range records {
		if record.StagedURI == "" || record.Status == StatusFailed {
			if record.Manifest.Required {
				return nil, fmt.Errorf("required staged artifact %s is unavailable", record.Manifest.DocumentID)
			}
			continue
		}
		mimeType := record.ActualMIME
		if mimeType == "" {
			mimeType = record.Manifest.MIMEType
		}
		parts = append(parts, ContentPart{
			Kind: PartFileData, URI: record.StagedURI, MIMEType: mimeType,
			Name: record.Manifest.Name, SHA256: record.Manifest.SHA256,
			DocumentID: record.Manifest.DocumentID,
		})
	}
	if r.retriever != nil && strings.TrimSpace(query) != "" {
		if limit <= 0 {
			limit = 12
		}
		hits, err := r.retriever.Retrieve(ctx, RetrievalQuery{Scope: scope, Text: query, Limit: limit})
		if err != nil {
			return nil, err
		}
		var contextText strings.Builder
		for _, hit := range hits {
			if strings.TrimSpace(hit.Chunk.Text) == "" {
				continue
			}
			contextText.WriteString("[document=")
			contextText.WriteString(hit.Chunk.DocumentID)
			contextText.WriteString("]\n")
			contextText.WriteString(hit.Chunk.Text)
			contextText.WriteString("\n\n")
		}
		if contextText.Len() > 0 {
			parts = append(parts, ContentPart{Kind: PartText, Text: contextText.String(), Name: "retrieved_context"})
		}
	}
	if len(parts) == 0 {
		return nil, ErrEmptyDerivative
	}
	return parts, nil
}
