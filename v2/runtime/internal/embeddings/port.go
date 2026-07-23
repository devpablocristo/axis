package embeddings

import "context"

const (
	TaskDocument = "RETRIEVAL_DOCUMENT"
	TaskQuery    = "RETRIEVAL_QUERY"
)

// EmbeddingPort is the application-owned boundary used by the HTTP use case.
// Provider-specific request and response DTOs stay in outbound adapters.
type EmbeddingPort interface {
	Embed(context.Context, EmbeddingRequest) ([]float32, error)
	Model() string
	Dimensions() int
}

type EmbeddingRequest struct {
	Text     string
	TaskType string
}
