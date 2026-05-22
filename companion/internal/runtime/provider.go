// Package runtime implementa el control plane del empleado IA Companion.
// Orquesta LLM + tools + context para dar una sola voz al suscriptor.
package runtime

import (
	"context"
	"fmt"
	"strings"

	coreai "github.com/devpablocristo/platform/kernels/ai/go"
	"golang.org/x/oauth2/google"
)

const (
	DefaultGeminiProvider = "vertex"
	DefaultGeminiModel    = "gemini-2.5-flash-lite"
	DefaultVertexLocation = "us-central1"
)

// Re-exportar tipos de platform/kernels/ai/go para que el resto del runtime no importe el kernel directamente.
type (
	LLMProvider  = coreai.Provider
	ChatRequest  = coreai.ChatRequest
	ChatResponse = coreai.ChatResponse
	LLMMessage   = coreai.Message
	LLMToolCall  = coreai.ToolCall
	ToolSchema   = coreai.Tool
)

// ProviderConfig agrupa la configuración para construir Gemini sobre Vertex AI.
type ProviderConfig struct {
	Provider       string
	Model          string
	VertexProject  string
	VertexLocation string
}

// NewProvider crea el provider Gemini. Companion solo soporta Gemini via
// Vertex AI; cualquier otro provider falla en boot para evitar drift local/GCP.
func NewProvider(cfg ProviderConfig) (LLMProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = DefaultGeminiProvider
	}
	if provider != "vertex" && provider != "vertex_ai" {
		return nil, fmt.Errorf("unsupported COMPANION_LLM_PROVIDER=%q: companion only supports Gemini via Vertex AI", cfg.Provider)
	}
	return newVertexProvider(cfg)
}

func newVertexProvider(cfg ProviderConfig) (LLMProvider, error) {
	project := strings.TrimSpace(cfg.VertexProject)
	region := strings.TrimSpace(cfg.VertexLocation)
	if region == "" {
		region = DefaultVertexLocation
	}
	if project == "" {
		return nil, fmt.Errorf("COMPANION_LLM_VERTEX_PROJECT is required for Gemini via Vertex AI")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultGeminiModel
	}
	if !strings.Contains(strings.ToLower(model), "gemini") {
		return nil, fmt.Errorf("COMPANION_LLM_MODEL=%q is not a Gemini model", cfg.Model)
	}

	ts, err := google.DefaultTokenSource(context.Background(),
		"https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("load Google ADC for Gemini Vertex AI: %w", err)
	}

	tokenSource := func(ctx context.Context) (string, error) {
		tok, err := ts.Token()
		if err != nil {
			return "", fmt.Errorf("vertex token: %w", err)
		}
		return tok.AccessToken, nil
	}

	return coreai.NewVertexAI(project, region, tokenSource, coreai.WithVertexModel(model)), nil
}
