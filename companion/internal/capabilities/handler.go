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
	mux.HandleFunc("POST /v1/capabilities/validate", h.validate)
	mux.HandleFunc("GET /v1/capabilities/conformance-runs", h.listConformanceRuns)
	mux.HandleFunc("POST /v1/capabilities/conformance-runs", h.runConformance)
	mux.HandleFunc("GET /v1/capabilities/{capability_id}/versions", h.versions)
	mux.HandleFunc("POST /v1/capabilities/{capability_id}/versions/{version}/promote", h.promote)
	mux.HandleFunc("POST /v1/capabilities/{capability_id}/versions/{version}/deprecate", h.deprecate)
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
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"capabilities": records})
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

func (h *Handler) validate(w http.ResponseWriter, r *http.Request) {
	if !requireCapabilityScope(w, r, scopeCapabilitiesAdmin) {
		return
	}
	var manifest Manifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	checks, errs := CheckManifestConformance(manifest)
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
	record, err := h.uc.PromoteManifest(r.Context(), r.PathValue("capability_id"), r.PathValue("version"))
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
	record, err := h.uc.DeprecateManifest(r.Context(), r.PathValue("capability_id"), r.PathValue("version"))
	if err != nil {
		writeManifestError(w, err, "deprecate capability failed")
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
