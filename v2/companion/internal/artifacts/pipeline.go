package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

type Pipeline struct {
	catalog ArtifactCatalogPort
	fetcher ArtifactFetcherPort
	scanner MalwareScannerPort
	store   ArtifactStorePort
	formats []FormatAdapter
	indexer ArtifactIndexerPort
}

func (p *Pipeline) SetIndexer(indexer ArtifactIndexerPort) { p.indexer = indexer }

func NewPipeline(catalog ArtifactCatalogPort, fetcher ArtifactFetcherPort, scanner MalwareScannerPort, store ArtifactStorePort, formats ...FormatAdapter) *Pipeline {
	return &Pipeline{catalog: catalog, fetcher: fetcher, scanner: scanner, store: store, formats: formats}
}

func (p *Pipeline) Ingest(ctx context.Context, req IngestRequest) (IngestResult, error) {
	if p.catalog == nil || p.fetcher == nil || p.scanner == nil || p.store == nil {
		return IngestResult{}, errors.New("artifact pipeline is not fully configured")
	}
	if err := validateRequest(req); err != nil {
		return IngestResult{}, err
	}

	result := IngestResult{Records: make([]Record, 0, len(req.Artifacts))}
	if req.Progress != nil {
		if err := req.Progress(ctx, StatusExtracting); err != nil {
			return IngestResult{}, err
		}
	}
	for _, manifest := range req.Artifacts {
		record, err := p.catalog.UpsertManifest(ctx, req.Scope, manifest)
		if err != nil {
			return IngestResult{}, err
		}
		if record.StagedURI == "" {
			record, _ = p.catalog.SetStatus(ctx, req.Scope.TenantID, record.ID, StatusStaging, "", "", "")
		}

		parts, updated, err := p.ingestOne(ctx, req.Scope, record)
		if err != nil {
			code := stableErrorCode(err)
			failed, _ := p.catalog.SetStatus(ctx, req.Scope.TenantID, record.ID, StatusFailed, "", "", code)
			result.Records = append(result.Records, failed)
			if manifest.Required {
				return IngestResult{}, fmt.Errorf("required artifact %s failed: %w", manifest.DocumentID, err)
			}
			continue
		}
		result.Records = append(result.Records, updated)
		result.Parts = append(result.Parts, parts...)
	}
	if len(result.Parts) == 0 {
		return IngestResult{}, ErrEmptyDerivative
	}
	if p.indexer != nil {
		if req.Progress != nil {
			if err := req.Progress(ctx, StatusIndexing); err != nil {
				return IngestResult{}, err
			}
		}
		for i, record := range result.Records {
			if record.Status == StatusExtracted {
				result.Records[i], _ = p.catalog.SetStatus(ctx, req.Scope.TenantID, record.ID, StatusIndexing, record.StagedURI, record.ActualMIME, "")
			}
		}
		if err := p.indexer.Index(ctx, req.Scope, result.Parts); err != nil {
			for i, record := range result.Records {
				if record.Status == StatusIndexing {
					result.Records[i], _ = p.catalog.SetStatus(ctx, req.Scope.TenantID, record.ID, StatusFailed, record.StagedURI, record.ActualMIME, "artifact_indexing_failed")
				}
			}
			return IngestResult{}, fmt.Errorf("%w: %v", ErrIndexingFailed, err)
		}
		for i, record := range result.Records {
			if record.Status == StatusIndexing {
				result.Records[i], _ = p.catalog.SetStatus(ctx, req.Scope.TenantID, record.ID, StatusIndexed, record.StagedURI, record.ActualMIME, "")
			}
		}
	}
	return result, nil
}

func (p *Pipeline) ingestOne(ctx context.Context, scope Scope, record Record) ([]ContentPart, Record, error) {
	fromStaging := record.StagedURI != ""
	stored := StoredArtifact{
		URI: record.StagedURI, MIMEType: record.ActualMIME, SHA256: record.Manifest.SHA256,
		SizeBytes: record.Manifest.SizeBytes, ExpiresAt: record.ExpiresAt,
	}
	blob, responseMIME, checksum, err := spool(func(dst io.Writer) (string, int64, error) {
		if fromStaging {
			return p.store.GetOriginal(ctx, stored, dst)
		}
		return p.fetcher.Fetch(ctx, record.Manifest, dst)
	})
	if err != nil {
		return nil, record, err
	}
	defer func() { _ = blob.Close() }()

	if record.Manifest.SizeBytes > 0 && record.Manifest.SizeBytes != blob.Size() {
		return nil, record, ErrSizeMismatch
	}
	if declared := strings.ToLower(strings.TrimSpace(record.Manifest.SHA256)); declared != "" && declared != checksum {
		return nil, record, ErrChecksumMismatch
	}
	actualMIME, err := sniffMIME(blob, responseMIME)
	if err != nil {
		return nil, record, err
	}
	if !mimeCompatible(record.Manifest.MIMEType, actualMIME) {
		return nil, record, ErrMIMEMismatch
	}
	record.Manifest.SHA256 = checksum
	record.Manifest.SizeBytes = blob.Size()
	record.Manifest.MIMEType = actualMIME

	if !fromStaging {
		if err := p.scanner.Scan(ctx, record.Manifest, blob); err != nil {
			return nil, record, fmt.Errorf("malware scan failed: %w", err)
		}
		stored, err = p.store.PutOriginal(ctx, scope, record.Manifest, blob)
		if err != nil {
			return nil, record, fmt.Errorf("stage original: %w", err)
		}
		record, _ = p.catalog.SetStatus(ctx, scope.TenantID, record.ID, StatusStaged, stored.URI, actualMIME, "")
	}
	record, _ = p.catalog.SetStatus(ctx, scope.TenantID, record.ID, StatusExtracting, stored.URI, actualMIME, "")

	adapter := p.adapterFor(actualMIME, record.Manifest.Name)
	if adapter == nil {
		return nil, record, ErrUnsupportedFormat
	}
	parts, err := adapter.Adapt(ctx, AdaptInput{Scope: scope, Manifest: record.Manifest, Stored: stored, Blob: blob})
	if err != nil {
		return nil, record, fmt.Errorf("adapt with %s: %w", adapter.Name(), err)
	}
	if !usableParts(parts) {
		return nil, record, ErrEmptyDerivative
	}
	record, err = p.catalog.SetStatus(ctx, scope.TenantID, record.ID, StatusExtracted, stored.URI, actualMIME, "")
	return parts, record, err
}

func validateRequest(req IngestRequest) error {
	if strings.TrimSpace(req.Scope.TenantID) == "" || req.Scope.VirployeeID.String() == "" || strings.TrimSpace(req.Scope.SubjectID) == "" || strings.TrimSpace(req.Scope.RepositoryGeneration) == "" {
		return errors.New("artifact scope is incomplete")
	}
	seen := map[string]struct{}{}
	var total int64
	for _, item := range req.Artifacts {
		if strings.TrimSpace(item.DocumentID) == "" || strings.TrimSpace(item.ReadURL) == "" {
			return errors.New("artifact manifest is incomplete")
		}
		if item.SizeBytes < 0 || item.SizeBytes > MaxArtifactBytes {
			return ErrArtifactTooLarge
		}
		if _, duplicate := seen[item.DocumentID]; duplicate {
			return errors.New("artifact manifest contains duplicate document_id")
		}
		seen[item.DocumentID] = struct{}{}
		total += item.SizeBytes
		if total > MaxDiagnosisBytes {
			return ErrDiagnosisTooLarge
		}
	}
	return nil
}

func (p *Pipeline) adapterFor(mimeType, filename string) FormatAdapter {
	for _, adapter := range p.formats {
		if adapter.Supports(mimeType, filename) {
			return adapter
		}
	}
	return nil
}

func sniffMIME(blob Blob, responseMIME string) (string, error) {
	r, err := blob.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()
	buf := make([]byte, 512)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	}
	actual := normalizeMIME(http.DetectContentType(buf[:n]))
	if actual == "application/octet-stream" {
		if parsed := normalizeMIME(responseMIME); parsed != "" {
			actual = parsed
		}
	}
	return actual, nil
}

func normalizeMIME(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return value
	}
	return parsed
}

func mimeCompatible(declared, actual string) bool {
	declared = normalizeMIME(declared)
	actual = normalizeMIME(actual)
	if declared == "" || declared == "application/octet-stream" {
		return true
	}
	if declared == actual {
		return true
	}
	if actual == "application/zip" && isOfficeContainerMIME(declared) {
		return true
	}
	// Browsers and net/http commonly classify JSON, XML, CSV and Markdown as
	// text/plain. They are mutually compatible text encodings, but never
	// compatible with an image/media declaration.
	return isTextualMIME(declared) && isTextualMIME(actual)
}

func isOfficeContainerMIME(value string) bool {
	value = normalizeMIME(value)
	return strings.Contains(value, "officedocument") || strings.Contains(value, "opendocument")
}

func isTextualMIME(value string) bool {
	value = normalizeMIME(value)
	if strings.HasPrefix(value, "text/") {
		return true
	}
	return value == "application/json" || value == "application/xml" || value == "application/xhtml+xml" ||
		strings.HasSuffix(value, "+json") || strings.HasSuffix(value, "+xml")
}

func usableParts(parts []ContentPart) bool {
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" || len(part.Data) > 0 || strings.TrimSpace(part.URI) != "" {
			return true
		}
	}
	return false
}

func stableErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrArtifactTooLarge):
		return "artifact_too_large"
	case errors.Is(err, ErrChecksumMismatch):
		return "checksum_mismatch"
	case errors.Is(err, ErrSizeMismatch):
		return "size_mismatch"
	case errors.Is(err, ErrMIMEMismatch):
		return "mime_mismatch"
	case errors.Is(err, ErrUnsupportedFormat):
		return "unsupported_format"
	case errors.Is(err, ErrEmptyDerivative):
		return "empty_derivative"
	case errors.Is(err, ErrIndexingFailed):
		return "artifact_indexing_failed"
	case errors.Is(err, ErrExtractionUnavailable):
		return "artifact_extraction_unavailable"
	default:
		return "artifact_processing_failed"
	}
}

// StableErrorCode maps infrastructure errors to ledger-safe operational codes.
// It intentionally never returns the raw error text.
func StableErrorCode(err error) string { return stableErrorCode(err) }
