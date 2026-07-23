// Package inbound is the machine-authenticated product edge for durable assists.
package inbound

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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
	"github.com/google/uuid"
)

type Binding struct {
	ProductID      string
	VirployeeID    string
	ActorID        string
	ProductSurface string
	RoutingPoolID  string
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
	CapabilityKey        string          `json:"capability_key,omitempty"`
	SubjectType          string          `json:"subject_type"`
	SubjectID            string          `json:"subject_id"`
	CaseID               string          `json:"case_id,omitempty"`
	AssignmentID         string          `json:"assignment_id,omitempty"`
	RepositoryGeneration string          `json:"repository_generation"`
	Input                json.RawMessage `json:"input"`
}

type assistRunResult struct {
	ID                     string          `json:"id"`
	CaseID                 string          `json:"case_id,omitempty"`
	ResponsibleVirployeeID string          `json:"responsible_virployee_id,omitempty"`
	CapabilityKey          string          `json:"capability_key,omitempty"`
	CapabilityManifestHash string          `json:"capability_manifest_hash,omitempty"`
	Status                 string          `json:"status"`
	AnswerStatus           string          `json:"answer_status,omitempty"`
	Citations              json.RawMessage `json:"citations,omitempty"`
	StatusURL              string          `json:"status_url,omitempty"`
	Output                 json.RawMessage `json:"output,omitempty"`
	Orchestration          json.RawMessage `json:"orchestration,omitempty"`
	ErrorMessage           string          `json:"error_message,omitempty"`
}

type companionAssistResponse struct {
	ID                     string          `json:"id"`
	CaseID                 string          `json:"case_id"`
	ResponsibleVirployeeID string          `json:"responsible_virployee_id"`
	CapabilityKey          string          `json:"capability_key"`
	CapabilityManifestHash string          `json:"capability_manifest_hash"`
	Status                 string          `json:"status"`
	AnswerStatus           string          `json:"answer_status"`
	Citations              json.RawMessage `json:"citations"`
	Output                 json.RawMessage `json:"output"`
	Orchestration          json.RawMessage `json:"orchestration"`
	Error                  string          `json:"error_message"`
}

type routingResolution struct {
	Status     string             `json:"status"`
	Assignment *routingAssignment `json:"assignment,omitempty"`
}

type routingAssignment struct {
	ID          string `json:"id"`
	VirployeeID string `json:"virployee_id"`
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
	envelope.CapabilityKey = strings.ToLower(strings.TrimSpace(envelope.CapabilityKey))
	if envelope.CapabilityKey != "" {
		if subjectID, err := uuid.Parse(strings.TrimSpace(envelope.SubjectID)); err != nil || subjectID == uuid.Nil {
			ginmw.WriteError(c, http.StatusBadRequest, "invalid_subject", "subject_id must be a UUID for capability assists")
			return
		}
		if strings.TrimSpace(envelope.RepositoryGeneration) == "" {
			ginmw.WriteError(c, http.StatusBadRequest, "repository_generation_required", "repository_generation is required for capability assists")
			return
		}
	}
	if strings.TrimSpace(binding.RoutingPoolID) != "" && strings.TrimSpace(envelope.SubjectID) == "" {
		ginmw.WriteError(c, http.StatusBadRequest, "subject_required", "subject_id is required for routed assists")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		ginmw.WriteError(c, http.StatusBadRequest, "idempotency_required", "Idempotency-Key must identify a stable input manifest")
		return
	}

	run, resolvedBinding, err := h.submit(c, binding, envelope, idempotencyKey)
	if err != nil {
		var downstream *downstreamError
		if errors.As(err, &downstream) && downstream.Status == http.StatusTooManyRequests {
			if downstream.RetryAfter != "" {
				c.Header("Retry-After", downstream.RetryAfter)
			}
			ginmw.WriteError(c, http.StatusTooManyRequests, "quota_exceeded", "product quota exceeded")
			return
		}
		if errors.As(err, &downstream) && (downstream.Status == http.StatusConflict || downstream.Status == http.StatusServiceUnavailable) {
			ginmw.WriteError(c, downstream.Status, "routing_unavailable", "no stable Virployee assignment is currently available")
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
				result.StatusURL = h.statusURL(result.ID, binding, resolvedBinding)
				c.JSON(http.StatusAccepted, result)
				return
			case <-ticker.C:
				polled, pollErr := h.get(c, resolvedBinding, result.ID)
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
	result.StatusURL = h.statusURL(result.ID, binding, resolvedBinding)
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
	resolvedBinding := binding
	if token := strings.TrimSpace(c.Query("route")); token != "" {
		virployeeID, valid := h.verifyRouteToken(token, runID, binding)
		if !valid {
			ginmw.WriteError(c, http.StatusForbidden, "forbidden", "invalid assist route")
			return
		}
		resolvedBinding.VirployeeID = virployeeID
	}
	run, err := h.get(c, resolvedBinding, runID)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "assist service unavailable")
		return
	}
	result := productResult(run)
	if !isTerminal(result.Status) {
		result.StatusURL = h.statusURL(result.ID, binding, resolvedBinding)
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AssistCapabilities(c *gin.Context) {
	if _, ok := h.authenticate(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"schema_version": "axis.assist_capabilities.v1",
		"states": []string{
			"received", "staging", "extracting", "indexing", "planning", "consulting",
			"synthesizing", "answering", "completed", "failed", "needs_human",
		},
		"orchestration": gin.H{
			"schema_version":  "axis.orchestration_summary.v1",
			"modes":           []string{"disabled", "shadow", "active"},
			"max_specialists": 3,
			"max_depth":       1,
			"handoffs":        true,
			"human_review":    true,
		},
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
		"capability_invocation": gin.H{
			"catalog": "tenant", "side_effect_class": "read", "schema_source": "active_manifest",
		},
		"limits": gin.H{"max_artifact_bytes": 250 << 20, "max_diagnosis_bytes": 500 << 20, "max_repository_bytes": 5 << 30},
	})
}

func (h *Handler) submit(c *gin.Context, binding Binding, envelope assistRunEnvelope, idempotencyKey string) (companionAssistResponse, Binding, error) {
	resolvedBinding := binding
	assignmentID := strings.TrimSpace(envelope.AssignmentID)
	if strings.TrimSpace(binding.RoutingPoolID) != "" {
		resolution, err := h.resolveRouting(c, binding, envelope.SubjectID, envelope.CapabilityKey)
		if err != nil {
			return companionAssistResponse{}, Binding{}, err
		}
		if resolution.Status == "unavailable" {
			return companionAssistResponse{}, Binding{}, &downstreamError{Status: http.StatusServiceUnavailable}
		}
		if resolution.Status == "reassignment_required" {
			return companionAssistResponse{}, Binding{}, &downstreamError{Status: http.StatusConflict}
		}
		if resolution.Status != "assigned" || resolution.Assignment == nil || strings.TrimSpace(resolution.Assignment.ID) == "" || strings.TrimSpace(resolution.Assignment.VirployeeID) == "" {
			return companionAssistResponse{}, Binding{}, errors.New("routing response has no stable assignment")
		}
		if assignmentID != "" && assignmentID != resolution.Assignment.ID {
			return companionAssistResponse{}, Binding{}, &downstreamError{Status: http.StatusConflict}
		}
		assignmentID = resolution.Assignment.ID
		resolvedBinding.VirployeeID = resolution.Assignment.VirployeeID
	}
	body, err := json.Marshal(map[string]any{
		"input_json": envelope.Input, "idempotency_key": idempotencyKey,
		"assist_type": envelope.AssistType, "product_surface": binding.ProductSurface,
		"capability_key": envelope.CapabilityKey,
		"subject_id":     envelope.SubjectID, "repository_generation": envelope.RepositoryGeneration,
		"case_id": envelope.CaseID, "assignment_id": assignmentID,
	})
	if err != nil {
		return companionAssistResponse{}, Binding{}, err
	}
	target := h.companionBaseURL + "/v1/virployees/" + url.PathEscape(resolvedBinding.VirployeeID) + "/assist-runs"
	request, err := h.internalRequest(c, binding, http.MethodPost, target, body)
	if err != nil {
		return companionAssistResponse{}, Binding{}, err
	}
	run, err := h.do(request)
	return run, resolvedBinding, err
}

func (h *Handler) resolveRouting(c *gin.Context, binding Binding, subjectID, capabilityKey string) (routingResolution, error) {
	body, err := json.Marshal(map[string]string{
		"pool_id": binding.RoutingPoolID, "subject_id": strings.TrimSpace(subjectID),
		"capability_key": strings.TrimSpace(capabilityKey),
	})
	if err != nil {
		return routingResolution{}, err
	}
	target := h.companionBaseURL + "/v1/virployee-routing:resolve"
	request, err := h.internalRequest(c, binding, http.MethodPost, target, body)
	if err != nil {
		return routingResolution{}, err
	}
	response, err := h.client.Do(request)
	if err != nil {
		return routingResolution{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return routingResolution{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return routingResolution{}, &downstreamError{Status: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	var resolution routingResolution
	if err := json.Unmarshal(raw, &resolution); err != nil {
		return routingResolution{}, err
	}
	return resolution, nil
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
	request.Header.Set("X-Product-ID", binding.ProductID)
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
	return assistRunResult{
		ID: run.ID, CaseID: run.CaseID, ResponsibleVirployeeID: run.ResponsibleVirployeeID,
		CapabilityKey: run.CapabilityKey, CapabilityManifestHash: run.CapabilityManifestHash,
		Status: status, AnswerStatus: run.AnswerStatus, Citations: run.Citations,
		Output: run.Output, Orchestration: run.Orchestration, ErrorMessage: run.Error,
	}
}

func isTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "needs_human"
}
func statusURL(id string) string { return "/v1/assist-runs/" + url.PathEscape(id) }

func (h *Handler) statusURL(runID string, original, resolved Binding) string {
	base := statusURL(runID)
	if strings.TrimSpace(resolved.VirployeeID) == "" || resolved.VirployeeID == original.VirployeeID {
		return base
	}
	token := h.routeToken(runID, resolved.VirployeeID, original)
	return base + "?route=" + url.QueryEscape(token)
}

func (h *Handler) routeToken(runID, virployeeID string, binding Binding) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(virployeeID)))
	mac := hmac.New(sha256.New, []byte(h.internalAuthSecret))
	_, _ = mac.Write([]byte(strings.Join([]string{binding.ProductID, binding.ProductSurface, runID, virployeeID}, "\x00")))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyRouteToken(token, runID string, binding Binding) (string, bool) {
	payload, signature, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payload == "" || signature == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || strings.TrimSpace(string(decoded)) == "" {
		return "", false
	}
	virployeeID := strings.TrimSpace(string(decoded))
	expected := h.routeToken(runID, virployeeID, binding)
	return virployeeID, subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

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
		binding := Binding{ProductID: strings.TrimSpace(parts[0]), VirployeeID: strings.TrimSpace(parts[1]), ActorID: strings.TrimSpace(parts[2]), ProductSurface: strings.TrimSpace(parts[3])}
		if len(parts) >= 5 {
			binding.RoutingPoolID = strings.TrimSpace(parts[4])
		}
		out[key] = binding
	}
	return out
}
