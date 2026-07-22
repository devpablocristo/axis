package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeCatalog struct {
	records  map[uuid.UUID]Record
	existing *Record
}

func (c *fakeCatalog) UpsertManifest(_ context.Context, scope Scope, manifest Manifest) (Record, error) {
	if c.records == nil {
		c.records = map[uuid.UUID]Record{}
	}
	if c.existing != nil {
		record := *c.existing
		record.Manifest.ReadURL = manifest.ReadURL
		c.records[record.ID] = record
		return record, nil
	}
	record := Record{ID: uuid.New(), Scope: scope, Manifest: manifest, Status: StatusReceived}
	c.records[record.ID] = record
	return record, nil
}

func (c *fakeCatalog) SetStatus(_ context.Context, _ string, id uuid.UUID, status Status, uri, actualMIME, code string) (Record, error) {
	record := c.records[id]
	record.Status = status
	if uri != "" {
		record.StagedURI = uri
	}
	if actualMIME != "" {
		record.ActualMIME = actualMIME
	}
	record.ErrorCode = code
	c.records[id] = record
	return record, nil
}

func (c *fakeCatalog) ListGeneration(context.Context, Scope) ([]Record, error) {
	out := make([]Record, 0, len(c.records))
	for _, record := range c.records {
		out = append(out, record)
	}
	return out, nil
}

type fakeFetcher struct {
	content     map[string][]byte
	contentType map[string]string
}

func (f fakeFetcher) Fetch(_ context.Context, manifest Manifest, dst io.Writer) (string, int64, error) {
	data, ok := f.content[manifest.DocumentID]
	if !ok {
		return "", 0, errors.New("missing fixture")
	}
	n, err := dst.Write(data)
	return f.contentType[manifest.DocumentID], int64(n), err
}

type fakeScanner struct{ calls int }

func (s *fakeScanner) Scan(context.Context, Manifest, Blob) error {
	s.calls++
	return nil
}

type fakeStore struct {
	puts       int
	gets       int
	storedData map[string][]byte
}

func (s *fakeStore) PutOriginal(_ context.Context, _ Scope, manifest Manifest, _ Blob) (StoredArtifact, error) {
	s.puts++
	return StoredArtifact{
		URI: "gs://stage/tenant/generation/" + manifest.DocumentID, MIMEType: manifest.MIMEType,
		SHA256: manifest.SHA256, SizeBytes: manifest.SizeBytes, ExpiresAt: time.Now().Add(StagingTTL),
	}, nil
}

func (s *fakeStore) GetOriginal(_ context.Context, stored StoredArtifact, dst io.Writer) (string, int64, error) {
	s.gets++
	data := s.storedData[stored.URI]
	n, err := dst.Write(data)
	return stored.MIMEType, int64(n), err
}

func TestPipelineResumesFromStagedOriginalAfterProductURLExpires(t *testing.T) {
	data := []byte("Glucosa 126 mg/dL")
	scope := testScope()
	record := Record{
		ID: uuid.New(), Scope: scope, Status: StatusStaged, StagedURI: "gs://stage/opaque/doc-1", ActualMIME: "text/plain",
		Manifest:  Manifest{DocumentID: "doc-1", Name: "labs.txt", SHA256: checksum(data), MIMEType: "text/plain", SizeBytes: int64(len(data)), Required: true},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	catalog := &fakeCatalog{existing: &record}
	store := &fakeStore{storedData: map[string][]byte{record.StagedURI: data}}
	scanner := &fakeScanner{}
	pipeline := NewPipeline(catalog, fakeFetcher{}, scanner, store, TextFormatAdapter{})

	result, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: scope, Artifacts: []Manifest{{
		DocumentID: "doc-1", Name: "labs.txt", ReadURL: "https://expired.invalid/doc-1", SHA256: checksum(data),
		MIMEType: "text/plain", SizeBytes: int64(len(data)), Required: true,
	}}})
	if err != nil {
		t.Fatalf("resume from staging: %v", err)
	}
	if store.gets != 1 || store.puts != 0 || scanner.calls != 0 || len(result.Parts) != 1 || result.Parts[0].Text != string(data) {
		t.Fatalf("must resume from verified staging without product URL: store=%+v scanner=%d result=%+v", store, scanner.calls, result)
	}
}

func testScope() Scope {
	return Scope{
		TenantID: "tenant-a", VirployeeID: uuid.New(), ProductSurface: "medmory",
		SubjectID: "patient-a", RepositoryGeneration: "generation-a",
	}
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestPipelineIngestsVerifiedTextAndPreservesProvenance(t *testing.T) {
	data := []byte("Glucosa en ayunas 126 mg/dL")
	catalog := &fakeCatalog{}
	scanner := &fakeScanner{}
	store := &fakeStore{}
	pipeline := NewPipeline(catalog, fakeFetcher{
		content: map[string][]byte{"doc-1": data}, contentType: map[string]string{"doc-1": "text/plain; charset=utf-8"},
	}, scanner, store, TextFormatAdapter{}, PDFFormatAdapter{}, NativeMediaAdapter{})

	result, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{{
		DocumentID: "doc-1", Name: "lab.txt", ReadURL: "https://product/doc-1", SHA256: checksum(data),
		MIMEType: "text/plain", SizeBytes: int64(len(data)), Required: true,
	}}})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if scanner.calls != 1 || store.puts != 1 || len(result.Records) != 1 || result.Records[0].Status != StatusExtracted {
		t.Fatalf("unexpected pipeline effects: scanner=%d store=%d records=%+v", scanner.calls, store.puts, result.Records)
	}
	if len(result.Parts) != 1 || result.Parts[0].Text != string(data) || result.Parts[0].DocumentID != "doc-1" || result.Parts[0].SHA256 != checksum(data) {
		t.Fatalf("unexpected content parts: %+v", result.Parts)
	}
}

func TestPipelineAcceptsJSONSniffedAsPlainText(t *testing.T) {
	data := []byte(`{"glucose":126}`)
	pipeline := NewPipeline(&fakeCatalog{}, fakeFetcher{
		content: map[string][]byte{"json-1": data}, contentType: map[string]string{"json-1": "application/json"},
	}, &fakeScanner{}, &fakeStore{}, TextFormatAdapter{})
	result, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{{
		DocumentID: "json-1", Name: "labs.json", ReadURL: "https://product/json-1", SHA256: checksum(data),
		MIMEType: "application/json", SizeBytes: int64(len(data)), Required: true,
	}}})
	if err != nil || len(result.Parts) != 1 || result.Parts[0].Text != string(data) {
		t.Fatalf("expected valid JSON text ingestion, result=%+v err=%v", result, err)
	}
}

func TestPipelineRejectsChecksumMismatchBeforeScanningOrStaging(t *testing.T) {
	data := []byte("actual")
	scanner := &fakeScanner{}
	store := &fakeStore{}
	pipeline := NewPipeline(&fakeCatalog{}, fakeFetcher{content: map[string][]byte{"doc-1": data}}, scanner, store, TextFormatAdapter{})

	_, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{{
		DocumentID: "doc-1", ReadURL: "https://product/doc-1", SHA256: checksum([]byte("other")), MIMEType: "text/plain", SizeBytes: int64(len(data)), Required: true,
	}}})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if scanner.calls != 0 || store.puts != 0 {
		t.Fatalf("unverified bytes must not be scanned/staged: scanner=%d store=%d", scanner.calls, store.puts)
	}
}

func TestPipelineRejectsFakeMIME(t *testing.T) {
	data := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), 600)...)
	pipeline := NewPipeline(&fakeCatalog{}, fakeFetcher{
		content: map[string][]byte{"doc-1": data}, contentType: map[string]string{"doc-1": "image/png"},
	}, &fakeScanner{}, &fakeStore{}, PDFFormatAdapter{}, NativeMediaAdapter{})

	_, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{{
		DocumentID: "doc-1", Name: "fake.png", ReadURL: "https://product/doc-1", SHA256: checksum(data),
		MIMEType: "image/png", SizeBytes: int64(len(data)), Required: true,
	}}})
	if !errors.Is(err, ErrMIMEMismatch) {
		t.Fatalf("expected MIME mismatch, got %v", err)
	}
}

func TestPipelineFailsWholeDiagnosisWhenRequiredFormatIsUnsupported(t *testing.T) {
	data := []byte{0, 1, 2, 3, 4, 5}
	pipeline := NewPipeline(&fakeCatalog{}, fakeFetcher{content: map[string][]byte{"doc-1": data}, contentType: map[string]string{"doc-1": "application/octet-stream"}}, &fakeScanner{}, &fakeStore{}, TextFormatAdapter{})

	_, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{{
		DocumentID: "doc-1", Name: "unknown.bin", ReadURL: "https://product/doc-1", SHA256: checksum(data),
		MIMEType: "application/octet-stream", SizeBytes: int64(len(data)), Required: true,
	}}})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected unsupported required artifact, got %v", err)
	}
}

func TestPipelineSkipsOptionalUnsupportedArtifactButNeverCreatesEmptyText(t *testing.T) {
	textData := []byte("usable")
	binaryData := []byte{0, 1, 2, 3}
	pipeline := NewPipeline(&fakeCatalog{}, fakeFetcher{
		content:     map[string][]byte{"text": textData, "binary": binaryData},
		contentType: map[string]string{"text": "text/plain", "binary": "application/octet-stream"},
	}, &fakeScanner{}, &fakeStore{}, TextFormatAdapter{})

	result, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{
		{DocumentID: "text", Name: "a.txt", ReadURL: "https://product/text", SHA256: checksum(textData), MIMEType: "text/plain", SizeBytes: int64(len(textData)), Required: true},
		{DocumentID: "binary", Name: "a.bin", ReadURL: "https://product/binary", SHA256: checksum(binaryData), MIMEType: "application/octet-stream", SizeBytes: int64(len(binaryData)), Required: false},
	}})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(result.Parts) != 1 || result.Parts[0].Text != "usable" {
		t.Fatalf("optional binary must not become empty text: %+v", result.Parts)
	}
	if len(result.Records) != 2 || result.Records[1].Status != StatusFailed || result.Records[1].ErrorCode != "unsupported_format" {
		t.Fatalf("optional failure must remain visible in catalog: %+v", result.Records)
	}
}

func TestPipelineRejectsDeclaredLimitsBeforeFetch(t *testing.T) {
	pipeline := NewPipeline(&fakeCatalog{}, fakeFetcher{}, &fakeScanner{}, &fakeStore{}, TextFormatAdapter{})
	_, err := pipeline.Ingest(context.Background(), IngestRequest{Scope: testScope(), Artifacts: []Manifest{{
		DocumentID: "too-big", ReadURL: "https://product/too-big", SizeBytes: MaxArtifactBytes + 1, Required: true,
	}}})
	if !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("expected artifact size rejection, got %v", err)
	}
}

func TestNativeMediaAdapterUsesStagedURIWithoutConvertingBinaryToText(t *testing.T) {
	adapter := NativeMediaAdapter{}
	parts, err := adapter.Adapt(context.Background(), AdaptInput{
		Manifest: Manifest{DocumentID: "image-1", Name: "scan.png", MIMEType: "image/png", SHA256: "abc"},
		Stored:   StoredArtifact{URI: "gs://stage/tenant/image-1"},
	})
	if err != nil {
		t.Fatalf("Adapt: %v", err)
	}
	if len(parts) != 1 || parts[0].Kind != PartFileData || parts[0].URI == "" || parts[0].Text != "" {
		t.Fatalf("expected one native URI part, got %+v", parts)
	}
}
