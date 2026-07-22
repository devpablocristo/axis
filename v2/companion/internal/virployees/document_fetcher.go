package virployees

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchedDocument is a document the assist flow pulled from its presigned URL.
// Readable is true when we extracted usable text; otherwise Note explains why
// (a binary/non-text type pending multimodal support, or a fetch error).
type FetchedDocument struct {
	Key         string `json:"key"`
	ContentType string `json:"content_type,omitempty"`
	Content     string `json:"content,omitempty"`
	Readable    bool   `json:"readable"`
	Note        string `json:"note,omitempty"`
}

// DocumentFetcherPort pulls a document's content from a (presigned) URL. The
// product sends references only; Axis reads them on demand (pull model).
type DocumentFetcherPort interface {
	Fetch(ctx context.Context, key, readURL, declaredContentType string) FetchedDocument
}

const (
	maxDocumentBytes = 2 << 20 // 2 MiB per document
	docFetchTimeout  = 15 * time.Second
)

// HTTPDocumentFetcher fetches a document over HTTP(S). It bounds size and time.
// Only text-like content is extracted for now; binary types (PDF, image, audio)
// are flagged as pending multimodal support rather than shoved at a text model.
type HTTPDocumentFetcher struct {
	client *http.Client
}

func NewHTTPDocumentFetcher(client *http.Client) *HTTPDocumentFetcher {
	if client == nil {
		client = &http.Client{Timeout: docFetchTimeout}
	}
	return &HTTPDocumentFetcher{client: client}
}

func (f *HTTPDocumentFetcher) Fetch(ctx context.Context, key, readURL, declaredContentType string) FetchedDocument {
	doc := FetchedDocument{Key: key, ContentType: strings.TrimSpace(declaredContentType)}
	if strings.TrimSpace(readURL) == "" {
		doc.Note = "no read_url"
		return doc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readURL, nil)
	if err != nil {
		doc.Note = "invalid read_url"
		return doc
	}
	resp, err := f.client.Do(req)
	if err != nil {
		doc.Note = "could not fetch document"
		return doc
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		doc.Note = fmt.Sprintf("fetch failed with status %d", resp.StatusCode)
		return doc
	}
	if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
		doc.ContentType = ct
	}
	if !isTextContentType(doc.ContentType) {
		doc.Note = "non-text content not read yet (multimodal pending)"
		return doc
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentBytes))
	if err != nil {
		doc.Note = "could not read document body"
		return doc
	}
	doc.Content = string(body)
	doc.Readable = true
	return doc
}

func isTextContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/xhtml+xml", "":
		return true
	}
	return strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml")
}
