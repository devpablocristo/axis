package artifacts

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

const (
	maxArchiveEntries          = 10_000
	maxArchiveUncompressed     = int64(1 << 30)
	maxArchiveCompressionRatio = int64(200)
)

type OfficeFormatAdapter struct{ Extractor ExtractionPort }

func (OfficeFormatAdapter) Name() string { return "office" }
func (OfficeFormatAdapter) Supports(mimeType, filename string) bool {
	mimeType = normalizeMIME(mimeType)
	if strings.Contains(mimeType, "officedocument") || strings.Contains(mimeType, "opendocument") {
		return true
	}
	switch mimeType {
	case "application/msword", "application/rtf", "text/rtf",
		"application/vnd.ms-excel", "application/vnd.ms-powerpoint":
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".doc", ".docx", ".odt", ".rtf", ".ppt", ".pptx", ".xls", ".xlsx", ".ods":
		return true
	}
	return false
}
func (adapter OfficeFormatAdapter) Adapt(ctx context.Context, input AdaptInput) ([]ContentPart, error) {
	if adapter.Extractor == nil {
		return nil, ErrExtractionUnavailable
	}
	if err := validateOfficeArchive(input); err != nil {
		return nil, err
	}
	return adapter.Extractor.Extract(ctx, ExtractRequest{
		Scope: input.Scope, Manifest: input.Manifest, Stored: input.Stored, Blob: input.Blob, Profile: "office",
	})
}

type ImageFormatAdapter struct{ Extractor ExtractionPort }

func (ImageFormatAdapter) Name() string { return "image" }
func (ImageFormatAdapter) Supports(mimeType, filename string) bool {
	if strings.HasPrefix(normalizeMIME(mimeType), "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".tif", ".tiff", ".heic", ".heif", ".gif", ".bmp":
		return true
	}
	return false
}
func (adapter ImageFormatAdapter) Adapt(ctx context.Context, input AdaptInput) ([]ContentPart, error) {
	mimeType := normalizeMIME(input.Manifest.MIMEType)
	if oneOfString(mimeType, "image/png", "image/jpeg", "image/webp", "image/heic", "image/heif") {
		return nativeWithOptionalDerivatives(ctx, input, adapter.Extractor, "image")
	}
	if adapter.Extractor == nil {
		return nil, ErrExtractionUnavailable
	}
	return adapter.Extractor.Extract(ctx, ExtractRequest{
		Scope: input.Scope, Manifest: input.Manifest, Stored: input.Stored, Blob: input.Blob, Profile: "image",
	})
}

type AudioFormatAdapter struct{ Extractor ExtractionPort }

func (AudioFormatAdapter) Name() string { return "audio" }
func (AudioFormatAdapter) Supports(mimeType, filename string) bool {
	mimeType = normalizeMIME(mimeType)
	if oneOfString(mimeType, "audio/wav", "audio/x-wav", "audio/mpeg", "audio/mp4", "audio/x-m4a", "audio/ogg", "audio/flac", "audio/x-flac") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".wav", ".mp3", ".m4a", ".ogg", ".flac":
		return true
	}
	return false
}
func (adapter AudioFormatAdapter) Adapt(ctx context.Context, input AdaptInput) ([]ContentPart, error) {
	return nativeWithOptionalDerivatives(ctx, input, adapter.Extractor, "audio")
}

type VideoFormatAdapter struct{ Extractor ExtractionPort }

func (VideoFormatAdapter) Name() string { return "video" }
func (VideoFormatAdapter) Supports(mimeType, filename string) bool {
	mimeType = normalizeMIME(mimeType)
	if oneOfString(mimeType, "video/mp4", "video/quicktime", "video/webm", "video/x-matroska") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".mov", ".webm", ".mkv":
		return true
	}
	return false
}
func (adapter VideoFormatAdapter) Adapt(ctx context.Context, input AdaptInput) ([]ContentPart, error) {
	if normalizeMIME(input.Manifest.MIMEType) != "video/x-matroska" && !strings.EqualFold(filepath.Ext(input.Manifest.Name), ".mkv") {
		return nativeWithOptionalDerivatives(ctx, input, adapter.Extractor, "video")
	}
	if adapter.Extractor == nil {
		return nil, ErrExtractionUnavailable
	}
	return adapter.Extractor.Extract(ctx, ExtractRequest{
		Scope: input.Scope, Manifest: input.Manifest, Stored: input.Stored, Blob: input.Blob, Profile: "video",
	})
}

type DICOMFormatAdapter struct{ Extractor ExtractionPort }

func (DICOMFormatAdapter) Name() string { return "dicom" }
func (DICOMFormatAdapter) Supports(mimeType, filename string) bool {
	mimeType = normalizeMIME(mimeType)
	return mimeType == "application/dicom" || mimeType == "application/dicom+json" || strings.EqualFold(filepath.Ext(filename), ".dcm")
}
func (adapter DICOMFormatAdapter) Adapt(ctx context.Context, input AdaptInput) ([]ContentPart, error) {
	if adapter.Extractor == nil {
		return nil, ErrExtractionUnavailable
	}
	return adapter.Extractor.Extract(ctx, ExtractRequest{
		Scope: input.Scope, Manifest: input.Manifest, Stored: input.Stored, Blob: input.Blob, Profile: "dicom",
	})
}

func stagedNativePart(input AdaptInput) ([]ContentPart, error) {
	if strings.TrimSpace(input.Stored.URI) == "" {
		return nil, errors.New("native media requires staged URI")
	}
	return []ContentPart{{
		Kind: PartFileData, URI: input.Stored.URI, MIMEType: input.Manifest.MIMEType, Name: input.Manifest.Name,
		SHA256: input.Manifest.SHA256, DocumentID: input.Manifest.DocumentID,
	}}, nil
}

func nativeWithOptionalDerivatives(ctx context.Context, input AdaptInput, extractor ExtractionPort, profile string) ([]ContentPart, error) {
	native, err := stagedNativePart(input)
	if err != nil || extractor == nil {
		return native, err
	}
	derivatives, extractionErr := extractor.Extract(ctx, ExtractRequest{
		Scope: input.Scope, Manifest: input.Manifest, Stored: input.Stored, Blob: input.Blob, Profile: profile,
	})
	if extractionErr != nil {
		// The verified original remains usable by the multimodal model. Optional
		// OCR, transcripts and keyframes must never erase that capability.
		return native, nil
	}
	return append(native, derivatives...), nil
}

func validateOfficeArchive(input AdaptInput) error {
	extension := strings.ToLower(filepath.Ext(input.Manifest.Name))
	if !oneOfString(extension, ".docx", ".pptx", ".xlsx", ".odt", ".ods") {
		return nil
	}
	reader, err := input.Blob.Open()
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	readerAt, ok := reader.(io.ReaderAt)
	if !ok {
		return errors.New("office archive spool is not seekable")
	}
	archive, err := zip.NewReader(readerAt, input.Blob.Size())
	if err != nil {
		return err
	}
	if len(archive.File) == 0 || len(archive.File) > maxArchiveEntries {
		return ErrArtifactTooLarge
	}
	var compressed, uncompressed int64
	for _, entry := range archive.File {
		compressed += int64(entry.CompressedSize64)
		uncompressed += int64(entry.UncompressedSize64)
		if uncompressed > maxArchiveUncompressed {
			return ErrArtifactTooLarge
		}
	}
	if compressed > 0 && uncompressed/compressed > maxArchiveCompressionRatio {
		return ErrArtifactTooLarge
	}
	return nil
}

func oneOfString(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
