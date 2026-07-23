package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devpablocristo/artifact-worker-v2/internal/adapters/out/toolchain"
	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
)

type workerRunner struct{}

func (workerRunner) Run(_ context.Context, workDir, name string, _ ...string) ([]byte, error) {
	if name == "convert" {
		return nil, os.WriteFile(filepath.Join(workDir, "normalized.png"), []byte("png"), 0o600)
	}
	if name == "tesseract" {
		return []byte("text"), nil
	}
	return nil, nil
}

func extractionRequest(t *testing.T, token, declaredHash string) *http.Request {
	t.Helper()
	artifact := []byte("image bytes")
	if declaredHash == "" {
		sum := sha256.Sum256(artifact)
		declaredHash = hex.EncodeToString(sum[:])
	}
	metadata, _ := json.Marshal(extractor.Metadata{
		Profile: "image", Scope: extractor.Scope{OrgID: "organization-1"},
		Manifest: extractor.Manifest{DocumentID: "doc-1", Name: "scan.tiff", SHA256: declaredHash, SizeBytes: int64(len(artifact))},
	})
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("metadata", string(metadata))
	part, _ := writer.CreateFormFile("artifact", "artifact.bin")
	_, _ = part.Write(artifact)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/v1/extract", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Axis-Internal-Token", token)
	return request
}

func TestHandlerAuthenticatesAndChecksArtifactHash(t *testing.T) {
	handler := NewHandler(extractor.NewService(toolchain.New(workerRunner{}, "", "")), "internal-token").Routes()
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, extractionRequest(t, "wrong", ""))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}

	tampered := httptest.NewRecorder()
	handler.ServeHTTP(tampered, extractionRequest(t, "internal-token", "bad-hash"))
	if tampered.Code != http.StatusBadRequest {
		t.Fatalf("expected checksum rejection, got %d: %s", tampered.Code, tampered.Body.String())
	}

	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, extractionRequest(t, "internal-token", ""))
	if valid.Code != http.StatusOK || !bytes.Contains(valid.Body.Bytes(), []byte(`"document_id":"doc-1"`)) {
		t.Fatalf("unexpected extraction response: %d %s", valid.Code, valid.Body.String())
	}
}

func TestExtractionMetadataUsesPublishedSnakeCaseContract(t *testing.T) {
	raw, err := json.Marshal(extractor.Metadata{
		Profile: "image",
		Scope: extractor.Scope{
			OrgID: "organization-1", VirployeeID: "11111111-1111-4111-8111-111111111111",
			ProductSurface: "product-a", SubjectID: "subject-1", RepositoryGeneration: "generation-1",
		},
		Manifest: extractor.Manifest{
			DocumentID: "doc-1", Name: "scan.tiff", SHA256: strings.Repeat("a", 64),
			MIMEType: "image/tiff", SizeBytes: 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"org_id"`, `"virployee_id"`, `"product_surface"`, `"repository_generation"`,
		`"document_id"`, `"mime_type"`, `"size_bytes"`,
	} {
		if !bytes.Contains(raw, []byte(field)) {
			t.Fatalf("published metadata field %s missing from %s", field, raw)
		}
	}
}
