// Package inbound is the machine-authenticated product edge for durable assists.
package inbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/bff-v2/internal/productedge"
	ginmw "github.com/devpablocristo/platform/http/gin/go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Binding struct {
	ProductID           string
	OrgID               string
	VirployeeID         string
	ActorID             string
	PrincipalType       string
	ProductSurface      string
	RoutingPoolID       string
	IntegrationID       string
	IntegrationVersion  int64
	IntegrationHash     string
	Scopes              []string
	AllowedVirployeeIDs []string
	AllowedPoolIDs      []string
	AllowedCapabilities []productedge.CapabilityRef
	AllowedEvents       []productedge.EventContract
	MaxRequestBytes     int64
}

type APIKeyAuthenticator = productedge.ProductAuthenticator

type Handler struct {
	bindings            map[string]Binding
	authenticator       APIKeyAuthenticator
	ports               productedge.Ports
	allowLegacyBindings bool
	routeSigningSecret  string
}

type HandlerOptions struct {
	// AllowLegacyBindings enables pre-database BFF_V2_PRODUCT_API_KEYS entries.
	// Callers must only set it in an explicitly selected development/test mode.
	AllowLegacyBindings bool
	RouteSigningSecret  string
}

// NewHandlerWithPorts is the composition entrypoint for the product edge.
// Application handlers depend only on functional ports; transport adapters are
// selected by wire.
func NewHandlerWithPorts(
	authenticator APIKeyAuthenticator,
	legacyBindings map[string]Binding,
	ports productedge.Ports,
	options HandlerOptions,
) *Handler {
	bindings := map[string]Binding{}
	if options.AllowLegacyBindings {
		for key, binding := range legacyBindings {
			if len(binding.Scopes) == 0 {
				binding.Scopes = []string{"assist.read", "assist.write"}
			}
			if strings.TrimSpace(binding.PrincipalType) == "" {
				binding.PrincipalType = "service"
			}
			bindings[key] = binding
		}
	}
	return &Handler{
		bindings:            bindings,
		authenticator:       authenticator,
		ports:               ports,
		allowLegacyBindings: options.AllowLegacyBindings,
		routeSigningSecret:  strings.TrimSpace(options.RouteSigningSecret),
	}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.POST("/v1/assist-runs", h.AssistRun)
	router.GET("/v1/assist-runs/:run_id", h.GetAssistRun)
	router.GET("/v1/assist-capabilities", h.AssistCapabilities)
	router.POST("/v1/product-events", h.ProductEvent)
}

type productEventEnvelope struct {
	EventID     string          `json:"event_id"`
	EventType   string          `json:"event_type"`
	Version     string          `json:"version"`
	VirployeeID string          `json:"virployee_id,omitempty"`
	SubjectID   string          `json:"subject_id,omitempty"`
	CaseID      string          `json:"case_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

func (h *Handler) ProductEvent(c *gin.Context) {
	binding, ok := h.authenticate(c)
	if !ok {
		return
	}
	if !containsString(binding.Scopes, "events.write") {
		ginmw.WriteError(c, http.StatusForbidden, "scope_required", "events.write scope is required")
		return
	}
	var envelope productEventEnvelope
	if err := c.ShouldBindJSON(&envelope); err != nil || len(envelope.Payload) == 0 || !json.Valid(envelope.Payload) {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_event", "product event payload is invalid")
		return
	}
	if id, err := uuid.Parse(strings.TrimSpace(envelope.EventID)); err != nil || id == uuid.Nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_event", "event_id must be a UUID")
		return
	}
	envelope.EventType = strings.ToLower(strings.TrimSpace(envelope.EventType))
	envelope.Version = strings.ToLower(strings.TrimSpace(envelope.Version))
	var contract *productedge.EventContract
	for i := range binding.AllowedEvents {
		candidate := &binding.AllowedEvents[i]
		if candidate.Type == envelope.EventType && candidate.Version == envelope.Version {
			contract = candidate
			break
		}
	}
	if contract == nil {
		ginmw.WriteError(c, http.StatusForbidden, "event_not_authorized", "event is not authorized by the active product contract")
		return
	}
	if !matchesEventSchema(contract.Schema, envelope.Payload) {
		ginmw.WriteError(c, http.StatusBadRequest, "event_schema_mismatch", "product event payload does not match the active contract")
		return
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_event", "product event payload is invalid")
		return
	}
	if binding.MaxRequestBytes > 0 && int64(len(body)) > binding.MaxRequestBytes {
		ginmw.WriteError(c, http.StatusRequestEntityTooLarge, "event_too_large", "product event exceeds the active contract limit")
		return
	}
	if h.ports.PublishProductEvent == nil {
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "event service unavailable")
		return
	}
	response, err := h.ports.PublishProductEvent.PublishProductEvent(
		c.Request.Context(),
		invocationContext(binding),
		productedge.ProductEvent{
			EventID:     envelope.EventID,
			EventType:   envelope.EventType,
			Version:     envelope.Version,
			VirployeeID: strings.TrimSpace(envelope.VirployeeID),
			Payload:     envelope.Payload,
		},
	)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "event service unavailable")
		return
	}
	if response.RetryAfter != "" {
		c.Header("Retry-After", response.RetryAfter)
	}
	contentType := response.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(response.StatusCode, contentType, response.Body)
}

type assistRunEnvelope struct {
	OwnerSystem          string          `json:"owner_system"`
	ProductSurface       string          `json:"product_surface"`
	AssistType           string          `json:"assist_type"`
	CapabilityID         string          `json:"capability_id,omitempty"`
	CapabilityKey        string          `json:"capability_key,omitempty"`
	SubjectType          string          `json:"subject_type"`
	SubjectID            string          `json:"subject_id"`
	CaseID               string          `json:"case_id,omitempty"`
	AssignmentID         string          `json:"assignment_id,omitempty"`
	VirployeeID          string          `json:"virployee_id,omitempty"`
	RoutingPoolID        string          `json:"routing_pool_id,omitempty"`
	RepositoryGeneration string          `json:"repository_generation"`
	Input                json.RawMessage `json:"input"`
}

type assistRunResult struct {
	ID                     string          `json:"id"`
	CaseID                 string          `json:"case_id,omitempty"`
	ResponsibleVirployeeID string          `json:"responsible_virployee_id,omitempty"`
	CapabilityID           string          `json:"capability_id,omitempty"`
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

func (h *Handler) AssistRun(c *gin.Context) {
	binding, ok := h.authenticate(c)
	if !ok {
		return
	}
	if !containsString(binding.Scopes, "assist.write") {
		ginmw.WriteError(c, http.StatusForbidden, "scope_required", "assist.write scope is required")
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
	envelope.CapabilityID = strings.TrimSpace(envelope.CapabilityID)
	envelope.CapabilityKey = strings.ToLower(strings.TrimSpace(envelope.CapabilityKey))
	if envelope.CapabilityID != "" {
		if id, err := uuid.Parse(envelope.CapabilityID); err != nil || id == uuid.Nil {
			ginmw.WriteError(c, http.StatusBadRequest, "invalid_capability", "capability_id must be a UUID")
			return
		}
	}
	if !h.resolveEntrypoint(c, &binding, &envelope) {
		return
	}
	if envelope.CapabilityID != "" || envelope.CapabilityKey != "" {
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
		var downstream *productedge.DownstreamError
		if errors.As(err, &downstream) && downstream.StatusCode == http.StatusTooManyRequests {
			if downstream.RetryAfter != "" {
				c.Header("Retry-After", downstream.RetryAfter)
			}
			ginmw.WriteError(c, http.StatusTooManyRequests, "quota_exceeded", "product quota exceeded")
			return
		}
		if errors.As(err, &downstream) && (downstream.StatusCode == http.StatusConflict || downstream.StatusCode == http.StatusServiceUnavailable) {
			ginmw.WriteError(c, downstream.StatusCode, "routing_unavailable", "no stable Virployee assignment is currently available")
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
	binding, ok := h.authenticate(c)
	if !ok {
		return
	}
	if h.ports.AssistCapabilities != nil {
		out, err := h.ports.AssistCapabilities.AssistCapabilities(c.Request.Context(), invocationContext(binding))
		if err != nil {
			ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "assist capability service unavailable")
			return
		}
		c.JSON(http.StatusOK, out)
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
		"limits": gin.H{"max_artifact_bytes": 250 << 20, "max_assist_bytes": 500 << 20, "max_repository_bytes": 5 << 30},
	})
}

func (h *Handler) submit(c *gin.Context, binding Binding, envelope assistRunEnvelope, idempotencyKey string) (productedge.AssistRun, Binding, error) {
	resolvedBinding := binding
	assignmentID := strings.TrimSpace(envelope.AssignmentID)
	if strings.TrimSpace(binding.RoutingPoolID) != "" {
		resolution, err := h.resolveRouting(c, binding, envelope.SubjectID, envelope.CapabilityID, envelope.CapabilityKey)
		if err != nil {
			return productedge.AssistRun{}, Binding{}, err
		}
		if resolution.Status == "unavailable" {
			return productedge.AssistRun{}, Binding{}, &productedge.DownstreamError{StatusCode: http.StatusServiceUnavailable}
		}
		if resolution.Status == "reassignment_required" {
			return productedge.AssistRun{}, Binding{}, &productedge.DownstreamError{StatusCode: http.StatusConflict}
		}
		if resolution.Status != "assigned" || resolution.Assignment == nil || strings.TrimSpace(resolution.Assignment.ID) == "" || strings.TrimSpace(resolution.Assignment.VirployeeID) == "" {
			return productedge.AssistRun{}, Binding{}, errors.New("routing response has no stable assignment")
		}
		if assignmentID != "" && assignmentID != resolution.Assignment.ID {
			return productedge.AssistRun{}, Binding{}, &productedge.DownstreamError{StatusCode: http.StatusConflict}
		}
		assignmentID = resolution.Assignment.ID
		resolvedBinding.VirployeeID = resolution.Assignment.VirployeeID
	}
	if h.ports.StartAssist == nil {
		return productedge.AssistRun{}, Binding{}, errors.New("start-assist port is unavailable")
	}
	run, err := h.ports.StartAssist.StartAssist(
		c.Request.Context(),
		invocationContext(binding),
		productedge.AssistInput{
			VirployeeID:          resolvedBinding.VirployeeID,
			Input:                envelope.Input,
			IdempotencyKey:       idempotencyKey,
			AssistType:           envelope.AssistType,
			CapabilityID:         envelope.CapabilityID,
			CapabilityKey:        envelope.CapabilityKey,
			SubjectID:            envelope.SubjectID,
			RepositoryGeneration: envelope.RepositoryGeneration,
			CaseID:               envelope.CaseID,
			AssignmentID:         assignmentID,
		},
	)
	return run, resolvedBinding, err
}

func (h *Handler) resolveRouting(c *gin.Context, binding Binding, subjectID, capabilityID, capabilityKey string) (productedge.RoutingResolution, error) {
	if h.ports.ResolveRouting == nil {
		return productedge.RoutingResolution{}, errors.New("resolve-routing port is unavailable")
	}
	return h.ports.ResolveRouting.ResolveRouting(
		c.Request.Context(),
		invocationContext(binding),
		productedge.RoutingInput{
			PoolID: binding.RoutingPoolID, SubjectID: subjectID,
			CapabilityID: capabilityID, CapabilityKey: capabilityKey,
		},
	)
}

func (h *Handler) get(c *gin.Context, binding Binding, runID string) (productedge.AssistRun, error) {
	if h.ports.GetAssistRun == nil {
		return productedge.AssistRun{}, errors.New("get-assist-run port is unavailable")
	}
	return h.ports.GetAssistRun.GetAssistRun(
		c.Request.Context(),
		invocationContext(binding),
		binding.VirployeeID,
		runID,
	)
}

func productResult(run productedge.AssistRun) assistRunResult {
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
		CapabilityID: run.CapabilityID, CapabilityKey: run.CapabilityKey,
		CapabilityManifestHash: run.CapabilityManifestHash,
		Status:                 status, AnswerStatus: run.AnswerStatus, Citations: run.Citations,
		Output: run.Output, Orchestration: run.Orchestration, ErrorMessage: run.Error,
	}
}

func isTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "needs_human"
}
func statusURL(id string) string { return "/v1/assist-runs/" + url.PathEscape(id) }

func (h *Handler) statusURL(runID string, original, resolved Binding) string {
	base := statusURL(runID)
	if strings.TrimSpace(resolved.VirployeeID) == "" ||
		(resolved.VirployeeID == original.VirployeeID && original.IntegrationID == "") {
		return base
	}
	token := h.routeToken(runID, resolved.VirployeeID, original)
	return base + "?route=" + url.QueryEscape(token)
}

func (h *Handler) routeToken(runID, virployeeID string, binding Binding) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(virployeeID)))
	mac := hmac.New(sha256.New, []byte(h.routeSigningSecret))
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
	key := requestAPIKey(c)
	if h.authenticator != nil && key != "" {
		resolved, err := h.authenticator.AuthenticateAPIKey(c.Request.Context(), key)
		if err == nil {
			return bindingFromMachine(resolved), true
		}
		// Persisted credentials have a reserved prefix. They are authoritative:
		// a revoked/unknown key or a repository error must never fall through to
		// a process-local credential with the same value.
		if strings.HasPrefix(key, "axis_pk_") || !h.allowLegacyBindings {
			ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "invalid or missing product api key")
			return Binding{}, false
		}
	}
	binding, ok := h.lookup(key)
	if !ok {
		ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "invalid or missing product api key")
	}
	return binding, ok
}

func bindingFromMachine(in productedge.MachineBinding) Binding {
	return Binding{
		ProductID: in.Context.ProductID, OrgID: in.Context.OrgID, ActorID: in.Context.PrincipalID,
		PrincipalType:  in.Context.PrincipalType,
		ProductSurface: in.Context.ProductSurface, IntegrationID: in.Context.IntegrationID,
		IntegrationVersion: in.Context.IntegrationRevision, IntegrationHash: in.Context.IntegrationHash,
		Scopes: in.Context.Scopes, VirployeeID: in.VirployeeID, RoutingPoolID: in.RoutingPoolID,
		AllowedVirployeeIDs: in.AllowedVirployeeIDs,
		AllowedPoolIDs:      in.AllowedPoolIDs, AllowedCapabilities: in.AllowedCapabilities,
		AllowedEvents: in.AllowedEvents, MaxRequestBytes: in.MaxRequestBytes,
	}
}

func invocationContext(binding Binding) productedge.InvocationContext {
	return productedge.InvocationContext{
		OrgID: binding.OrgID, ProductID: binding.ProductID, ProductSurface: binding.ProductSurface,
		IntegrationID: binding.IntegrationID, IntegrationRevision: binding.IntegrationVersion,
		IntegrationHash: binding.IntegrationHash, PrincipalID: binding.ActorID,
		PrincipalType: binding.PrincipalType,
		Scopes:        append([]string(nil), binding.Scopes...), AccessMode: productedge.AccessModeDirect,
	}
}

func (h *Handler) resolveEntrypoint(c *gin.Context, binding *Binding, envelope *assistRunEnvelope) bool {
	requestedVirployee := strings.TrimSpace(envelope.VirployeeID)
	requestedPool := strings.TrimSpace(envelope.RoutingPoolID)
	if requestedVirployee != "" && requestedPool != "" {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_entrypoint", "choose a virployee or routing pool, not both")
		return false
	}
	if len(binding.AllowedCapabilities) > 0 && (envelope.CapabilityID != "" || envelope.CapabilityKey != "") {
		allowed := false
		for _, ref := range binding.AllowedCapabilities {
			idMatches := envelope.CapabilityID == "" || ref.ID == envelope.CapabilityID
			keyMatches := envelope.CapabilityKey == "" || ref.Key == envelope.CapabilityKey
			if idMatches && keyMatches {
				if envelope.CapabilityID == "" {
					envelope.CapabilityID = ref.ID
				}
				if envelope.CapabilityKey == "" {
					envelope.CapabilityKey = ref.Key
				}
				allowed = true
				break
			}
		}
		if !allowed {
			ginmw.WriteError(c, http.StatusForbidden, "capability_not_authorized", "capability is not authorized by the active product contract")
			return false
		}
	}
	if requestedVirployee != "" {
		if !containsString(binding.AllowedVirployeeIDs, requestedVirployee) {
			ginmw.WriteError(c, http.StatusForbidden, "entrypoint_not_authorized", "virployee is not authorized by the active product contract")
			return false
		}
		binding.VirployeeID, binding.RoutingPoolID = requestedVirployee, ""
		return true
	}
	if requestedPool != "" {
		if !containsString(binding.AllowedPoolIDs, requestedPool) {
			ginmw.WriteError(c, http.StatusForbidden, "entrypoint_not_authorized", "routing pool is not authorized by the active product contract")
			return false
		}
		binding.RoutingPoolID, binding.VirployeeID = requestedPool, ""
		return true
	}
	if binding.VirployeeID != "" || binding.RoutingPoolID != "" {
		return true
	}
	if len(binding.AllowedPoolIDs) == 1 {
		binding.RoutingPoolID = binding.AllowedPoolIDs[0]
		return true
	}
	if len(binding.AllowedVirployeeIDs) == 1 {
		binding.VirployeeID = binding.AllowedVirployeeIDs[0]
		return true
	}
	ginmw.WriteError(c, http.StatusBadRequest, "entrypoint_required", "virployee_id or routing_pool_id is required")
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
