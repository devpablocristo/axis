package artifacts

import (
	"context"
	"io"

	"github.com/google/uuid"
)

type ArtifactCatalogPort interface {
	UpsertManifest(context.Context, Scope, Manifest) (Record, error)
	SetStatus(context.Context, string, uuid.UUID, Status, string, string, string) (Record, error)
	ListGeneration(context.Context, Scope) ([]Record, error)
}

type ArtifactFetcherPort interface {
	Fetch(context.Context, Manifest, io.Writer) (contentType string, sizeBytes int64, err error)
}

type MalwareScannerPort interface {
	Scan(context.Context, Manifest, Blob) error
}

type ArtifactStorePort interface {
	PutOriginal(context.Context, Scope, Manifest, Blob) (StoredArtifact, error)
	GetOriginal(context.Context, StoredArtifact, io.Writer) (contentType string, sizeBytes int64, err error)
}

type FormatAdapter interface {
	Name() string
	Supports(mimeType, filename string) bool
	Adapt(context.Context, AdaptInput) ([]ContentPart, error)
}

type ExtractionPort interface {
	Extract(context.Context, ExtractRequest) ([]ContentPart, error)
}

type ChunkerPort interface {
	Chunk(context.Context, Scope, []ContentPart) ([]Chunk, error)
}

type EmbeddingPort interface {
	Embed(context.Context, Scope, []Chunk) ([]Embedding, error)
}

type VectorStorePort interface {
	Upsert(context.Context, Scope, []Chunk, []Embedding) error
	DeleteGeneration(context.Context, Scope) error
}

type ArtifactRetrieverPort interface {
	Retrieve(context.Context, RetrievalQuery) ([]RetrievalHit, error)
}

type MultimodalAnswerPort interface {
	Answer(context.Context, AnswerRequest) (Answer, error)
}
