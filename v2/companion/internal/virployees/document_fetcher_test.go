package virployees

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPDocumentFetcherReadsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Glucosa en ayunas 126 mg/dL (ref 70-100)"))
	}))
	defer srv.Close()

	doc := NewHTTPDocumentFetcher(srv.Client()).Fetch(context.Background(), "labs.txt", srv.URL, "text/plain")
	if !doc.Readable || doc.Content == "" {
		t.Fatalf("expected readable text content, got %+v", doc)
	}
	if doc.Key != "labs.txt" {
		t.Fatalf("expected key preserved, got %q", doc.Key)
	}
}

func TestHTTPDocumentFetcherFlagsNonTextAsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7 ..."))
	}))
	defer srv.Close()

	doc := NewHTTPDocumentFetcher(srv.Client()).Fetch(context.Background(), "study.pdf", srv.URL, "application/pdf")
	if doc.Readable || doc.Content != "" {
		t.Fatalf("PDF must not be read by the text path, got %+v", doc)
	}
	if doc.Note == "" {
		t.Fatal("a non-text document must carry a note explaining why it was not read")
	}
}

func TestHTTPDocumentFetcherHandlesFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	doc := NewHTTPDocumentFetcher(srv.Client()).Fetch(context.Background(), "x", srv.URL, "text/plain")
	if doc.Readable || doc.Note == "" {
		t.Fatalf("a non-2xx fetch must be unreadable with a note, got %+v", doc)
	}
}

func TestIsTextContentType(t *testing.T) {
	for _, ct := range []string{"text/plain", "text/plain; charset=utf-8", "application/json", "application/fhir+json", ""} {
		if !isTextContentType(ct) {
			t.Fatalf("expected %q to be treated as text", ct)
		}
	}
	for _, ct := range []string{"application/pdf", "image/png", "audio/mpeg"} {
		if isTextContentType(ct) {
			t.Fatalf("expected %q to be treated as non-text", ct)
		}
	}
}
