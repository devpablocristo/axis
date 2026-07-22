package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPFetcher struct {
	client       *http.Client
	allowedHosts []string
}

func NewHTTPFetcher(client *http.Client, allowedHosts []string) (*HTTPFetcher, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	normalized := make([]string, 0, len(allowedHosts))
	for _, host := range allowedHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		host = strings.TrimPrefix(host, "*.")
		if host != "" {
			normalized = append(normalized, host)
		}
	}
	if len(normalized) == 0 {
		return nil, errors.New("artifact fetcher requires an explicit host allowlist")
	}
	fetcher := &HTTPFetcher{allowedHosts: normalized}
	clientCopy := *client
	originalRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !fetcher.hostAllowed(req.URL.Hostname()) {
			return errors.New("artifact redirect host is not allowed")
		}
		if originalRedirect != nil {
			return originalRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many artifact redirects")
		}
		return nil
	}
	fetcher.client = &clientCopy
	return fetcher, nil
}

func (f *HTTPFetcher) Fetch(ctx context.Context, manifest Manifest, dst io.Writer) (string, int64, error) {
	parsed, err := url.Parse(strings.TrimSpace(manifest.ReadURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", 0, errors.New("invalid artifact read URL")
	}
	if !f.hostAllowed(parsed.Hostname()) {
		return "", 0, errors.New("artifact read URL host is not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("build artifact request: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fetch artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("fetch artifact status %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxArtifactBytes {
		return "", 0, ErrArtifactTooLarge
	}
	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		return "", written, fmt.Errorf("stream artifact: %w", err)
	}
	return resp.Header.Get("Content-Type"), written, nil
}

func (f *HTTPFetcher) hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, allowed := range f.allowedHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}
