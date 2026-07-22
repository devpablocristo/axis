package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
)

const (
	maxMetadataBytes  = 1 << 20
	multipartOverhead = 1 << 20
)

var safeExtension = regexp.MustCompile(`^\.[A-Za-z0-9]{1,8}$`)

type Handler struct {
	service *extractor.Service
	token   string
	slots   chan struct{}
}

func NewHandler(service *extractor.Service, token string) *Handler {
	return NewHandlerWithConcurrency(service, token, 2)

}

func NewHandlerWithConcurrency(service *extractor.Service, token string, concurrency int) *Handler {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Handler{service: service, token: strings.TrimSpace(token), slots: make(chan struct{}, concurrency)}
}

func (handler *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /v1/extract", handler.extract)
	return mux
}

func (handler *Handler) extract(writer http.ResponseWriter, request *http.Request) {
	if !handler.authorized(request.Header.Get("X-Axis-Internal-Token")) {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}
	select {
	case handler.slots <- struct{}{}:
		defer func() { <-handler.slots }()
	case <-request.Context().Done():
		writeError(writer, http.StatusRequestTimeout, "request_cancelled")
		return
	}
	workDir, err := os.MkdirTemp("", "axis-artifact-extract-")
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "worker_unavailable")
		return
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	metadata, inputPath, err := readMultipart(writer, request, workDir)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_artifact")
		return
	}
	parts, err := handler.service.Extract(request.Context(), workDir, inputPath, metadata)
	if err != nil {
		status := http.StatusUnprocessableEntity
		code := "extraction_failed"
		if errors.Is(err, extractor.ErrUnavailable) {
			status, code = http.StatusServiceUnavailable, "extractor_unavailable"
		} else if errors.Is(err, extractor.ErrUnsupported) {
			code = "unsupported_profile"
		} else if errors.Is(err, extractor.ErrOutputTooLarge) {
			code = "derivative_too_large"
		}
		writeError(writer, status, code)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(extractor.Response{Parts: parts})
}

func (handler *Handler) authorized(value string) bool {
	if handler.token == "" || len(value) != len(handler.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(handler.token)) == 1
}

func readMultipart(writer http.ResponseWriter, request *http.Request, workDir string) (extractor.Metadata, string, error) {
	request.Body = http.MaxBytesReader(writer, request.Body, extractor.MaxArtifactBytes+maxMetadataBytes+multipartOverhead)
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return extractor.Metadata{}, "", extractor.ErrInvalidRequest
	}
	reader := multipart.NewReader(request.Body, params["boundary"])
	var metadata extractor.Metadata
	inputPath := filepath.Join(workDir, "artifact.bin")
	var gotMetadata, gotArtifact bool
	hash := sha256.New()
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return extractor.Metadata{}, "", nextErr
		}
		switch part.FormName() {
		case "metadata":
			raw, readErr := io.ReadAll(io.LimitReader(part, maxMetadataBytes+1))
			if readErr != nil || len(raw) > maxMetadataBytes || json.Unmarshal(raw, &metadata) != nil {
				_ = part.Close()
				return extractor.Metadata{}, "", extractor.ErrInvalidRequest
			}
			gotMetadata = true
		case "artifact":
			file, createErr := os.OpenFile(inputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if createErr != nil {
				_ = part.Close()
				return extractor.Metadata{}, "", createErr
			}
			written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(part, extractor.MaxArtifactBytes+1))
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil || written <= 0 || written > extractor.MaxArtifactBytes {
				_ = part.Close()
				return extractor.Metadata{}, "", extractor.ErrInvalidRequest
			}
			gotArtifact = true
		}
		_ = part.Close()
	}
	if !gotMetadata || !gotArtifact || strings.TrimSpace(metadata.Profile) == "" ||
		strings.TrimSpace(metadata.Scope.TenantID) == "" || strings.TrimSpace(metadata.Manifest.DocumentID) == "" {
		return extractor.Metadata{}, "", extractor.ErrInvalidRequest
	}
	if metadata.Manifest.SizeBytes > 0 {
		info, statErr := os.Stat(inputPath)
		if statErr != nil || info.Size() != metadata.Manifest.SizeBytes {
			return extractor.Metadata{}, "", extractor.ErrInvalidRequest
		}
	}
	if expected := strings.ToLower(strings.TrimSpace(metadata.Manifest.SHA256)); expected != "" && expected != hex.EncodeToString(hash.Sum(nil)) {
		return extractor.Metadata{}, "", extractor.ErrInvalidRequest
	}
	extension := filepath.Ext(metadata.Manifest.Name)
	if safeExtension.MatchString(extension) {
		renamed := filepath.Join(workDir, "input"+strings.ToLower(extension))
		if err := os.Rename(inputPath, renamed); err != nil {
			return extractor.Metadata{}, "", err
		}
		inputPath = renamed
	}
	return metadata, inputPath, nil
}

func writeError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"code": code}})
}
