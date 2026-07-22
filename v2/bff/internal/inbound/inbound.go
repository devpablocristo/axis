// Package inbound is the machine-authenticated product edge for durable assists.
package inbound

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
)

type Binding struct {
	TenantID       string
	VirployeeID    string
	ActorID        string
	ProductSurface string
}

type Handler struct {
	bindings           map[string]Binding
	companionBaseURL   string
	internalAuthSecret string
	client             *http.Client
}

func NewHandler(bindings map[string]Binding, companionBaseURL, internalAuthSecret string, client *http.Client) *Handler {
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &Handler{bindings: bindings, companionBaseURL: strings.TrimRight(strings.TrimSpace(companionBaseURL), "/"), internalAuthSecret: strings.TrimSpace(internalAuthSecret), client: client}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.POST("/v1/assist-runs", h.AssistRun)
	router.GET("/v1/assist-runs/:run_id", h.GetAssistRun)
	router.GET("/v1/assist-capabilities", h.AssistCapabilities)
}

type assistRunEnvelope struct {
	OwnerSystem          string          `json:"owner_system"`
	ProductSurface       string          `json:"product_surface"`
	AssistType           string          `json:"assist_type"`
	SubjectType          string          `json:"subject_type"`
	SubjectID            string          `json:"subject_id"`
	RepositoryGeneration string          `json:"repository_generation"`
	Input                json.RawMessage `json:"input"`
}

type assistRunResult struct {
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	StatusURL    string          `json:"status_url,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

type companionAssistResponse struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error_message"`
}

func (h *Handler) AssistRun(c *gin.Context) {
	binding, ok := h.authenticate(c)
	if !ok {
		return
	}
	var envelope assistRunEnvelope
	if err := c.ShouldBindJSON(&envelope); err != nil || len(envelope.Input) == 0 || !json.Valid(envelope.Input) {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "invalid assist-run request")
		return
	}
	if surface := strings.TrimSpace(envelope.ProductSurface); surface != "" && !strings.EqualFold(surface, binding.ProductSurface) {
		ginmw.WriteError(c, http.StatusForbidden, "forbidden", "product_surface does not match the api key")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		ginmw.WriteError(c, http.StatusBadRequest, "idempotency_required", "Idempotency-Key must identify a stable input manifest")
		return
	}

	run, err := h.submit(c, binding, envelope, idempotencyKey)
	if err != nil {
		var downstream *downstreamError
		if errors.As(err, &downstream) && downstream.Status == http.StatusTooManyRequests {
			if downstream.RetryAfter != "" {
				c.Header("Retry-After", downstream.RetryAfter)
			}
			ginmw.WriteError(c, http.StatusTooManyRequests, "quota_exceeded", "product quota exceeded")
			return
		}
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "assist service unavailable")
		return
	}
	result := productResult(run)
	if isTerminal(result.Status) {
		c.JSON(http.StatusOK, result)
		return
	}

	wait := preferredWait(c.GetHeader("Prefer"))
	if wait > 0 {
		deadline := time.NewTimer(wait)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer deadline.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-c.Request.Context().Done():
				return
			case <-deadline.C:
				result.StatusURL = statusURL(result.ID)
				c.JSON(http.StatusAccepted, result)
				return
			case <-ticker.C:
				polled, pollErr := h.get(c, binding, result.ID)
				if pollErr != nil {
					continue
				}
				result = productResult(polled)
				if isTerminal(result.Status) {
					c.JSON(http.StatusOK, result)
					return
				}
			}
		}
	}
	result.StatusURL = statusURL(result.ID)
	c.JSON(http.StatusAccepted, result)
}

func (h *Handler) GetAssistRun(c *gin.Context) {
	binding, ok := h.authenticate(c)
	if !ok {
		return
	}
	runID := strings.TrimSpace(c.Param("run_id"))
	if runID == "" {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "run id is required")
		return
	}
	run, err := h.get(c, binding, runID)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "assist service unavailable")
		return
	}
	result := productResult(run)
	if !isTerminal(result.Status) {
		result.StatusURL = statusURL(result.ID)
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AssistCapabilities(c *gin.Context) {
	if _, ok := h.authenticate(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"schema_version": "axis.assist_capabilities.v1",
		"states":         []string{"received", "staging", "extracting", "indexing", "answering", "completed", "failed"},
		"formats": gin.H{
			"native": []string{
				"text/*", "application/json", "application/xml", "application/pdf",
				"image/png", "image/jpeg", "image/webp", "image/heic", "image/heif",
				"audio/wav", "audio/mpeg", "audio/mp4", "audio/ogg", "audio/flac",
				"video/mp4", "video/quicktime", "video/webm",
			},
			"extractor": []string{
				"image/tiff", "image/gif", "image/bmp", "video/x-matroska", "application/dicom",
				"application/msword", "application/rtf", "application/vnd.ms-excel", "application/vnd.ms-powerpoint",
				"application/vnd.openxmlformats-officedocument.*", "application/vnd.oasis.opendocument.*",
			},
		},
		"limits": gin.H{"max_artifact_bytes": 250 << 20, "max_diagnosis_bytes": 500 << 20, "max_repository_bytes": 5 << 30},
	})
}

func (h *Handler) submit(c *gin.Context, binding Binding, envelope assistRunEnvelope, idempotencyKey string) (companionAssistResponse, error) {
	body, err := json.Marshal(map[string]any{
		"input_json": envelope.Input, "idempotency_key": idempotencyKey,
		"assist_type": envelope.AssistType, "product_surface": binding.ProductSurface,
		"subject_id": envelope.SubjectID, "repository_generation": envelope.RepositoryGeneration,
	})
	if err != nil {
		return companionAssistResponse{}, err
	}
	target := h.companionBaseURL + "/v1/virployees/" + url.PathEscape(binding.VirployeeID) + "/assist-runs"
	request, err := h.internalRequest(c, binding, http.MethodPost, target, body)
	if err != nil {
		return companionAssistResponse{}, err
	}
	return h.do(request)
}

func (h *Handler) get(c *gin.Context, binding Binding, runID string) (companionAssistResponse, error) {
	target := h.companionBaseURL + "/v1/virployees/" + url.PathEscape(binding.VirployeeID) + "/assist-runs/" + url.PathEscape(runID)
	request, err := h.internalRequest(c, binding, http.MethodGet, target, nil)
	if err != nil {
		return companionAssistResponse{}, err
	}
	return h.do(request)
}

func (h *Handler) internalRequest(c *gin.Context, binding Binding, method, target string, body []byte) (*http.Request, error) {
	request, err := http.NewRequestWithContext(c.Request.Context(), method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Axis-Internal-Token", h.internalAuthSecret)
	request.Header.Set("X-Tenant-ID", binding.TenantID)
	request.Header.Set("X-Actor-ID", binding.ActorID)
	request.Header.Set("X-Product-Surface", binding.ProductSurface)
	request.Header.Set("X-Axis-Forwarded-By", "bff-v2")
	return request, nil
}

func (h *Handler) do(request *http.Request) (companionAssistResponse, error) {
	response, err := h.client.Do(request)
	if err != nil {
		return companionAssistResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return companionAssistResponse{}, err
	}
	var decoded companionAssistResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return companionAssistResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decoded, &downstreamError{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	return decoded, nil
}

type downstreamError struct {
	Status     int
	RetryAfter string
}

func (e *downstreamError) Error() string { return fmt.Sprintf("companion assist status %d", e.Status) }

func productResult(run companionAssistResponse) assistRunResult {
	status := strings.ToLower(strings.TrimSpace(run.Status))
	switch status {
	case "done":
		status = "completed"
	case "running":
		status = "answering"
	case "":
		status = "received"
	}
	return assistRunResult{ID: run.ID, Status: status, Output: run.Output, ErrorMessage: run.Error}
}

func isTerminal(status string) bool { return status == "completed" || status == "failed" }
func statusURL(id string) string    { return "/v1/assist-runs/" + url.PathEscape(id) }

func preferredWait(header string) time.Duration {
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "wait") {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || seconds <= 0 {
			return 0
		}
		if seconds > 30 {
			seconds = 30
		}
		return time.Duration(seconds) * time.Second
	}
	return 0
}

func (h *Handler) authenticate(c *gin.Context) (Binding, bool) {
	binding, ok := h.lookup(requestAPIKey(c))
	if !ok {
		ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "invalid or missing product api key")
	}
	return binding, ok
}

func (h *Handler) lookup(key string) (Binding, bool) {
	if key == "" {
		return Binding{}, false
	}
	for candidate, binding := range h.bindings {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(key)) == 1 {
			return binding, true
		}
	}
	return Binding{}, false
}

func requestAPIKey(c *gin.Context) string {
	if key := strings.TrimSpace(c.GetHeader("X-API-Key")); key != "" {
		return key
	}
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if value, found := strings.CutPrefix(authorization, "Bearer "); found {
		return strings.TrimSpace(value)
	}
	return ""
}

func ParseBindings(raw string) map[string]Binding {
	out := map[string]Binding{}
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' })
	for _, entry := range fields {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, rest, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		parts := strings.Split(rest, "|")
		if len(parts) < 4 {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = Binding{TenantID: strings.TrimSpace(parts[0]), VirployeeID: strings.TrimSpace(parts[1]), ActorID: strings.TrimSpace(parts[2]), ProductSurface: strings.TrimSpace(parts[3])}
	}
	return out
}
