package artifacts

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHTTPFetcherRequiresExplicitAllowlistAndStreamsAllowedHost(t *testing.T) {
	if _, err := NewHTTPFetcher(nil, nil); err == nil {
		t.Fatal("expected empty allowlist to fail")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("verified"))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	fetcher, err := NewHTTPFetcher(server.Client(), []string{parsed.Hostname()})
	if err != nil {
		t.Fatalf("NewHTTPFetcher: %v", err)
	}
	var dst bytes.Buffer
	mimeType, size, err := fetcher.Fetch(context.Background(), Manifest{ReadURL: server.URL}, &dst)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if mimeType != "text/plain" || size != 8 || dst.String() != "verified" {
		t.Fatalf("unexpected fetch result mime=%q size=%d body=%q", mimeType, size, dst.String())
	}
}

func TestHTTPFetcherRejectsUnlistedHostBeforeNetwork(t *testing.T) {
	fetcher, err := NewHTTPFetcher(&http.Client{}, []string{"storage.example"})
	if err != nil {
		t.Fatalf("NewHTTPFetcher: %v", err)
	}
	var dst bytes.Buffer
	if _, _, err := fetcher.Fetch(context.Background(), Manifest{ReadURL: "http://169.254.169.254/latest/meta-data"}, &dst); err == nil {
		t.Fatal("expected metadata host to be rejected")
	}
}
