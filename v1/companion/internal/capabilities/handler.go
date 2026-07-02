package capabilities

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
)

const (
	scopeCapabilitiesRead  = "companion:capabilities:read"
	scopeCapabilitiesAdmin = "companion:capabilities:admin"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/capabilities", h.list)
	mux.HandleFunc("POST /v1/capabilities", h.importManifest)
	mux.HandleFunc("POST /v1/capabilities/import-source", h.importManifestSource)
	mux.HandleFunc("POST /v1/capabilities/validate", h.validate)
	mux.HandleFunc("GET /v1/capabilities/conformance-runs", h.listConformanceRuns)
	mux.HandleFunc("POST /v1/capabilities/conformance-runs", h.runConformance)
	mux.HandleFunc("GET /v1/capabilities/{capability_id}", h.getCapability)
	mux.HandleFunc("POST /v1/capabilities/{capability_id}/status", h.setCapabilityStatus)
	mux.HandleFunc("GET /v1/capabilities/{capability_id}/versions", h.versions)
	mux.HandleFunc("POST /v1/capabilities/{capability_id}/versions/{version}/promote", h.promote)
	mux.HandleFunc("POST /v1/capabilities/{capability_id}/versions/{version}/deprecate", h.deprecate)
	mux.HandleFunc("POST /v1/capabilities/{capability_id}/versions/{version}/block", h.block)
	mux.HandleFunc("GET /v1/tools", h.listTools)
	mux.HandleFunc("GET /v1/tools/{tool_id}", h.getTool)
	mux.HandleFunc("POST /v1/tools/{tool_id}/status", h.setToolStatus)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesRead, scopeCapabilitiesAdmin) {
		return
	}
	limit := parseLimit(r, 100)
	records, err := h.uc.ListManifests(r.Context(), ManifestFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  limit,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list capabilities failed")
		return
	}
	capabilities, err := h.uc.ListCapabilities(r.Context(), ManifestFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  limit,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list capabilities failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{
		"capabilities":       records,
		"data":               capabilities,
		"manifest_records":   records,
		"capability_catalog": capabilities,
	})
}

func (h *Handler) getCapability(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesRead, scopeCapabilitiesAdmin) {
		return
	}
	capability, err := h.uc.GetCapability(r.Context(), r.PathValue("capability_id"))
	if err != nil {
		writeManifestError(w, err, "get capability failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, capability)
}

func (h *Handler) setCapabilityStatus(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	capability, err := h.uc.SetCapabilityStatus(r.Context(), r.PathValue("capability_id"), body.Status)
	if err != nil {
		writeManifestError(w, err, "set capability status failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, capability)
}

func (h *Handler) listTools(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesRead, scopeCapabilitiesAdmin) {
		return
	}
	tools, err := h.uc.ListTools(r.Context(), ManifestFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  parseLimit(r, 100),
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list tools failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"tools": tools, "data": tools})
}

func (h *Handler) getTool(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesRead, scopeCapabilitiesAdmin) {
		return
	}
	tool, err := h.uc.GetTool(r.Context(), r.PathValue("tool_id"))
	if err != nil {
		writeManifestError(w, err, "get tool failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tool)
}

func (h *Handler) setToolStatus(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	tool, err := h.uc.SetToolStatus(r.Context(), r.PathValue("tool_id"), body.Status)
	if err != nil {
		writeManifestError(w, err, "set tool status failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tool)
}

func (h *Handler) versions(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesRead, scopeCapabilitiesAdmin) {
		return
	}
	records, err := h.uc.ListManifests(r.Context(), ManifestFilter{
		CapabilityID: strings.TrimSpace(r.PathValue("capability_id")),
		Limit:        parseLimit(r, 100),
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list capability versions failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"versions": records})
}

func (h *Handler) importManifest(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var manifest Manifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	record, err := h.uc.ImportManifest(r.Context(), manifest, identityctx.FromRequest(r).EffectiveActorID())
	if err != nil {
		if errors.Is(err, ErrInvalidManifest) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		httpjson.WriteFlatInternalError(w, err, "import capability failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, record)
}

func (h *Handler) importManifestSource(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var body struct {
		SourceURL      string `json:"source_url"`
		ProductSurface string `json:"product_surface,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	records, err := h.uc.ImportManifestSource(r.Context(), ImportManifestSourceInput{
		SourceURL:              body.SourceURL,
		ExpectedProductSurface: body.ProductSurface,
		ImportedBy:             identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		if errors.Is(err, ErrInvalidManifest) || errors.Is(err, ErrDuplicateManifest) {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		httpjson.WriteFlatInternalError(w, err, "import capability source failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, map[string]any{"capabilities": records})
}

func (h *Handler) validate(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var manifest Manifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	checks, errs := h.uc.CheckConformance(r.Context(), manifest)
	status := ConformanceStatusPassed
	if len(errs) > 0 {
		status = ConformanceStatusFailed
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"checks": checks,
		"errors": errs,
	})
}

func (h *Handler) promote(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	record, err := h.uc.PromoteManifestWithAudit(r.Context(), ManifestStatusChangeInput{
		CapabilityID: r.PathValue("capability_id"),
		Version:      r.PathValue("version"),
		ActorID:      identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		writeManifestError(w, err, "promote capability failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, record)
}

func (h *Handler) deprecate(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	record, err := h.uc.DeprecateManifestWithAudit(r.Context(), ManifestStatusChangeInput{
		CapabilityID: r.PathValue("capability_id"),
		Version:      r.PathValue("version"),
		ActorID:      identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		writeManifestError(w, err, "deprecate capability failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, record)
}

func (h *Handler) block(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	record, err := h.uc.BlockManifestWithAudit(r.Context(), ManifestStatusChangeInput{
		CapabilityID: r.PathValue("capability_id"),
		Version:      r.PathValue("version"),
		ActorID:      identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		writeManifestError(w, err, "block capability failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, record)
}

func (h *Handler) runConformance(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var body struct {
		CapabilityID string    `json:"capability_id"`
		Version      string    `json:"version"`
		Manifest     *Manifest `json:"manifest,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	if body.Manifest == nil && (strings.TrimSpace(body.CapabilityID) == "" || strings.TrimSpace(body.Version) == "") {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "capability_id and version are required")
		return
	}
	run, err := h.uc.RunConformance(r.Context(), ConformanceInput{
		OrgID:        identityctx.PrincipalOrgID(r),
		CapabilityID: body.CapabilityID,
		Version:      body.Version,
		Manifest:     body.Manifest,
		CreatedBy:    identityctx.FromRequest(r).EffectiveActorID(),
	})
	if err != nil {
		writeManifestError(w, err, "run capability conformance failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, run)
}

func (h *Handler) listConformanceRuns(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesRead, scopeCapabilitiesAdmin) {
		return
	}
	runs, err := h.uc.ListConformanceRuns(r.Context(), identityctx.PrincipalOrgID(r), strings.TrimSpace(r.URL.Query().Get("capability_id")), parseLimit(r, 100))
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list capability conformance runs failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func writeManifestError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, ErrManifestNotFound) {
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "capability manifest not found")
		return
	}
	if errors.Is(err, ErrInvalidManifest) {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteFlatInternalError(w, err, fallback)
}

func requireCapabilityScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) || identityctx.HasAnyScope(r, scopes...) {
		return true
	}
	httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing capability scope")
	return false
}

func parseLimit(r *http.Request, fallback int) int {
	limit := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return limit
}
