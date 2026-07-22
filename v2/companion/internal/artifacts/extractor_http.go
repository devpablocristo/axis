package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxExtractionResponseBytes = 96 << 20

// HTTPExtractionClient is the outbound adapter for the isolated converter. It
// streams the already verified blob; neither product URLs nor credentials cross
// this boundary.
type HTTPExtractionClient struct {
	baseURL string
	client  *http.Client
	token   string
}

func NewHTTPExtractionClient(baseURL string, client *http.Client, token string) (*HTTPExtractionClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, ErrExtractionUnavailable
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("artifact extractor base URL must be an absolute HTTP URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &HTTPExtractionClient{baseURL: baseURL, client: client, token: strings.TrimSpace(token)}, nil
}

func (c *HTTPExtractionClient) Extract(ctx context.Context, input ExtractRequest) ([]ContentPart, error) {
	if c == nil || c.client == nil || input.Blob == nil || strings.TrimSpace(input.Profile) == "" {
		return nil, ErrExtractionUnavailable
	}
	reader, err := input.Blob.Open()
	if err != nil {
		return nil, err
	}
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	writeDone := make(chan error, 1)
	go func() {
		defer func() { _ = reader.Close() }()
		metadata, marshalErr := json.Marshal(struct {
			Scope    Scope    `json:"scope"`
			Manifest Manifest `json:"manifest"`
			Profile  string   `json:"profile"`
		}{Scope: input.Scope, Manifest: input.Manifest, Profile: input.Profile})
		if marshalErr != nil {
			_ = pipeWriter.CloseWithError(marshalErr)
			writeDone <- marshalErr
			return
		}
		if fieldErr := multipartWriter.WriteField("metadata", string(metadata)); fieldErr != nil {
			_ = pipeWriter.CloseWithError(fieldErr)
			writeDone <- fieldErr
			return
		}
		part, createErr := multipartWriter.CreateFormFile("artifact", "artifact.bin")
		if createErr == nil {
			_, createErr = io.Copy(part, reader)
		}
		if closeErr := multipartWriter.Close(); createErr == nil {
			createErr = closeErr
		}
		if createErr != nil {
			_ = pipeWriter.CloseWithError(createErr)
		} else {
			_ = pipeWriter.Close()
		}
		writeDone <- createErr
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/extract", pipeReader)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		return nil, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	if c.token != "" {
		request.Header.Set("X-Axis-Internal-Token", c.token)
	}
	response, requestErr := c.client.Do(request)
	writeErr := <-writeDone
	if requestErr != nil {
		return nil, fmt.Errorf("artifact extraction transport: %w", requestErr)
	}
	defer func() { _ = response.Body.Close() }()
	if writeErr != nil {
		return nil, fmt.Errorf("stream artifact extraction request: %w", writeErr)
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		if response.StatusCode == http.StatusServiceUnavailable {
			return nil, ErrExtractionUnavailable
		}
		return nil, fmt.Errorf("artifact extraction status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxExtractionResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxExtractionResponseBytes {
		return nil, errors.New("artifact extraction response exceeds limit")
	}
	var payload struct {
		Parts []ContentPart `json:"parts"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("invalid artifact extraction response")
	}
	if len(payload.Parts) == 0 || len(payload.Parts) > 1024 {
		return nil, ErrEmptyDerivative
	}
	var derivativeBytes int64
	for index := range payload.Parts {
		part := &payload.Parts[index]
		if !oneOfPartKind(part.Kind, PartText, PartInlineData) || strings.TrimSpace(part.URI) != "" {
			return nil, errors.New("artifact extractor returned an invalid derivative kind")
		}
		derivativeBytes += int64(len(part.Text) + len(part.Data))
		if derivativeBytes > maxExtractionResponseBytes {
			return nil, errors.New("artifact extraction derivatives exceed limit")
		}
		part.DocumentID = input.Manifest.DocumentID
		part.SHA256 = input.Manifest.SHA256
		if part.Name == "" {
			part.Name = input.Manifest.Name
		}
	}
	if !usableParts(payload.Parts) {
		return nil, ErrEmptyDerivative
	}
	return payload.Parts, nil
}

func oneOfPartKind(value PartKind, values ...PartKind) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
