package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	EmbeddingProviderHash   = "hash"
	EmbeddingProviderVertex = "vertex"

	defaultVertexEmbeddingModel = "text-embedding-005"
	defaultVertexLocation       = "us-central1"
)

type EmbeddingProviderConfig struct {
	Provider       string
	Model          string
	VertexProject  string
	VertexLocation string
	Dimensions     int
	HTTPClient     *http.Client
}

func NewEmbeddingProvider(cfg EmbeddingProviderConfig) (EmbeddingProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		if strings.TrimSpace(cfg.VertexProject) != "" {
			provider = EmbeddingProviderVertex
		} else {
			provider = EmbeddingProviderHash
		}
	}
	switch provider {
	case EmbeddingProviderHash, "fake", "noop", "local":
		return NewHashEmbeddingProvider(), nil
	case EmbeddingProviderVertex, "vertex_ai":
		return NewVertexEmbeddingProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.Provider)
	}
}

type VertexEmbeddingProvider struct {
	project     string
	location    string
	model       string
	dimensions  int
	httpClient  *http.Client
	tokenSource func(context.Context) (string, error)
}

func NewVertexEmbeddingProvider(cfg EmbeddingProviderConfig) (*VertexEmbeddingProvider, error) {
	project := strings.TrimSpace(cfg.VertexProject)
	if project == "" {
		return nil, fmt.Errorf("COMPANION_EMBEDDING_VERTEX_PROJECT is required for vertex embeddings")
	}
	location := strings.TrimSpace(cfg.VertexLocation)
	if location == "" {
		location = defaultVertexLocation
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultVertexEmbeddingModel
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	ts, err := google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("load Google ADC for Vertex embeddings: %w", err)
	}
	tokenSource := func(ctx context.Context) (string, error) {
		tok, err := ts.Token()
		if err != nil {
			return "", fmt.Errorf("vertex embedding token: %w", err)
		}
		return tok.AccessToken, nil
	}
	return &VertexEmbeddingProvider{
		project:     project,
		location:    location,
		model:       model,
		dimensions:  cfg.Dimensions,
		httpClient:  httpClient,
		tokenSource: tokenSource,
	}, nil
}

func (p *VertexEmbeddingProvider) Embed(ctx context.Context, in EmbeddingInput) (Embedding, error) {
	if p == nil {
		return Embedding{}, fmt.Errorf("vertex embedding provider is not configured")
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		text = " "
	}
	token, err := p.tokenSource(ctx)
	if err != nil {
		return Embedding{}, err
	}
	parameters := map[string]any{}
	if p.dimensions > 0 {
		parameters["outputDimensionality"] = p.dimensions
	}
	body := map[string]any{
		"instances": []map[string]any{{
			"content":   text,
			"task_type": "RETRIEVAL_DOCUMENT",
		}},
	}
	if len(parameters) > 0 {
		body["parameters"] = parameters
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Embedding{}, fmt.Errorf("marshal vertex embedding request: %w", err)
	}
	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		p.location, p.project, p.location, p.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Embedding{}, fmt.Errorf("build vertex embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Embedding{}, fmt.Errorf("vertex embedding request: %w", err)
	}
	defer resp.Body.Close()
	var decoded struct {
		Predictions []struct {
			Embeddings struct {
				Values []float64 `json:"values"`
			} `json:"embeddings"`
		} `json:"predictions"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Embedding{}, fmt.Errorf("decode vertex embedding response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(decoded.Error.Message)
		if msg == "" {
			msg = resp.Status
		}
		return Embedding{}, fmt.Errorf("vertex embedding failed: %s", msg)
	}
	if len(decoded.Predictions) == 0 || len(decoded.Predictions[0].Embeddings.Values) == 0 {
		return Embedding{}, fmt.Errorf("vertex embedding response did not include values")
	}
	return Embedding{
		Model:       p.model,
		Vector:      normalizeVector(decoded.Predictions[0].Embeddings.Values),
		Namespace:   memoryNamespace(in.OrgID, in.ProductSurface, in.AgentID),
		ContentHash: contentHash(in.Text),
	}, nil
}
