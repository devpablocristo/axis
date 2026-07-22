package artifactindex

import (
	"context"
	"errors"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
)

type QueryEmbedder interface {
	artifacts.EmbeddingPort
	EmbedQuery(context.Context, artifacts.Scope, string) ([]float32, string, error)
}

type Store interface {
	artifacts.VectorStorePort
	Search(context.Context, artifacts.RetrievalQuery, []float32, string) ([]artifacts.RetrievalHit, error)
}

type Service struct {
	chunker  artifacts.ChunkerPort
	embedder QueryEmbedder
	store    Store
}

func NewService(chunker artifacts.ChunkerPort, embedder QueryEmbedder, store Store) (*Service, error) {
	if chunker == nil || embedder == nil || store == nil {
		return nil, errors.New("artifact index dependencies are required")
	}
	return &Service{chunker: chunker, embedder: embedder, store: store}, nil
}

func (s *Service) Index(ctx context.Context, scope artifacts.Scope, parts []artifacts.ContentPart) error {
	chunks, err := s.chunker.Chunk(ctx, scope, parts)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return s.store.DeleteGeneration(ctx, scope)
	}
	embeddings, err := s.embedder.Embed(ctx, scope, chunks)
	if err != nil {
		return err
	}
	return s.store.Upsert(ctx, scope, chunks, embeddings)
}

func (s *Service) Retrieve(ctx context.Context, query artifacts.RetrievalQuery) ([]artifacts.RetrievalHit, error) {
	if strings.TrimSpace(query.Text) == "" {
		return nil, errors.New("retrieval query text is required")
	}
	vector, model, err := s.embedder.EmbedQuery(ctx, query.Scope, query.Text)
	if err != nil {
		return nil, err
	}
	return s.store.Search(ctx, query, vector, model)
}
