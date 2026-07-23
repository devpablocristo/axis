package wire

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	ai "github.com/devpablocristo/platform/kernels/ai/go"
	cfg "github.com/devpablocristo/runtime-v2/cmd/config"
	"github.com/devpablocristo/runtime-v2/internal/adapters/out/embeddingdeterministic"
	"github.com/devpablocristo/runtime-v2/internal/adapters/out/embeddingvertex"
	"github.com/devpablocristo/runtime-v2/internal/embeddings"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2/google"
)

const vertexScope = "https://www.googleapis.com/auth/cloud-platform"

// buildProvider selects the LLM provider. For "vertex" it uses Vertex AI
// (Gemini) authenticated with Application Default Credentials — no API key. If
// the project is unset or ADC is unavailable it falls back to Echo, so dev and
// CI run without GCP access. Other providers go through kernels/ai's factory
// (which itself falls back to Echo without an API key).
func buildProvider(ctx context.Context, config cfg.Config) (ai.Provider, string) {
	switch config.LLMProvider {
	case "vertex", "vertexai", "vertex_ai":
		if config.VertexProject == "" {
			slog.WarnContext(ctx, "runtime_vertex_no_project_fallback_echo")
			return ai.NewEcho(), "echo"
		}
		source, err := google.DefaultTokenSource(ctx, vertexScope)
		if err != nil {
			slog.WarnContext(ctx, "runtime_vertex_no_adc_fallback_echo", "error", err.Error())
			return ai.NewEcho(), "echo"
		}
		tokenSource := func(context.Context) (string, error) {
			token, err := source.Token()
			if err != nil {
				return "", err
			}
			return token.AccessToken, nil
		}
		model := config.LLMModel
		if model == "" {
			model = "gemini-2.5-flash-lite"
		}
		return ai.NewVertexAI(config.VertexProject, config.VertexLocation, tokenSource, ai.WithVertexModel(model)), model
	default:
		provider := ai.NewProvider(config.LLMProvider, config.LLMAPIKey, config.LLMModel)
		if config.LLMAPIKey == "" {
			return provider, "echo"
		}
		return provider, config.LLMModel
	}
}

func buildEmbeddingProvider(ctx context.Context, config cfg.Config) embeddings.EmbeddingPort {
	if config.VertexProject == "" {
		slog.WarnContext(ctx, "runtime_embedding_no_project")
		return developmentEmbeddingFallback(ctx, config)
	}
	source, err := google.DefaultTokenSource(ctx, vertexScope)
	if err != nil {
		slog.WarnContext(ctx, "runtime_embedding_no_adc", "error", err.Error())
		return developmentEmbeddingFallback(ctx, config)
	}
	provider, err := embeddingvertex.New(embeddingvertex.Config{
		Project: config.VertexProject, Location: config.VertexLocation, Model: config.EmbeddingModel,
		Dimensions: config.EmbeddingDim,
		TokenSource: func(context.Context) (string, error) {
			token, tokenErr := source.Token()
			if tokenErr != nil {
				return "", tokenErr
			}
			return token.AccessToken, nil
		},
		HTTPClient: &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
	})
	if err != nil {
		slog.WarnContext(ctx, "runtime_embedding_invalid_config", "error", err.Error())
		return developmentEmbeddingFallback(ctx, config)
	}
	return provider
}

func developmentEmbeddingFallback(ctx context.Context, config cfg.Config) embeddings.EmbeddingPort {
	if config.Environment == "production" || !config.DevelopmentEmbeddingsEnabled {
		return nil
	}
	provider, err := embeddingdeterministic.New(config.EmbeddingDim)
	if err != nil {
		slog.WarnContext(ctx, "runtime_development_embedding_invalid_config", "error", err.Error())
		return nil
	}
	slog.WarnContext(ctx, "runtime_development_embedding_enabled", "model", provider.Model())
	return provider
}
