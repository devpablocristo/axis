package artifacts

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestArtifactCatalogIsTenantScopedAndRejectsManifestOverwrite(t *testing.T) {
	databaseURL := os.Getenv("COMPANION_V2_ARTIFACT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COMPANION_V2_ARTIFACT_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewRepository(pool)
	virployeeID := uuid.New()
	tenantA, tenantB := "artifact-a-"+uuid.NewString(), "artifact-b-"+uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM companion_artifacts WHERE tenant_id=ANY($1)`, []string{tenantA, tenantB})
	})
	scopeA := Scope{TenantID: tenantA, VirployeeID: virployeeID, ProductSurface: "medmory", SubjectID: "patient-a", RepositoryGeneration: "generation-a"}
	scopeB := scopeA
	scopeB.TenantID = tenantB
	manifest := Manifest{DocumentID: "doc-1", Name: "labs.pdf", SourceRef: "opaque/ref", ReadURL: "https://signed.example/secret", SHA256: "abc", MIMEType: "application/pdf", SizeBytes: 100, Required: true}
	recordA, err := repository.UpsertManifest(ctx, scopeA, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertManifest(ctx, scopeB, manifest); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListGeneration(ctx, scopeA)
	if err != nil || len(items) != 1 || items[0].Scope.TenantID != tenantA {
		t.Fatalf("tenant scoped list failed: items=%+v err=%v", items, err)
	}
	if _, err := repository.SetStatus(ctx, tenantB, recordA.ID, StatusStaged, "gs://stage/a", "application/pdf", ""); err != pgx.ErrNoRows {
		t.Fatalf("cross-tenant status mutation must not find row, got %v", err)
	}
	changed := manifest
	changed.SHA256 = "different"
	if _, err := repository.UpsertManifest(ctx, scopeA, changed); err == nil {
		t.Fatal("same document/generation must reject a checksum overwrite")
	}
	var signedURLColumns int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name='companion_artifacts' AND column_name IN ('read_url','signed_url')
	`).Scan(&signedURLColumns); err != nil {
		t.Fatal(err)
	}
	if signedURLColumns != 0 {
		t.Fatal("artifact catalog must never persist signed URL columns")
	}
}
