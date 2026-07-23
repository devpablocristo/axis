package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
)

type CommandPort interface {
	Run(context.Context, string, string, ...string) ([]byte, error)
}

type Adapter struct {
	runner       CommandPort
	whisperModel string
	whisperBin   string
}

func New(runner CommandPort, whisperModel, whisperBin string) *Adapter {
	if strings.TrimSpace(whisperBin) == "" {
		whisperBin = "whisper-cli"
	}
	return &Adapter{runner: runner, whisperModel: strings.TrimSpace(whisperModel), whisperBin: whisperBin}
}

func (adapter *Adapter) Extract(ctx context.Context, workDir, inputPath, profile string) ([]extractor.Part, error) {
	if adapter.runner == nil {
		return nil, extractor.ErrUnavailable
	}
	var parts []extractor.Part
	var err error
	switch profile {
	case "office":
		parts, err = adapter.office(ctx, workDir, inputPath)
	case "ocr_pdf":
		parts, err = adapter.ocrPDF(ctx, workDir, inputPath)
	case "image":
		parts, err = adapter.image(ctx, workDir, inputPath)
	case "audio":
		parts, err = adapter.audio(ctx, workDir, inputPath)
	case "video":
		parts, err = adapter.video(ctx, workDir, inputPath)
	case "dicom":
		parts, err = adapter.dicom(ctx, workDir, inputPath)
	default:
		return nil, extractor.ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	return parts, nil
}

func (adapter *Adapter) office(ctx context.Context, workDir, inputPath string) ([]extractor.Part, error) {
	profileURI := "file://" + filepath.Join(workDir, "libreoffice-profile")
	if _, err := adapter.runner.Run(ctx, workDir, "soffice", "--headless", "--nologo", "--nodefault", "--nolockcheck", "--nofirststartwizard", "-env:UserInstallation="+profileURI, "--convert-to", "pdf", "--outdir", workDir, inputPath); err != nil {
		return nil, err
	}
	pdfs, _ := filepath.Glob(filepath.Join(workDir, "*.pdf"))
	if len(pdfs) == 0 {
		return nil, errors.New("office converter produced no PDF")
	}
	sort.Strings(pdfs)
	data, err := boundedRead(pdfs[0])
	if err != nil {
		return nil, err
	}
	parts := []extractor.Part{{Kind: "inline_data", Data: data, MIMEType: "application/pdf", Name: filepath.Base(pdfs[0])}}
	if text, textErr := adapter.runner.Run(ctx, workDir, "pdftotext", "-layout", pdfs[0], "-"); textErr == nil && strings.TrimSpace(string(text)) != "" {
		parts = append([]extractor.Part{{Kind: "text", Text: string(text), MIMEType: "text/plain"}}, parts...)
	}
	return parts, nil
}

func (adapter *Adapter) ocrPDF(ctx context.Context, workDir, inputPath string) ([]extractor.Part, error) {
	if text, err := adapter.runner.Run(ctx, workDir, "pdftotext", "-layout", inputPath, "-"); err == nil && strings.TrimSpace(string(text)) != "" {
		return []extractor.Part{{Kind: "text", Text: string(text), MIMEType: "text/plain"}}, nil
	}
	prefix := filepath.Join(workDir, "page")
	if _, err := adapter.runner.Run(ctx, workDir, "pdftoppm", "-png", "-r", "150", "-f", "1", "-l", "100", inputPath, prefix); err != nil {
		return nil, err
	}
	images, _ := filepath.Glob(prefix + "-*.png")
	sort.Strings(images)
	parts := make([]extractor.Part, 0, len(images))
	for index, imagePath := range images {
		text, err := adapter.runner.Run(ctx, workDir, "tesseract", imagePath, "stdout")
		if err == nil && strings.TrimSpace(string(text)) != "" {
			parts = append(parts, extractor.Part{Kind: "text", Text: string(text), MIMEType: "text/plain", Locator: &extractor.Locator{Page: index + 1}})
		}
	}
	return parts, nil
}

func (adapter *Adapter) image(ctx context.Context, workDir, inputPath string) ([]extractor.Part, error) {
	output := filepath.Join(workDir, "normalized.png")
	if _, err := adapter.runner.Run(ctx, workDir, "convert", inputPath+"[0]", "-auto-orient", output); err != nil {
		return nil, err
	}
	data, err := boundedRead(output)
	if err != nil {
		return nil, err
	}
	parts := []extractor.Part{{Kind: "inline_data", Data: data, MIMEType: "image/png", Name: "normalized.png"}}
	if text, textErr := adapter.runner.Run(ctx, workDir, "tesseract", output, "stdout"); textErr == nil && strings.TrimSpace(string(text)) != "" {
		parts = append([]extractor.Part{{Kind: "text", Text: string(text), MIMEType: "text/plain"}}, parts...)
	}
	return parts, nil
}

func (adapter *Adapter) audio(ctx context.Context, workDir, inputPath string) ([]extractor.Part, error) {
	if adapter.whisperModel == "" {
		return nil, extractor.ErrUnavailable
	}
	wav := filepath.Join(workDir, "audio.wav")
	if _, err := adapter.runner.Run(ctx, workDir, "ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", inputPath, "-vn", "-ac", "1", "-ar", "16000", wav); err != nil {
		return nil, err
	}
	prefix := filepath.Join(workDir, "transcript")
	if _, err := adapter.runner.Run(ctx, workDir, adapter.whisperBin, "-m", adapter.whisperModel, "-f", wav, "-oj", "-of", prefix); err != nil {
		return nil, err
	}
	return whisperParts(prefix + ".json")
}

func (adapter *Adapter) video(ctx context.Context, workDir, inputPath string) ([]extractor.Part, error) {
	parts := make([]extractor.Part, 0)
	normalized := filepath.Join(workDir, "normalized.mp4")
	if _, err := adapter.runner.Run(ctx, workDir, "ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", inputPath, "-c:v", "libx264", "-c:a", "aac", "-movflags", "+faststart", normalized); err != nil {
		return nil, err
	}
	data, err := boundedRead(normalized)
	if err != nil {
		return nil, err
	}
	parts = append(parts, extractor.Part{Kind: "inline_data", Data: data, MIMEType: "video/mp4", Name: "normalized.mp4"})
	framePattern := filepath.Join(workDir, "frame-%03d.jpg")
	if _, frameErr := adapter.runner.Run(ctx, workDir, "ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", inputPath, "-vf", "fps=1/30", "-frames:v", "60", framePattern); frameErr == nil {
		frames, _ := filepath.Glob(filepath.Join(workDir, "frame-*.jpg"))
		sort.Strings(frames)
		for index, framePath := range frames {
			frame, readErr := boundedRead(framePath)
			if readErr != nil {
				return nil, readErr
			}
			parts = append(parts, extractor.Part{Kind: "inline_data", Data: frame, MIMEType: "image/jpeg", Name: filepath.Base(framePath), Locator: &extractor.Locator{Frame: index + 1, StartMS: int64(index) * 30_000}})
		}
	}
	if adapter.whisperModel != "" {
		if transcript, transcriptErr := adapter.audio(ctx, workDir, inputPath); transcriptErr == nil {
			parts = append(parts, transcript...)
		}
	}
	return parts, nil
}

func (adapter *Adapter) dicom(ctx context.Context, workDir, inputPath string) ([]extractor.Part, error) {
	parts := make([]extractor.Part, 0, 2)
	if metadata, err := adapter.runner.Run(ctx, workDir, "dcmdump", "+L", inputPath); err == nil && strings.TrimSpace(string(metadata)) != "" {
		parts = append(parts, extractor.Part{Kind: "text", Text: string(metadata), MIMEType: "text/plain"})
	}
	output := filepath.Join(workDir, "frame.png")
	if _, err := adapter.runner.Run(ctx, workDir, "dcmj2pnm", "--write-png", inputPath, output); err != nil {
		return nil, err
	}
	frame, err := boundedRead(output)
	if err != nil {
		return nil, err
	}
	parts = append(parts, extractor.Part{Kind: "inline_data", Data: frame, MIMEType: "image/png", Name: "frame.png", Locator: &extractor.Locator{Frame: 1}})
	return parts, nil
}

func boundedRead(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > extractor.MaxDerivativeBytes {
		return nil, extractor.ErrOutputTooLarge
	}
	return os.ReadFile(path)
}

func whisperParts(path string) ([]extractor.Part, error) {
	raw, err := boundedRead(path)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Transcription []struct {
			Text    string `json:"text"`
			Offsets struct {
				From int64 `json:"from"`
				To   int64 `json:"to"`
			} `json:"offsets"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("invalid transcription output")
	}
	parts := make([]extractor.Part, 0, len(payload.Transcription))
	for index, segment := range payload.Transcription {
		if strings.TrimSpace(segment.Text) == "" {
			continue
		}
		start, end := segment.Offsets.From, segment.Offsets.To
		// whisper.cpp offsets are centiseconds. Retain a monotonic fallback when
		// an older binary omits them.
		if end <= start {
			start, end = int64(index)*1000, int64(index+1)*1000
		} else {
			start, end = start*10, end*10
		}
		parts = append(parts, extractor.Part{Kind: "text", Text: strings.TrimSpace(segment.Text), MIMEType: "text/plain", Locator: &extractor.Locator{StartMS: start, EndMS: end}})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("transcription contained no segments: %s", strconv.Itoa(len(payload.Transcription)))
	}
	return parts, nil
}
