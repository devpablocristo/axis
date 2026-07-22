package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
)

type extractionStub struct {
	profile string
	parts   []ContentPart
	err     error
}

func (stub *extractionStub) Extract(_ context.Context, request ExtractRequest) ([]ContentPart, error) {
	stub.profile = request.Profile
	return stub.parts, stub.err
}

func blobFromBytes(t *testing.T, data []byte) Blob {
	t.Helper()
	blob, _, _, err := spool(func(destination io.Writer) (string, int64, error) {
		count, writeErr := destination.Write(data)
		return "application/octet-stream", int64(count), writeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blob.Close() })
	return blob
}

func TestOfficeAdapterUsesIsolatedExtractionPort(t *testing.T) {
	archive := new(bytes.Buffer)
	writer := zip.NewWriter(archive)
	entry, _ := writer.Create("word/document.xml")
	_, _ = entry.Write([]byte("document"))
	_ = writer.Close()
	extractor := &extractionStub{parts: []ContentPart{{Kind: PartText, Text: "converted"}}}
	parts, err := (OfficeFormatAdapter{Extractor: extractor}).Adapt(context.Background(), AdaptInput{
		Manifest: Manifest{Name: "report.docx"}, Blob: blobFromBytes(t, archive.Bytes()),
	})
	if err != nil || extractor.profile != "office" || len(parts) != 1 || parts[0].Text != "converted" {
		t.Fatalf("office extraction failed profile=%q parts=%+v err=%v", extractor.profile, parts, err)
	}
}

func TestOfficeAdapterRejectsHighRatioArchiveBeforeExtractor(t *testing.T) {
	archive := new(bytes.Buffer)
	writer := zip.NewWriter(archive)
	entry, _ := writer.Create("word/document.xml")
	_, _ = entry.Write(bytes.Repeat([]byte{0}, 2<<20))
	_ = writer.Close()
	extractor := &extractionStub{}
	_, err := (OfficeFormatAdapter{Extractor: extractor}).Adapt(context.Background(), AdaptInput{
		Manifest: Manifest{Name: "bomb.docx"}, Blob: blobFromBytes(t, archive.Bytes()),
	})
	if err != ErrArtifactTooLarge || extractor.profile != "" {
		t.Fatalf("archive bomb was not rejected before extraction: profile=%q err=%v", extractor.profile, err)
	}
}

func TestConvertedAndNativeMediaRouting(t *testing.T) {
	if _, err := (ImageFormatAdapter{}).Adapt(context.Background(), AdaptInput{Manifest: Manifest{MIMEType: "image/tiff"}}); err != ErrExtractionUnavailable {
		t.Fatalf("TIFF must require converter, got %v", err)
	}
	native, err := (AudioFormatAdapter{}).Adapt(context.Background(), AdaptInput{
		Manifest: Manifest{DocumentID: "audio", MIMEType: "audio/mpeg"}, Stored: StoredArtifact{URI: "gs://stage/audio"},
	})
	if err != nil || len(native) != 1 || native[0].Kind != PartFileData || native[0].Text != "" {
		t.Fatalf("audio must stay native: %+v err=%v", native, err)
	}
}

func TestNativeMediaKeepsOriginalWhenOptionalExtractionFails(t *testing.T) {
	extractor := &extractionStub{err: ErrExtractionUnavailable}
	input := AdaptInput{
		Manifest: Manifest{DocumentID: "audio-1", Name: "sample.mp3", MIMEType: "audio/mpeg", SHA256: "sha"},
		Stored:   StoredArtifact{URI: "gs://tenant/sample.mp3"}, Blob: blobFromBytes(t, []byte("audio")),
	}
	parts, err := (AudioFormatAdapter{Extractor: extractor}).Adapt(context.Background(), input)
	if err != nil || len(parts) != 1 || parts[0].Kind != PartFileData || parts[0].URI != "gs://tenant/sample.mp3" {
		t.Fatalf("native original should remain usable: parts=%+v err=%v", parts, err)
	}
}
