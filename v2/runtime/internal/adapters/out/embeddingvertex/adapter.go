package embeddingvertex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/devpablocristo/runtime-v2/internal/embeddings"
)

const (
	DefaultModel = "gemini-embedding-001"
	DefaultDim   = 768
)

type TokenSource func(context.Context) (string, error)

type Adapter struct {
	project    string
	location   string
	model      string
	dimensions int
	baseURL    string
	tokens     TokenSource
	http       *http.Client
}

type Config struct {
	Project, Location, Model, BaseURL string
	Dimensions                        int
	TokenSource                       TokenSource
	HTTPClient                        *http.Client
}

func New(config Config) (*Adapter, error) {
	config.Project = strings.TrimSpace(config.Project)
	config.Location = strings.TrimSpace(config.Location)
	config.Model = strings.TrimSpace(config.Model)
	if config.Project == "" || config.Location == "" || config.TokenSource == nil {
		return nil, errors.New("vertex embedding project, location and token source are required")
	}
	if config.Model == "" {
		config.Model = DefaultModel
	}
	if config.Dimensions == 0 {
		config.Dimensions = DefaultDim
	}
	if config.Dimensions != DefaultDim {
		return nil, fmt.Errorf("embedding dimensions must be %d", DefaultDim)
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://" + config.Location + "-aiplatform.googleapis.com"
	}
	return &Adapter{
		project: config.Project, location: config.Location, model: config.Model,
		dimensions: config.Dimensions, baseURL: baseURL, tokens: config.TokenSource,
		http: config.HTTPClient,
	}, nil
}

func (v *Adapter) Model() string   { return v.model }
func (v *Adapter) Dimensions() int { return v.dimensions }

func (v *Adapter) Embed(ctx context.Context, request embeddings.EmbeddingRequest) ([]float32, error) {
	text := strings.TrimSpace(request.Text)
	if text == "" {
		return nil, errors.New("embedding text is required")
	}
	if request.TaskType != embeddings.TaskDocument && request.TaskType != embeddings.TaskQuery {
		return nil, errors.New("unsupported embedding task type")
	}
	body, err := json.Marshal(map[string]any{
		"instances":  []map[string]any{{"content": text, "task_type": request.TaskType}},
		"parameters": map[string]any{"autoTruncate": false, "outputDimensionality": v.dimensions},
	})
	if err != nil {
		return nil, err
	}
	token, err := v.tokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("vertex embedding token: %w", err)
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		v.baseURL, url.PathEscape(v.project), url.PathEscape(v.location), url.PathEscape(v.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vertex embedding request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("vertex embedding status %d", resp.StatusCode)
	}
	var decoded struct {
		Predictions []struct {
			Embeddings struct {
				Values     []float32 `json:"values"`
				Statistics struct {
					Truncated bool `json:"truncated"`
				} `json:"statistics"`
			} `json:"embeddings"`
		} `json:"predictions"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode vertex embedding: %w", err)
	}
	if len(decoded.Predictions) != 1 || len(decoded.Predictions[0].Embeddings.Values) != v.dimensions {
		return nil, fmt.Errorf("vertex embedding returned invalid dimensions")
	}
	if decoded.Predictions[0].Embeddings.Statistics.Truncated {
		return nil, errors.New("vertex embedding input was truncated")
	}
	return decoded.Predictions[0].Embeddings.Values, nil
}
