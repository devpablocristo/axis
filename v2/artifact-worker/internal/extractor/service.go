package extractor

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
)

type Service struct {
	runner       Runner
	whisperModel string
	whisperBin   string
}

func NewService(runner Runner, whisperModel, whisperBin string) *Service {
	if runner == nil {
		runner = OSRunner{}
	}
	if strings.TrimSpace(whisperBin) == "" {
		whisperBin = "whisper-cli"
	}
	return &Service{runner: runner, whisperModel: strings.TrimSpace(whisperModel), whisperBin: whisperBin}
}

func (service *Service) Extract(ctx context.Context, workDir, inputPath string, metadata Metadata) ([]Part, error) {
	if strings.TrimSpace(workDir) == "" || strings.TrimSpace(inputPath) == "" || strings.TrimSpace(metadata.Profile) == "" {
		return nil, ErrInvalidRequest
	}
	var parts []Part
	var err error
	switch metadata.Profile {
	case "office":
		parts, err = service.office(ctx, workDir, inputPath)
	case "ocr_pdf":
		parts, err = service.ocrPDF(ctx, workDir, inputPath)
	case "image":
		parts, err = service.image(ctx, workDir, inputPath)
	case "audio":
		parts, err = service.audio(ctx, workDir, inputPath)
	case "video":
		parts, err = service.video(ctx, workDir, inputPath)
	case "dicom":
		parts, err = service.dicom(ctx, workDir, inputPath)
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, ErrInvalidRequest
	}
	var total int64
	for index := range parts {
		parts[index].DocumentID = metadata.Manifest.DocumentID
		parts[index].SHA256 = metadata.Manifest.SHA256
		if parts[index].Name == "" {
			parts[index].Name = metadata.Manifest.Name
		}
		total += int64(len(parts[index].Text) + len(parts[index].Data))
		if total > MaxDerivativeBytes {
			return nil, ErrOutputTooLarge
		}
	}
	return parts, nil
}

func (service *Service) office(ctx context.Context, workDir, inputPath string) ([]Part, error) {
	profileURI := "file://" + filepath.Join(workDir, "libreoffice-profile")
	if _, err := service.runner.Run(ctx, workDir, "soffice", "--headless", "--nologo", "--nodefault", "--nolockcheck", "--nofirststartwizard", "-env:UserInstallation="+profileURI, "--convert-to", "pdf", "--outdir", workDir, inputPath); err != nil {
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
	parts := []Part{{Kind: "inline_data", Data: data, MIMEType: "application/pdf", Name: filepath.Base(pdfs[0])}}
	if text, textErr := service.runner.Run(ctx, workDir, "pdftotext", "-layout", pdfs[0], "-"); textErr == nil && strings.TrimSpace(string(text)) != "" {
		parts = append([]Part{{Kind: "text", Text: string(text), MIMEType: "text/plain"}}, parts...)
	}
	return parts, nil
}

func (service *Service) ocrPDF(ctx context.Context, workDir, inputPath string) ([]Part, error) {
	if text, err := service.runner.Run(ctx, workDir, "pdftotext", "-layout", inputPath, "-"); err == nil && strings.TrimSpace(string(text)) != "" {
		return []Part{{Kind: "text", Text: string(text), MIMEType: "text/plain"}}, nil
	}
	prefix := filepath.Join(workDir, "page")
	if _, err := service.runner.Run(ctx, workDir, "pdftoppm", "-png", "-r", "150", "-f", "1", "-l", "100", inputPath, prefix); err != nil {
		return nil, err
	}
	images, _ := filepath.Glob(prefix + "-*.png")
	sort.Strings(images)
	parts := make([]Part, 0, len(images))
	for index, imagePath := range images {
		text, err := service.runner.Run(ctx, workDir, "tesseract", imagePath, "stdout")
		if err == nil && strings.TrimSpace(string(text)) != "" {
			parts = append(parts, Part{Kind: "text", Text: string(text), MIMEType: "text/plain", Locator: &Locator{Page: index + 1}})
		}
	}
	return parts, nil
}

func (service *Service) image(ctx context.Context, workDir, inputPath string) ([]Part, error) {
	output := filepath.Join(workDir, "normalized.png")
	if _, err := service.runner.Run(ctx, workDir, "convert", inputPath+"[0]", "-auto-orient", output); err != nil {
		return nil, err
	}
	data, err := boundedRead(output)
	if err != nil {
		return nil, err
	}
	parts := []Part{{Kind: "inline_data", Data: data, MIMEType: "image/png", Name: "normalized.png"}}
	if text, textErr := service.runner.Run(ctx, workDir, "tesseract", output, "stdout"); textErr == nil && strings.TrimSpace(string(text)) != "" {
		parts = append([]Part{{Kind: "text", Text: string(text), MIMEType: "text/plain"}}, parts...)
	}
	return parts, nil
}

func (service *Service) audio(ctx context.Context, workDir, inputPath string) ([]Part, error) {
	if service.whisperModel == "" {
		return nil, ErrUnavailable
	}
	wav := filepath.Join(workDir, "audio.wav")
	if _, err := service.runner.Run(ctx, workDir, "ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", inputPath, "-vn", "-ac", "1", "-ar", "16000", wav); err != nil {
		return nil, err
	}
	prefix := filepath.Join(workDir, "transcript")
	if _, err := service.runner.Run(ctx, workDir, service.whisperBin, "-m", service.whisperModel, "-f", wav, "-oj", "-of", prefix); err != nil {
		return nil, err
	}
	return whisperParts(prefix + ".json")
}

func (service *Service) video(ctx context.Context, workDir, inputPath string) ([]Part, error) {
	parts := make([]Part, 0)
	normalized := filepath.Join(workDir, "normalized.mp4")
	if _, err := service.runner.Run(ctx, workDir, "ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", inputPath, "-c:v", "libx264", "-c:a", "aac", "-movflags", "+faststart", normalized); err != nil {
		return nil, err
	}
	data, err := boundedRead(normalized)
	if err != nil {
		return nil, err
	}
	parts = append(parts, Part{Kind: "inline_data", Data: data, MIMEType: "video/mp4", Name: "normalized.mp4"})
	framePattern := filepath.Join(workDir, "frame-%03d.jpg")
	if _, frameErr := service.runner.Run(ctx, workDir, "ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-i", inputPath, "-vf", "fps=1/30", "-frames:v", "60", framePattern); frameErr == nil {
		frames, _ := filepath.Glob(filepath.Join(workDir, "frame-*.jpg"))
		sort.Strings(frames)
		for index, framePath := range frames {
			frame, readErr := boundedRead(framePath)
			if readErr != nil {
				return nil, readErr
			}
			parts = append(parts, Part{Kind: "inline_data", Data: frame, MIMEType: "image/jpeg", Name: filepath.Base(framePath), Locator: &Locator{Frame: index + 1, StartMS: int64(index) * 30_000}})
		}
	}
	if service.whisperModel != "" {
		if transcript, transcriptErr := service.audio(ctx, workDir, inputPath); transcriptErr == nil {
			parts = append(parts, transcript...)
		}
	}
	return parts, nil
}

func (service *Service) dicom(ctx context.Context, workDir, inputPath string) ([]Part, error) {
	parts := make([]Part, 0, 2)
	if metadata, err := service.runner.Run(ctx, workDir, "dcmdump", "+L", inputPath); err == nil && strings.TrimSpace(string(metadata)) != "" {
		parts = append(parts, Part{Kind: "text", Text: string(metadata), MIMEType: "text/plain"})
	}
	output := filepath.Join(workDir, "frame.png")
	if _, err := service.runner.Run(ctx, workDir, "dcmj2pnm", "--write-png", inputPath, output); err != nil {
		return nil, err
	}
	frame, err := boundedRead(output)
	if err != nil {
		return nil, err
	}
	parts = append(parts, Part{Kind: "inline_data", Data: frame, MIMEType: "image/png", Name: "frame.png", Locator: &Locator{Frame: 1}})
	return parts, nil
}

func boundedRead(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > MaxDerivativeBytes {
		return nil, ErrOutputTooLarge
	}
	return os.ReadFile(path)
}

func whisperParts(path string) ([]Part, error) {
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
	parts := make([]Part, 0, len(payload.Transcription))
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
		parts = append(parts, Part{Kind: "text", Text: strings.TrimSpace(segment.Text), MIMEType: "text/plain", Locator: &Locator{StartMS: start, EndMS: end}})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("transcription contained no segments: %s", strconv.Itoa(len(payload.Transcription)))
	}
	return parts, nil
}
