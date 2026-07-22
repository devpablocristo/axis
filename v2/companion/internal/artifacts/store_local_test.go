package artifacts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLocalStoreRoundTripUsesOpaquePrivatePath(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(LocalStoreConfig{RootDir: root, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("verified professional source")
	blob := localTestBlob(t, data)
	scope := Scope{
		OrgID: "organization-sensitive", VirployeeID: uuid.New(), ProductSurface: "knowledge_base",
		SubjectID: "patient-sensitive", RepositoryGeneration: "generation-sensitive",
	}
	manifest := Manifest{
		DocumentID: "document-sensitive", MIMEType: "text/plain", SHA256: checksum(data), SizeBytes: int64(len(data)),
	}
	stored, err := store.PutOriginal(context.Background(), scope, manifest, blob)
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	for _, secret := range []string{scope.OrgID, scope.SubjectID, scope.RepositoryGeneration, manifest.DocumentID} {
		if strings.Contains(stored.URI, secret) {
			t.Fatalf("local URI leaked source identity %q: %s", secret, stored.URI)
		}
	}
	var output bytes.Buffer
	mimeType, size, err := store.GetOriginal(context.Background(), stored, &output)
	if err != nil {
		t.Fatalf("GetOriginal: %v", err)
	}
	if output.String() != string(data) || mimeType != manifest.MIMEType || size != int64(len(data)) {
		t.Fatalf("unexpected round trip mime=%q size=%d output=%q", mimeType, size, output.String())
	}
	ref, _ := url.Parse(stored.URI)
	filename, err := store.resolveRelative(strings.TrimPrefix(ref.Path, "/"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact mode=%o, want 600", info.Mode().Perm())
	}
}

func TestLocalStoreEnforcesCapacityAndRejectsEscapingURI(t *testing.T) {
	first := []byte("first artifact")
	second := []byte("second artifact")
	store, err := NewLocalStore(LocalStoreConfig{RootDir: t.TempDir(), MaxBytes: int64(len(first) + len(second) - 1)})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{OrgID: "organization", VirployeeID: uuid.New(), ProductSurface: "knowledge_base", SubjectID: "professional", RepositoryGeneration: "g1"}
	firstBlob := localTestBlob(t, first)
	if _, err := store.PutOriginal(context.Background(), scope, Manifest{DocumentID: "one", MIMEType: "text/plain", SHA256: checksum(first), SizeBytes: int64(len(first))}, firstBlob); err != nil {
		t.Fatal(err)
	}
	secondBlob := localTestBlob(t, second)
	scope.RepositoryGeneration = "g2"
	if _, err := store.PutOriginal(context.Background(), scope, Manifest{DocumentID: "two", MIMEType: "text/plain", SHA256: checksum(second), SizeBytes: int64(len(second))}, secondBlob); !errors.Is(err, ErrArtifactStoreFull) {
		t.Fatalf("expected bounded store rejection, got %v", err)
	}
	if _, _, err := store.GetOriginal(context.Background(), StoredArtifact{URI: "axis-local-artifact://other/objects/a", SizeBytes: 1}, io.Discard); err == nil {
		t.Fatal("expected foreign local-store URI rejection")
	}
}

func localTestBlob(t *testing.T, data []byte) *fileBlob {
	t.Helper()
	blob, _, _, err := spool(func(dst io.Writer) (string, int64, error) {
		n, writeErr := dst.Write(data)
		return "text/plain", int64(n), writeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blob.Close() })
	return blob
}
