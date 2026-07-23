package wire

import (
	"context"
	"testing"

	cfg "github.com/devpablocristo/runtime-v2/cmd/config"
	"github.com/devpablocristo/runtime-v2/internal/adapters/out/embeddingdeterministic"
	"github.com/devpablocristo/runtime-v2/internal/adapters/out/embeddingvertex"
)

func TestEmbeddingProviderUsesDeterministicFallbackOnlyOutsideProduction(t *testing.T) {
	development := buildEmbeddingProvider(context.Background(), cfg.Config{
		Environment: "development", EmbeddingDim: embeddingvertex.DefaultDim, DevelopmentEmbeddingsEnabled: true,
	})
	if development == nil || development.Model() != embeddingdeterministic.DeterministicDevelopmentModel {
		t.Fatalf("expected deterministic development fallback, got %#v", development)
	}
	production := buildEmbeddingProvider(context.Background(), cfg.Config{
		Environment: "production", EmbeddingDim: embeddingvertex.DefaultDim, DevelopmentEmbeddingsEnabled: true,
	})
	if production != nil {
		t.Fatalf("production must fail closed without Vertex, got %q", production.Model())
	}
	disabled := buildEmbeddingProvider(context.Background(), cfg.Config{
		Environment: "test", EmbeddingDim: embeddingvertex.DefaultDim, DevelopmentEmbeddingsEnabled: false,
	})
	if disabled != nil {
		t.Fatalf("explicitly disabled fallback must fail closed, got %q", disabled.Model())
	}
}

func TestLLMProviderReportsEchoWhenVertexIsNotConfigured(t *testing.T) {
	provider, model := buildProvider(context.Background(), cfg.Config{LLMProvider: "vertex", LLMModel: "gemini-test"})
	if provider == nil || model != "echo" {
		t.Fatalf("expected explicit echo model, got provider=%#v model=%q", provider, model)
	}
}
