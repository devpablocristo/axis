package artifacts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestGCSStoreUsesCMEKAndOpaqueTenantScopedObjectName(t *testing.T) {
	var gotName, gotKMS, gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("verified bytes"))
			return
		}
		gotName = r.URL.Query().Get("name")
		gotKMS = r.URL.Query().Get("kmsKeyName")
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"` + gotName + `"}`))
	}))
	defer server.Close()

	store, err := NewGCSStore(GCSStoreConfig{
		Bucket: "axis-stage", Prefix: "axis-v2/staging", CMEKKey: "projects/p/locations/l/keyRings/r/cryptoKeys/k",
		RequireCMEK: true, Endpoint: server.URL,
	}, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"}), server.Client())
	if err != nil {
		t.Fatalf("NewGCSStore: %v", err)
	}
	blob := &memoryBlob{data: []byte("verified bytes")}
	stored, err := store.PutOriginal(context.Background(), testScope(), Manifest{
		DocumentID: "patient-lab.pdf", MIMEType: "application/pdf", SHA256: "abc", SizeBytes: blob.Size(),
	}, blob)
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	if gotKMS == "" || gotAuth != "Bearer test-token" || gotBody != "verified bytes" {
		t.Fatalf("upload missing security/data: kms=%q auth=%q body=%q", gotKMS, gotAuth, gotBody)
	}
	if !strings.Contains(gotName, "tenant-a/medmory/") || strings.Contains(gotName, "patient-a") || strings.Contains(gotName, "patient-lab.pdf") {
		t.Fatalf("object must be tenant-scoped with opaque subject/document segments: %q", gotName)
	}
	if !strings.HasPrefix(stored.URI, "gs://axis-stage/") || stored.ExpiresAt.IsZero() {
		t.Fatalf("unexpected stored artifact: %+v", stored)
	}
	var downloaded strings.Builder
	mimeType, size, err := store.GetOriginal(context.Background(), stored, &downloaded)
	if err != nil {
		t.Fatalf("GetOriginal: %v", err)
	}
	if mimeType != "application/pdf" || size != int64(len("verified bytes")) || downloaded.String() != "verified bytes" {
		t.Fatalf("unexpected staged download mime=%q size=%d body=%q", mimeType, size, downloaded.String())
	}
}

func TestGCSStoreRejectsMissingCMEKWhenRequired(t *testing.T) {
	_, err := NewGCSStore(GCSStoreConfig{Bucket: "axis-stage", RequireCMEK: true}, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"}), nil)
	if err == nil {
		t.Fatal("expected missing CMEK error")
	}
}

type memoryBlob struct{ data []byte }

func (b *memoryBlob) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b.data))), nil
}
func (b *memoryBlob) Size() int64 { return int64(len(b.data)) }
