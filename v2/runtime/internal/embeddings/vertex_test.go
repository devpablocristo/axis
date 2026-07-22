package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVertexUsesRetrievalTaskAndDisablesTruncation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if !strings.Contains(r.URL.Path, "/gemini-embedding-001:predict") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body struct {
			Instances  []map[string]any `json:"instances"`
			Parameters map[string]any   `json:"parameters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Instances[0]["task_type"] != TaskDocument || body.Parameters["autoTruncate"] != false || body.Parameters["outputDimensionality"] != float64(DefaultDim) {
			t.Fatalf("unexpected request: %#v", body)
		}
		values := make([]float32, DefaultDim)
		_ = json.NewEncoder(w).Encode(map[string]any{"predictions": []any{map[string]any{"embeddings": map[string]any{"values": values, "statistics": map[string]any{"truncated": false}}}}})
	}))
	defer server.Close()
	provider, err := NewVertex(VertexConfig{
		Project: "project", Location: "us-central1", Dimensions: DefaultDim,
		BaseURL: server.URL, TokenSource: func(context.Context) (string, error) { return "test-token", nil },
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := provider.Embed(context.Background(), "clinical document", TaskDocument)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != DefaultDim {
		t.Fatalf("dimensions = %d", len(values))
	}
}

func TestVertexRejectsTruncatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		values := make([]float32, DefaultDim)
		_ = json.NewEncoder(w).Encode(map[string]any{"predictions": []any{map[string]any{"embeddings": map[string]any{"values": values, "statistics": map[string]any{"truncated": true}}}}})
	}))
	defer server.Close()
	provider, err := NewVertex(VertexConfig{
		Project: "project", Location: "us-central1", Dimensions: DefaultDim, BaseURL: server.URL,
		TokenSource: func(context.Context) (string, error) { return "token", nil }, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Embed(context.Background(), "document", TaskDocument); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected truncation error, got %v", err)
	}
}
