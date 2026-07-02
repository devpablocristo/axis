package capabilities

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultManifestSourceMaxBytes int64 = 1024 * 1024

type ManifestSourceRequest struct {
	SourceURL string
	MaxBytes  int64
}

type ManifestSourceFetcher interface {
	FetchManifests(ctx context.Context, req ManifestSourceRequest) ([]Manifest, error)
}

type HTTPManifestSourceFetcher struct {
	Client   *http.Client
	MaxBytes int64
}

func NewHTTPManifestSourceFetcher() HTTPManifestSourceFetcher {
	return HTTPManifestSourceFetcher{
		Client:   &http.Client{Timeout: 10 * time.Second},
		MaxBytes: defaultManifestSourceMaxBytes,
	}
}

func (f HTTPManifestSourceFetcher) FetchManifests(ctx context.Context, req ManifestSourceRequest) ([]Manifest, error) {
	sourceURL := strings.TrimSpace(req.SourceURL)
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("%w: source_url must be an absolute URL", ErrInvalidManifest)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("%w: source_url scheme must be http or https", ErrInvalidManifest)
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = f.MaxBytes
	}
	if maxBytes <= 0 {
		maxBytes = defaultManifestSourceMaxBytes
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build manifest source request: %v", ErrInvalidManifest, err)
	}
	httpReq.Header.Set("Accept", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetch capability manifest source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: manifest source returned HTTP %d", ErrInvalidManifest, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read capability manifest source: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("%w: manifest source exceeds %d bytes", ErrInvalidManifest, maxBytes)
	}
	return decodeManifestFile(bytes.TrimSpace(raw))
}
