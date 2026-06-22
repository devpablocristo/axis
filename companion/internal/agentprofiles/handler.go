package agentprofiles

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
	scopeAgentProfilesRead  = "companion:agent_profiles:read"
	scopeAgentProfilesAdmin = "companion:agent_profiles:admin"
	scopeRuntimeAdmin       = "companion:runtime:admin"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agent-profiles", h.listProfiles)
	mux.HandleFunc("GET /v1/agent-profiles/{profile_id}", h.getProfile)
	mux.HandleFunc("PUT /v1/agent-profiles/{profile_id}", h.putProfile)
	mux.HandleFunc("POST /v1/agent-profiles/{profile_id}/archive", h.archiveProfile)
	mux.HandleFunc("POST /v1/agent-profiles/{profile_id}/trash", h.trashProfile)
	mux.HandleFunc("POST /v1/agent-profiles/{profile_id}/restore", h.restoreProfile)
	mux.HandleFunc("DELETE /v1/agent-profiles/{profile_id}/purge", h.purgeProfile)
	mux.HandleFunc("GET /v1/agent-profiles/{profile_id}/versions", h.listVersions)
}

func (h *Handler) listProfiles(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesRead, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	lifecycle := strings.TrimSpace(r.URL.Query().Get("lifecycle"))
	profiles, err := h.uc.ListProfiles(r.Context(), lifecycle, includeArchived)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list agent profiles failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesRead, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.GetProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) putProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	var body profileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	profile, err := h.uc.UpsertProfile(r.Context(), Profile{
		ProfileID:           strings.TrimSpace(r.PathValue("profile_id")),
		FamilyID:            body.FamilyID,
		VersionLabel:        body.VersionLabel,
		Name:                body.Name,
		Description:         body.Description,
		SystemPrompt:        body.SystemPrompt,
		MaxAutonomy:         body.MaxAutonomy,
		AllowedTools:        body.AllowedTools,
		AllowedCapabilities: body.AllowedCapabilities,
		MemoryPolicy:        body.MemoryPolicy,
		LLMConfig:           body.LLMConfig,
		Enabled:             enabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, profile)
}

type profileRequest struct {
	FamilyID            string         `json:"family_id"`
	VersionLabel        string         `json:"version_label"`
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	SystemPrompt        string         `json:"system_prompt"`
	MaxAutonomy         string         `json:"max_autonomy"`
	AllowedTools        []string       `json:"allowed_tools"`
	AllowedCapabilities []string       `json:"allowed_capabilities"`
	MemoryPolicy        map[string]any `json:"memory_policy"`
	LLMConfig           map[string]any `json:"llm_config"`
	Enabled             *bool          `json:"enabled"`
}

func (h *Handler) archiveProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.ArchiveProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) restoreProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.RestoreProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) trashProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.TrashProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) purgeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	if err := h.uc.PurgeProfile(r.Context(), r.PathValue("profile_id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeAgentProfilesRead, scopeAgentProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid limit")
			return
		}
		limit = parsed
	}
	versions, err := h.uc.ListVersions(r.Context(), r.PathValue("profile_id"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func requireScope(w http.ResponseWriter, r *http.Request, scopes ...string) bool {
	if identityctx.HasNoAuthContext(r) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "agent profile endpoints require authenticated admin context")
		return false
	}
	if !identityctx.HasAnyScope(r, scopes...) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "missing agent profile scope")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "agent profile not found")
	case errors.Is(err, ErrValidation):
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrConflict):
		httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", err.Error())
	default:
		httpjson.WriteFlatInternalError(w, err, "agent profile operation failed")
	}
}
