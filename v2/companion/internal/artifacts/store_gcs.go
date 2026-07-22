package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type GCSStoreConfig struct {
	Bucket      string
	CMEKKey     string
	Prefix      string
	RequireCMEK bool
	Endpoint    string
}

type GCSStore struct {
	config GCSStoreConfig
	tokens oauth2.TokenSource
	client *http.Client
	now    func() time.Time
}

func NewGCSStore(config GCSStoreConfig, tokens oauth2.TokenSource, client *http.Client) (*GCSStore, error) {
	config.Bucket = strings.TrimSpace(config.Bucket)
	config.CMEKKey = strings.TrimSpace(config.CMEKKey)
	config.Prefix = strings.Trim(strings.TrimSpace(config.Prefix), "/")
	config.Endpoint = strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if config.Endpoint == "" {
		config.Endpoint = "https://storage.googleapis.com"
	}
	if config.Bucket == "" || tokens == nil {
		return nil, errors.New("GCS artifact store requires bucket and token source")
	}
	if config.RequireCMEK && config.CMEKKey == "" {
		return nil, errors.New("GCS artifact store requires CMEK")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &GCSStore{config: config, tokens: tokens, client: client, now: time.Now}, nil
}

func (s *GCSStore) PutOriginal(ctx context.Context, scope Scope, manifest Manifest, blob Blob) (StoredArtifact, error) {
	objectName := path.Join(
		s.config.Prefix,
		cleanSegment(scope.OrgID),
		cleanSegment(scope.ProductSurface),
		cleanSegment(scope.VirployeeID.String()),
		"subjects", opaqueSegment(scope.SubjectID),
		"generations", opaqueSegment(scope.RepositoryGeneration),
		"documents", opaqueSegment(manifest.DocumentID),
		"sha256", cleanSegment(manifest.SHA256),
		"original",
	)
	endpoint, _ := url.Parse(s.config.Endpoint + "/upload/storage/v1/b/" + url.PathEscape(s.config.Bucket) + "/o")
	query := endpoint.Query()
	query.Set("uploadType", "media")
	query.Set("name", objectName)
	if s.config.CMEKKey != "" {
		query.Set("kmsKeyName", s.config.CMEKKey)
	}
	endpoint.RawQuery = query.Encode()

	body, err := blob.Open()
	if err != nil {
		return StoredArtifact{}, err
	}
	defer func() { _ = body.Close() }()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("build GCS upload: %w", err)
	}
	token, err := s.tokens.Token()
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("GCS token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", manifest.MIMEType)
	req.ContentLength = blob.Size()
	resp, err := s.client.Do(req)
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("upload GCS original: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StoredArtifact{}, fmt.Errorf("upload GCS original status %d", resp.StatusCode)
	}
	var uploaded struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		return StoredArtifact{}, fmt.Errorf("decode GCS upload: %w", err)
	}
	if uploaded.Name == "" {
		uploaded.Name = objectName
	}
	return StoredArtifact{
		URI: "gs://" + s.config.Bucket + "/" + uploaded.Name, MIMEType: manifest.MIMEType,
		SHA256: manifest.SHA256, SizeBytes: manifest.SizeBytes, ExpiresAt: s.now().UTC().Add(StagingTTL),
	}, nil
}

func (s *GCSStore) GetOriginal(ctx context.Context, stored StoredArtifact, dst io.Writer) (string, int64, error) {
	ref, err := url.Parse(strings.TrimSpace(stored.URI))
	if err != nil || ref.Scheme != "gs" || ref.Host != s.config.Bucket {
		return "", 0, errors.New("staged artifact URI is outside configured bucket")
	}
	objectName := strings.TrimPrefix(ref.Path, "/")
	if objectName == "" || (s.config.Prefix != "" && !strings.HasPrefix(objectName, s.config.Prefix+"/")) {
		return "", 0, errors.New("staged artifact URI is outside configured prefix")
	}
	endpoint, _ := url.Parse(s.config.Endpoint + "/storage/v1/b/" + url.PathEscape(s.config.Bucket) + "/o/" + url.PathEscape(objectName))
	query := endpoint.Query()
	query.Set("alt", "media")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("build GCS download: %w", err)
	}
	token, err := s.tokens.Token()
	if err != nil {
		return "", 0, fmt.Errorf("GCS token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("download GCS original: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("download GCS original status %d", resp.StatusCode)
	}
	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		return "", written, fmt.Errorf("stream GCS original: %w", err)
	}
	return resp.Header.Get("Content-Type"), written, nil
}

func cleanSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "..", "_")
	if value == "" {
		return "unknown"
	}
	return value
}

func opaqueSegment(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return fmt.Sprintf("%x", sum)
}
