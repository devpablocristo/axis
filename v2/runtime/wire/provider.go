package wire

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	ai "github.com/devpablocristo/platform/kernels/ai/go"
	cfg "github.com/devpablocristo/runtime-v2/cmd/config"
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
func buildProvider(ctx context.Context, config cfg.Config) ai.Provider {
	switch config.LLMProvider {
	case "vertex", "vertexai", "vertex_ai":
		if config.VertexProject == "" {
			slog.WarnContext(ctx, "runtime_vertex_no_project_fallback_echo")
			return ai.NewEcho()
		}
		source, err := google.DefaultTokenSource(ctx, vertexScope)
		if err != nil {
			slog.WarnContext(ctx, "runtime_vertex_no_adc_fallback_echo", "error", err.Error())
			return ai.NewEcho()
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
		return ai.NewVertexAI(config.VertexProject, config.VertexLocation, tokenSource, ai.WithVertexModel(model))
	default:
		return ai.NewProvider(config.LLMProvider, config.LLMAPIKey, config.LLMModel)
	}
}

func buildEmbeddingProvider(ctx context.Context, config cfg.Config) embeddings.Provider {
	if config.VertexProject == "" {
		slog.WarnContext(ctx, "runtime_embedding_no_project")
		return nil
	}
	source, err := google.DefaultTokenSource(ctx, vertexScope)
	if err != nil {
		slog.WarnContext(ctx, "runtime_embedding_no_adc", "error", err.Error())
		return nil
	}
	provider, err := embeddings.NewVertex(embeddings.VertexConfig{
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
		return nil
	}
	return provider
}
