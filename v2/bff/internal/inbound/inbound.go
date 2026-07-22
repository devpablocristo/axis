// Package inbound is the product-facing edge: a machine (a product like medmory,
// not a Clerk user) calls POST /v1/assist-runs with an API key. The key maps to a
// tenant + virployee + service-principal actor; the request is proxied to
// companion's assist endpoint and the response is mapped back to the product's
// assist-runs contract. This lives OUTSIDE the human-session middleware.
package inbound

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	ginmw "github.com/devpablocristo/platform/http/gin/go"
)

// Binding is what an API key resolves to: the tenant the product writes under, the
// virployee that does the work, the service-principal actor, and the product.
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
	return &Handler{
		bindings:           bindings,
		companionBaseURL:   strings.TrimRight(strings.TrimSpace(companionBaseURL), "/"),
		internalAuthSecret: strings.TrimSpace(internalAuthSecret),
		client:             client,
	}
}

func (h *Handler) Routes(router gin.IRouter) {
	router.POST("/v1/assist-runs", h.AssistRun)
}

// assistRunEnvelope is the product's request shape. Only `input` is forwarded to
// the virployee (as input_json); the rest identifies/routes the request.
type assistRunEnvelope struct {
	OwnerSystem    string          `json:"owner_system"`
	ProductSurface string          `json:"product_surface"`
	AssistType     string          `json:"assist_type"`
	SubjectType    string          `json:"subject_type"`
	SubjectID      string          `json:"subject_id"`
	Input          json.RawMessage `json:"input"`
}

// assistRunResult is the product-facing response contract.
type assistRunResult struct {
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	Output       json.RawMessage `json:"output,omitempty"`
	ErrorMessage string          `json:"error_message"`
}

// companionAssistResponse is companion's generic assist result.
type companionAssistResponse struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error_message"`
}

func (h *Handler) AssistRun(c *gin.Context) {
	key := requestAPIKey(c)
	binding, ok := h.lookup(key)
	if !ok {
		ginmw.WriteError(c, http.StatusUnauthorized, "unauthorized", "invalid or missing product api key")
		return
	}

	var env assistRunEnvelope
	if err := c.ShouldBindJSON(&env); err != nil {
		ginmw.WriteError(c, http.StatusBadRequest, "invalid_request", "invalid assist-run request")
		return
	}
	// Defense in depth: a key issued for one product cannot drive another.
	if ps := strings.TrimSpace(env.ProductSurface); ps != "" && !strings.EqualFold(ps, binding.ProductSurface) {
		ginmw.WriteError(c, http.StatusForbidden, "forbidden", "product_surface does not match the api key")
		return
	}

	compBody := map[string]any{"input_json": env.Input}
	if idem := strings.TrimSpace(c.GetHeader("Idempotency-Key")); idem != "" {
		compBody["idempotency_key"] = idem
	}
	raw, err := json.Marshal(compBody)
	if err != nil {
		ginmw.WriteError(c, http.StatusInternalServerError, "internal", "could not encode request")
		return
	}

	target := h.companionBaseURL + "/v1/virployees/" + url.PathEscape(binding.VirployeeID) + "/assist"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "bad_gateway", "downstream request failed")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Axis-Internal-Token", h.internalAuthSecret)
	req.Header.Set("X-Tenant-ID", binding.TenantID)
	req.Header.Set("X-Actor-ID", binding.ActorID)
	req.Header.Set("X-Product-Surface", binding.ProductSurface)
	req.Header.Set("X-Axis-Forwarded-By", "bff-v2")

	resp, err := h.client.Do(req)
	if err != nil {
		ginmw.WriteError(c, http.StatusBadGateway, "downstream_unavailable", "assist runtime unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var cr companionAssistResponse
	_ = json.Unmarshal(body, &cr)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(cr.Error)
		if msg == "" {
			msg = "assist run failed"
		}
		c.JSON(http.StatusBadGateway, assistRunResult{ID: cr.ID, Status: "failed", ErrorMessage: msg})
		return
	}

	// Map companion's status to the product contract ("done" -> "completed").
	status := cr.Status
	if status == "done" {
		status = "completed"
	}
	c.JSON(http.StatusOK, assistRunResult{ID: cr.ID, Status: status, Output: cr.Output, ErrorMessage: cr.Error})
}

func (h *Handler) lookup(key string) (Binding, bool) {
	if key == "" {
		return Binding{}, false
	}
	// Constant-time compare against each configured key to avoid leaking which
	// prefix matched via timing.
	for candidate, binding := range h.bindings {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(key)) == 1 {
			return binding, true
		}
	}
	return Binding{}, false
}

func requestAPIKey(c *gin.Context) string {
	if k := strings.TrimSpace(c.GetHeader("X-API-Key")); k != "" {
		return k
	}
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if after, found := strings.CutPrefix(auth, "Bearer "); found {
		return strings.TrimSpace(after)
	}
	return ""
}

// ParseBindings reads BFF_V2_PRODUCT_API_KEYS. Each entry is
// `<apiKey>=<tenant>|<virployee>|<actor>|<product>`; entries are separated by
// newlines or commas. Malformed entries are skipped.
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
		out[key] = Binding{
			TenantID:       strings.TrimSpace(parts[0]),
			VirployeeID:    strings.TrimSpace(parts[1]),
			ActorID:        strings.TrimSpace(parts[2]),
			ProductSurface: strings.TrimSpace(parts[3]),
		}
	}
	return out
}
