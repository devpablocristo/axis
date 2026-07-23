package artifacts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPExtractionClientStreamsVerifiedBlobAndRebindsProvenance(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Axis-Internal-Token") != "local-token" {
			t.Error("internal token was not forwarded")
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var metadata struct {
			Profile string `json:"profile"`
			Scope   struct {
				OrgID string `json:"org_id"`
			} `json:"scope"`
			Manifest struct {
				DocumentID string `json:"document_id"`
			} `json:"manifest"`
		}
		if err := json.Unmarshal([]byte(request.FormValue("metadata")), &metadata); err != nil ||
			metadata.Profile != "office" || metadata.Scope.OrgID != "organization-a" ||
			metadata.Manifest.DocumentID != "doc-1" {
			t.Errorf("invalid metadata: %+v err=%v", metadata, err)
		}
		file, _, err := request.FormFile("artifact")
		if err != nil {
			t.Errorf("artifact form file: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		raw, _ := io.ReadAll(file)
		received = string(raw)
		_ = json.NewEncoder(writer).Encode(map[string]any{"parts": []map[string]any{{
			"kind": "text", "text": "converted", "mime_type": "text/plain",
			"document_id": "wrong", "sha256": "wrong",
		}}})
	}))
	defer server.Close()

	blob, _, _, err := spool(func(destination io.Writer) (string, int64, error) {
		count, writeErr := io.WriteString(destination, "verified artifact bytes")
		return "application/octet-stream", int64(count), writeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blob.Close() }()
	client, err := NewHTTPExtractionClient(server.URL, server.Client(), "local-token")
	if err != nil {
		t.Fatal(err)
	}
	parts, err := client.Extract(context.Background(), ExtractRequest{
		Scope: testScope(), Manifest: Manifest{DocumentID: "doc-1", Name: "report.docx", SHA256: "trusted-hash"},
		Blob: blob, Profile: "office",
	})
	if err != nil {
		t.Fatal(err)
	}
	if received != "verified artifact bytes" || len(parts) != 1 || parts[0].Text != "converted" || parts[0].DocumentID != "doc-1" || parts[0].SHA256 != "trusted-hash" {
		t.Fatalf("unexpected extraction result received=%q parts=%+v", received, parts)
	}
}

func TestHTTPExtractionClientRejectsExtractorOwnedURI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"parts":[{"kind":"file_data","uri":"https://untrusted.invalid/result"}]}`)
	}))
	defer server.Close()
	blob, _, _, err := spool(func(destination io.Writer) (string, int64, error) {
		count, writeErr := io.WriteString(destination, "x")
		return "text/plain", int64(count), writeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blob.Close() }()
	client, _ := NewHTTPExtractionClient(server.URL, server.Client(), "")
	_, err = client.Extract(context.Background(), ExtractRequest{Manifest: Manifest{DocumentID: "doc"}, Blob: blob, Profile: "image"})
	if err == nil || !strings.Contains(err.Error(), "invalid derivative kind") {
		t.Fatalf("expected untrusted URI rejection, got %v", err)
	}
}

func TestHTTPExtractionClientRejectsNonHTTPBaseURL(t *testing.T) {
	for _, value := range []string{"artifact-worker:8080", "file:///tmp/worker.sock", "://broken"} {
		if _, err := NewHTTPExtractionClient(value, nil, "token"); err == nil {
			t.Fatalf("expected invalid base URL %q to be rejected", value)
		}
	}
}
