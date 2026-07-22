package artifacts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

type TextFormatAdapter struct{}

func (TextFormatAdapter) Name() string { return "text" }

func (TextFormatAdapter) Supports(mimeType, filename string) bool {
	mimeType = normalizeMIME(mimeType)
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/xhtml+xml":
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".txt", ".csv", ".json", ".xml", ".md", ".markdown", ".html", ".htm":
		return true
	}
	return false
}

func (TextFormatAdapter) Adapt(_ context.Context, input AdaptInput) ([]ContentPart, error) {
	r, err := input.Blob.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, ErrEmptyDerivative
	}
	return []ContentPart{{
		Kind: PartText, Text: string(data), MIMEType: input.Manifest.MIMEType, Name: input.Manifest.Name,
		SHA256: input.Manifest.SHA256, DocumentID: input.Manifest.DocumentID,
	}}, nil
}

type PDFFormatAdapter struct{ Extractor ExtractionPort }

func (PDFFormatAdapter) Name() string { return "pdf" }
func (PDFFormatAdapter) Supports(mimeType, filename string) bool {
	return normalizeMIME(mimeType) == "application/pdf" || strings.EqualFold(filepath.Ext(filename), ".pdf")
}

func (adapter PDFFormatAdapter) Adapt(ctx context.Context, input AdaptInput) ([]ContentPart, error) {
	r, err := input.Blob.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	parts := []ContentPart{{
		Kind: PartFileData, URI: input.Stored.URI, MIMEType: "application/pdf", Name: input.Manifest.Name,
		SHA256: input.Manifest.SHA256, DocumentID: input.Manifest.DocumentID,
	}}
	var readerAt io.ReaderAt
	if seekable, ok := r.(io.ReaderAt); ok {
		readerAt = seekable
	} else {
		data, readErr := io.ReadAll(r)
		if readErr != nil {
			return nil, readErr
		}
		readerAt = bytes.NewReader(data)
	}
	reader, err := pdf.NewReader(readerAt, input.Blob.Size())
	hasText := false
	if err == nil {
		plain, plainErr := reader.GetPlainText()
		if plainErr == nil {
			extracted, readErr := io.ReadAll(plain)
			if readErr == nil && strings.TrimSpace(string(extracted)) != "" {
				hasText = true
				parts = append([]ContentPart{{
					Kind: PartText, Text: string(extracted), MIMEType: "text/plain", Name: input.Manifest.Name,
					SHA256: input.Manifest.SHA256, DocumentID: input.Manifest.DocumentID,
				}}, parts...)
			}
		}
	}
	if input.Stored.URI == "" {
		return nil, fmt.Errorf("PDF native part requires staged URI")
	}
	if !hasText && adapter.Extractor != nil {
		derived, extractErr := adapter.Extractor.Extract(ctx, ExtractRequest{
			Scope: input.Scope, Manifest: input.Manifest, Stored: input.Stored, Blob: input.Blob, Profile: "ocr_pdf",
		})
		if extractErr == nil && usableParts(derived) {
			parts = append(derived, parts...)
		}
	}
	return parts, nil
}

type NativeMediaAdapter struct{}

func (NativeMediaAdapter) Name() string { return "native_media" }
func (NativeMediaAdapter) Supports(mimeType, _ string) bool {
	mimeType = normalizeMIME(mimeType)
	return strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "video/")
}

func (NativeMediaAdapter) Adapt(_ context.Context, input AdaptInput) ([]ContentPart, error) {
	if input.Stored.URI == "" {
		return nil, errors.New("native media requires staged URI")
	}
	return []ContentPart{{
		Kind: PartFileData, URI: input.Stored.URI, MIMEType: input.Manifest.MIMEType, Name: input.Manifest.Name,
		SHA256: input.Manifest.SHA256, DocumentID: input.Manifest.DocumentID,
	}}, nil
}
