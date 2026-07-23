package toolchain_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devpablocristo/artifact-worker-v2/internal/adapters/out/toolchain"
	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
)

type runnerStub struct{ calls []string }

func (runner *runnerStub) Run(_ context.Context, workDir, name string, arguments ...string) ([]byte, error) {
	runner.calls = append(runner.calls, name)
	switch name {
	case "soffice":
		return nil, os.WriteFile(filepath.Join(workDir, "report.pdf"), []byte("converted-pdf"), 0o600)
	case "pdftotext":
		return []byte("Glucose 126 mg/dL"), nil
	case "convert":
		return nil, os.WriteFile(filepath.Join(workDir, "normalized.png"), []byte("png"), 0o600)
	case "tesseract":
		return []byte("OCR result"), nil
	case "dcmdump":
		return []byte("(0028,0010) Rows 512"), nil
	case "dcmj2pnm":
		return nil, os.WriteFile(filepath.Join(workDir, "frame.png"), []byte("frame"), 0o600)
	}
	return nil, nil
}

func TestOfficeProducesTextAndLayoutPreservingPDF(t *testing.T) {
	workDir := t.TempDir()
	input := filepath.Join(workDir, "report.docx")
	if err := os.WriteFile(input, []byte("office"), 0o600); err != nil {
		t.Fatal(err)
	}
	parts, err := extractor.NewService(toolchain.New(&runnerStub{}, "", "")).Extract(context.Background(), workDir, input, extractor.Metadata{
		Profile: "office", Manifest: extractor.Manifest{DocumentID: "doc-1", Name: "report.docx", SHA256: "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Kind != "text" || parts[1].MIMEType != "application/pdf" || parts[1].DocumentID != "doc-1" || parts[1].SHA256 != "abc" {
		t.Fatalf("unexpected office derivatives: %+v", parts)
	}
}

func TestImageAndDICOMProduceNativeVisualDerivatives(t *testing.T) {
	for _, testCase := range []struct {
		profile  string
		mimeType string
	}{
		{profile: "image", mimeType: "image/png"},
		{profile: "dicom", mimeType: "image/png"},
	} {
		workDir := t.TempDir()
		input := filepath.Join(workDir, "input.bin")
		_ = os.WriteFile(input, []byte("binary"), 0o600)
		parts, err := extractor.NewService(toolchain.New(&runnerStub{}, "", "")).Extract(context.Background(), workDir, input, extractor.Metadata{
			Profile: testCase.profile, Manifest: extractor.Manifest{DocumentID: "doc"},
		})
		if err != nil {
			t.Fatalf("%s: %v", testCase.profile, err)
		}
		found := false
		for _, part := range parts {
			if part.Kind == "inline_data" && part.MIMEType == testCase.mimeType && len(part.Data) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s visual derivative missing: %+v", testCase.profile, parts)
		}
	}
}

func TestAudioRequiresConfiguredTranscriptionModel(t *testing.T) {
	_, err := extractor.NewService(toolchain.New(&runnerStub{}, "", "")).Extract(context.Background(), t.TempDir(), "input.mp3", extractor.Metadata{Profile: "audio"})
	if err != extractor.ErrUnavailable {
		t.Fatalf("expected unavailable transcription without model, got %v", err)
	}
}
