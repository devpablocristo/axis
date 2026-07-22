package artifactindex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
	"github.com/google/uuid"
)

func TestChunkerIsDeterministicAndPreservesProvenance(t *testing.T) {
	chunker := NewChunker()
	chunker.MaxRunes = 20
	chunker.OverlapRunes = 4
	part := artifacts.ContentPart{Kind: artifacts.PartText, Text: strings.Repeat("clinical value ", 5), DocumentID: "doc-1", SHA256: "sha", MIMEType: "text/plain", Locator: &artifacts.Locator{Page: 2}}
	first, err := chunker.Chunk(context.Background(), artifacts.Scope{}, []artifacts.ContentPart{part})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := chunker.Chunk(context.Background(), artifacts.Scope{}, []artifacts.ContentPart{part})
	if len(first) < 2 || first[0].ID != second[0].ID || first[0].Locator.Page != 2 || first[0].ChunkerVersion == "" {
		t.Fatalf("unexpected chunks: %#v", first)
	}
}

func TestRuntimeEmbedderBatchesLargeCorpus(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Texts []string `json:"texts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		embeddings := make([][]float32, len(request.Texts))
		for i := range embeddings {
			embeddings[i] = make([]float32, Dimensions)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "gemini-embedding-001", "dimensions": Dimensions, "embeddings": embeddings})
	}))
	defer server.Close()
	chunks := make([]artifacts.Chunk, embeddingBatchSize+1)
	for i := range chunks {
		chunks[i] = artifacts.Chunk{ID: uuid.NewString(), Text: "clinical"}
	}
	embedder := NewRuntimeEmbedder(runtimeclient.New(server.URL, server.Client(), "token"))
	result, err := embedder.Embed(context.Background(), artifacts.Scope{}, chunks)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(result) != len(chunks) {
		t.Fatalf("calls=%d embeddings=%d", calls, len(result))
	}
}

type fakeEmbedder struct{ queryTask bool }

func (e *fakeEmbedder) Embed(_ context.Context, _ artifacts.Scope, chunks []artifacts.Chunk) ([]artifacts.Embedding, error) {
	out := make([]artifacts.Embedding, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, artifacts.Embedding{ChunkID: chunk.ID, Values: make([]float32, Dimensions), Model: "gemini-embedding-001"})
	}
	return out, nil
}
func (e *fakeEmbedder) EmbedQuery(context.Context, artifacts.Scope, string) ([]float32, string, error) {
	e.queryTask = true
	return make([]float32, Dimensions), "gemini-embedding-001", nil
}

type fakeStore struct {
	scope    artifacts.Scope
	chunks   []artifacts.Chunk
	searched bool
}

func (s *fakeStore) Upsert(_ context.Context, scope artifacts.Scope, chunks []artifacts.Chunk, _ []artifacts.Embedding) error {
	s.scope, s.chunks = scope, chunks
	return nil
}
func (s *fakeStore) DeleteGeneration(context.Context, artifacts.Scope) error { return nil }
func (s *fakeStore) Search(_ context.Context, query artifacts.RetrievalQuery, _ []float32, _ string) ([]artifacts.RetrievalHit, error) {
	s.searched, s.scope = true, query.Scope
	return []artifacts.RetrievalHit{{Chunk: artifacts.Chunk{Text: "hit"}, Score: .9}}, nil
}

func TestServiceIndexesAndRetrievesWithinScope(t *testing.T) {
	scope := artifacts.Scope{TenantID: "tenant-a", VirployeeID: uuid.New(), ProductSurface: "medmory", SubjectID: "subject", RepositoryGeneration: "generation"}
	embedder, store := &fakeEmbedder{}, &fakeStore{}
	service, err := NewService(NewChunker(), embedder, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Index(context.Background(), scope, []artifacts.ContentPart{{Kind: artifacts.PartText, Text: "glucose 126 mg/dL", DocumentID: "doc", SHA256: "sha"}}); err != nil {
		t.Fatal(err)
	}
	if store.scope.TenantID != scope.TenantID || len(store.chunks) != 1 {
		t.Fatalf("index scope/chunks = %#v %d", store.scope, len(store.chunks))
	}
	hits, err := service.Retrieve(context.Background(), artifacts.RetrievalQuery{Scope: scope, Text: "glucose", Limit: 5})
	if err != nil || len(hits) != 1 || !embedder.queryTask || !store.searched {
		t.Fatalf("retrieve hits=%v err=%v", hits, err)
	}
}

func TestVectorLiteralHasExpectedShape(t *testing.T) {
	values := make([]float32, Dimensions)
	values[0], values[Dimensions-1] = 1.5, -2
	literal := vectorLiteral(values)
	if !strings.HasPrefix(literal, "[1.5,0") || !strings.HasSuffix(literal, ",-2]") || strings.Count(literal, ",") != Dimensions-1 {
		t.Fatalf("invalid vector literal: %s", literal)
	}
}
