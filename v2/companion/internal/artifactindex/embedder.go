package artifactindex

import (
	"context"
	"errors"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
)

const Dimensions = 768

const embeddingBatchSize = 64

type RuntimeEmbedder struct{ client *runtimeclient.Client }

func NewRuntimeEmbedder(client *runtimeclient.Client) *RuntimeEmbedder {
	return &RuntimeEmbedder{client: client}
}

func (e *RuntimeEmbedder) Embed(ctx context.Context, _ artifacts.Scope, chunks []artifacts.Chunk) ([]artifacts.Embedding, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("runtime embedder is not configured")
	}
	out := make([]artifacts.Embedding, 0, len(chunks))
	model := ""
	for start := 0; start < len(chunks); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		texts := make([]string, 0, end-start)
		for _, chunk := range chunks[start:end] {
			texts = append(texts, chunk.Text)
		}
		result, err := e.client.Embed(ctx, runtimeclient.EmbedRequest{Texts: texts, TaskType: runtimeclient.EmbeddingTaskDocument})
		if err != nil {
			return nil, err
		}
		if result.Dimensions != Dimensions || len(result.Embeddings) != len(texts) || (model != "" && model != result.Model) {
			return nil, errors.New("runtime returned invalid embedding shape")
		}
		model = result.Model
		for i, values := range result.Embeddings {
			if len(values) != Dimensions {
				return nil, errors.New("runtime returned invalid embedding dimensions")
			}
			out = append(out, artifacts.Embedding{ChunkID: chunks[start+i].ID, Values: values, Model: result.Model})
		}
	}
	return out, nil
}

func (e *RuntimeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, string, error) {
	if e == nil || e.client == nil {
		return nil, "", errors.New("runtime embedder is not configured")
	}
	result, err := e.client.Embed(ctx, runtimeclient.EmbedRequest{Texts: []string{text}, TaskType: runtimeclient.EmbeddingTaskQuery})
	if err != nil {
		return nil, "", err
	}
	if result.Dimensions != Dimensions || len(result.Embeddings) != 1 || len(result.Embeddings[0]) != Dimensions {
		return nil, "", errors.New("runtime returned invalid query embedding shape")
	}
	return result.Embeddings[0], result.Model, nil
}
