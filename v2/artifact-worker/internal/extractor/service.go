package extractor

import (
	"context"
	"strings"
)

// ProfileExtractorPort isolates the application workflow from OCR,
// transcription, media conversion and operating-system command details.
type ProfileExtractorPort interface {
	Extract(context.Context, string, string, string) ([]Part, error)
}

type Service struct {
	profiles ProfileExtractorPort
}

func NewService(profiles ProfileExtractorPort) *Service {
	return &Service{profiles: profiles}
}

func (service *Service) Extract(ctx context.Context, workDir, inputPath string, metadata Metadata) ([]Part, error) {
	if service.profiles == nil ||
		strings.TrimSpace(workDir) == "" ||
		strings.TrimSpace(inputPath) == "" ||
		strings.TrimSpace(metadata.Profile) == "" {
		return nil, ErrInvalidRequest
	}
	parts, err := service.profiles.Extract(ctx, workDir, inputPath, metadata.Profile)
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
