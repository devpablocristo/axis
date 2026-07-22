package knowledgebases

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func knowledgeIngestionRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(NewUseCases(nil)).Routes(router.Group("/v1"))
	return router
}

func TestConnectorIngestionRequiresOwnerOrAdminAndFailsClosed(t *testing.T) {
	baseID := uuid.NewString()
	validBody := `{
		"target":{"virployee_id":"` + uuid.NewString() + `","subject_id":"professional","document_id":"protocol-v7"},
		"source":{"connector":"box","external_id":"file-123","name":"protocol.txt","read_url":"https://files.example.test/signed/protocol","sha256":"` + strings.Repeat("a", 64) + `","mime_type":"text/plain","size_bytes":42}
	}`
	for _, tc := range []struct {
		name       string
		role       string
		body       string
		wantStatus int
	}{
		{name: "member is forbidden before ingestion", role: "member", body: validBody, wantStatus: http.StatusForbidden},
		{name: "invalid manifest", role: "owner", body: strings.Replace(validBody, strings.Repeat("a", 64), "invalid", 1), wantStatus: http.StatusBadRequest},
		{name: "pipeline unavailable", role: "admin", body: validBody, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/knowledge-bases/"+baseID+"/ingestions/connector", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", "tenant-a")
			req.Header.Set("X-Axis-Tenant-Role", tc.role)
			rec := httptest.NewRecorder()
			knowledgeIngestionRouter().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUploadIngestionUsesMultipartContractAndFailsClosed(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("title", "Professional protocol")
	_ = writer.WriteField("virployee_id", uuid.NewString())
	_ = writer.WriteField("subject_id", ProfessionalArtifactSubject)
	_ = writer.WriteField("document_id", "protocol-upload")
	file, err := writer.CreateFormFile("file", "protocol.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("protocol contents"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge-bases/"+uuid.NewString()+"/ingestions/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Tenant-ID", "tenant-a")
	req.Header.Set("X-Axis-Tenant-Role", "owner")
	rec := httptest.NewRecorder()
	knowledgeIngestionRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected fail-closed 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadIngestionRejectsUnauthorizedCallerBeforeParsingBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge-bases/"+uuid.NewString()+"/ingestions/upload", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	req.Header.Set("X-Axis-Tenant-Role", "member")
	rec := httptest.NewRecorder()
	knowledgeIngestionRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 before body parsing, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadIngestionPreflightsBeforeConsumingFileBytes(t *testing.T) {
	boundary := "axis-streaming-boundary"
	prefix := strings.Join([]string{
		"--" + boundary,
		`Content-Disposition: form-data; name="virployee_id"`,
		"",
		uuid.NewString(),
		"--" + boundary,
		`Content-Disposition: form-data; name="subject_id"`,
		"",
		ProfessionalArtifactSubject,
		"--" + boundary,
		`Content-Disposition: form-data; name="file"; filename="large.txt"`,
		"Content-Type: text/plain",
		"",
	}, "\r\n") + "\r\n"
	fileSize := 8 << 20
	body := &countingReader{reader: io.MultiReader(strings.NewReader(prefix), strings.NewReader(strings.Repeat("x", fileSize)))}
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge-bases/"+uuid.NewString()+"/ingestions/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	req.Header.Set("X-Axis-Tenant-Role", "owner")
	rec := httptest.NewRecorder()
	knowledgeIngestionRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected preflight 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if body.read >= int64(fileSize) {
		t.Fatalf("preflight consumed the entire file: read=%d file=%d", body.read, fileSize)
	}
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(dst []byte) (int, error) {
	n, err := r.reader.Read(dst)
	r.read += int64(n)
	return n, err
}
