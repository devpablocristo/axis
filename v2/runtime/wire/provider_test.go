package wire

import (
	"context"
	"testing"

	cfg "github.com/devpablocristo/runtime-v2/cmd/config"
	"github.com/devpablocristo/runtime-v2/internal/embeddings"
)

func TestEmbeddingProviderUsesDeterministicFallbackOnlyOutsideProduction(t *testing.T) {
	development := buildEmbeddingProvider(context.Background(), cfg.Config{
		Environment: "development", EmbeddingDim: embeddings.DefaultDim, DevelopmentEmbeddingsEnabled: true,
	})
	if development == nil || development.Model() != embeddings.DeterministicDevelopmentModel {
		t.Fatalf("expected deterministic development fallback, got %#v", development)
	}
	production := buildEmbeddingProvider(context.Background(), cfg.Config{
		Environment: "production", EmbeddingDim: embeddings.DefaultDim, DevelopmentEmbeddingsEnabled: true,
	})
	if production != nil {
		t.Fatalf("production must fail closed without Vertex, got %q", production.Model())
	}
	disabled := buildEmbeddingProvider(context.Background(), cfg.Config{
		Environment: "test", EmbeddingDim: embeddings.DefaultDim, DevelopmentEmbeddingsEnabled: false,
	})
	if disabled != nil {
		t.Fatalf("explicitly disabled fallback must fail closed, got %q", disabled.Model())
	}
}
