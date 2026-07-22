package artifactindex

import (
	"context"
	"os"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
	"github.com/devpablocristo/companion-v2/internal/runtimeclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryTenantScopedHybridSearch(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_ARTIFACT_INDEX_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_ARTIFACT_INDEX_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool)
	virployeeID := uuid.New()
	scopeA := artifacts.Scope{TenantID: "index-tenant-a", VirployeeID: virployeeID, ProductSurface: "medmory", SubjectID: "patient", RepositoryGeneration: "g1"}
	scopeB := scopeA
	scopeB.TenantID = "index-tenant-b"
	vector := make([]float32, Dimensions)
	vector[0] = 1
	chunk := artifacts.Chunk{ID: "chunk-a", Text: "glucose fasting 126 mg dL", DocumentID: "doc-a", SHA256: "sha-a", SourceVersion: "sha-a", ExtractorVersion: "extract-v1", ChunkerVersion: "chunk-v1"}
	embedding := artifacts.Embedding{ChunkID: chunk.ID, Values: vector, Model: "gemini-embedding-001"}
	if err := repository.Upsert(ctx, scopeA, []artifacts.Chunk{chunk}, []artifacts.Embedding{embedding}); err != nil {
		t.Fatal(err)
	}
	other := chunk
	other.ID, other.DocumentID, other.Text = "chunk-b", "doc-b", "secret other tenant"
	embedding.ChunkID = other.ID
	if err := repository.Upsert(ctx, scopeB, []artifacts.Chunk{other}, []artifacts.Embedding{embedding}); err != nil {
		t.Fatal(err)
	}
	hits, err := repository.Search(ctx, artifacts.RetrievalQuery{Scope: scopeA, Text: "glucose", Limit: 10}, vector, "gemini-embedding-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chunk.DocumentID != "doc-a" {
		t.Fatalf("cross-tenant or missing result: %#v", hits)
	}
	_, _ = pool.Exec(ctx, "DELETE FROM companion_artifact_chunks WHERE tenant_id IN ('index-tenant-a','index-tenant-b')")
}

func TestServiceRuntimePostgresEndToEnd(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_ARTIFACT_INDEX_TEST_DATABASE_URL")
	runtimeURL := os.Getenv("COMPANION_V2_ARTIFACT_INDEX_TEST_RUNTIME_URL")
	if databaseURL == "" || runtimeURL == "" {
		t.Skip("artifact index database/runtime integration endpoints are not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	client := runtimeclient.New(runtimeURL, nil, os.Getenv("AXIS_V2_INTERNAL_AUTH_SECRET"))
	service, err := NewService(NewChunker(), NewRuntimeEmbedder(client), NewRepository(pool))
	if err != nil {
		t.Fatal(err)
	}
	scope := artifacts.Scope{TenantID: "index-e2e", VirployeeID: uuid.New(), ProductSurface: "medmory", SubjectID: "patient-e2e", RepositoryGeneration: "g-e2e"}
	if err := service.Index(ctx, scope, []artifacts.ContentPart{{
		Kind: artifacts.PartText, Text: "El resultado de glucemia en ayunas fue 126 mg/dL.",
		DocumentID: "lab-e2e", SHA256: "sha-e2e", MIMEType: "text/plain", Locator: &artifacts.Locator{Page: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	hits, err := service.Retrieve(ctx, artifacts.RetrievalQuery{Scope: scope, Text: "¿Cuál fue el resultado de glucemia?", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chunk.DocumentID != "lab-e2e" || hits[0].Chunk.Locator.Page != 1 {
		t.Fatalf("unexpected hits: %#v", hits)
	}
	if err := service.store.DeleteGeneration(ctx, scope); err != nil {
		t.Fatal(err)
	}
}
