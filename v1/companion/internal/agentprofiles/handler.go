package agentprofiles

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion/internal/identityctx"
	"github.com/devpablocristo/platform/http/go/httpjson"
	"github.com/google/uuid"
)

const (
	scopeVirployeeProfilesRead  = "companion:virployee_profiles:read"
	scopeVirployeeProfilesAdmin = "companion:virployee_profiles:admin"
	scopeRuntimeAdmin           = "companion:runtime:admin"
)

type Handler struct {
	uc *Usecases
}

func NewHandler(uc *Usecases) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/virployee-profiles", h.listVirployeeProfiles)
	mux.HandleFunc("POST /v1/virployee-profiles", h.postVirployeeProfile)
	mux.HandleFunc("GET /v1/virployee-profiles/{profile_id}", h.getVirployeeProfile)
	mux.HandleFunc("PATCH /v1/virployee-profiles/{profile_id}", h.patchVirployeeProfile)
	mux.HandleFunc("POST /v1/virployee-profiles/{profile_id}/status", h.setVirployeeProfileStatus)
	mux.HandleFunc("POST /v1/virployee-profiles/{profile_id}/archive", h.archiveVirployeeProfile)
	mux.HandleFunc("POST /v1/virployee-profiles/{profile_id}/trash", h.trashVirployeeProfile)
	mux.HandleFunc("POST /v1/virployee-profiles/{profile_id}/restore", h.restoreVirployeeProfile)
	mux.HandleFunc("DELETE /v1/virployee-profiles/{profile_id}/purge", h.purgeVirployeeProfile)
	mux.HandleFunc("GET /v1/virployee-profiles/{profile_id}/versions", h.listVirployeeProfileVersions)
}

func (h *Handler) listVirployeeProfiles(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesRead, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	lifecycle := strings.TrimSpace(r.URL.Query().Get("lifecycle"))
	profiles, err := h.uc.ListProfiles(r.Context(), lifecycle, includeArchived)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list virployee profiles failed")
		return
	}
	out := make([]VirployeeProfile, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, virployeeProfileFromProfile(profile))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"virployee_profiles": out, "profiles": out})
}

func (h *Handler) getVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesRead, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.GetProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, virployeeProfileFromProfile(profile))
}

func (h *Handler) postVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	var body virployeeProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	profile, err := h.uc.UpsertProfile(r.Context(), virployeeProfileRequestToProfile(body, Profile{}))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, virployeeProfileFromProfile(profile))
}

func (h *Handler) patchVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	current, err := h.uc.GetProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	var body virployeeProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	profile, err := h.uc.UpsertProfile(r.Context(), virployeeProfileRequestToProfile(body, current))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, virployeeProfileFromProfile(profile))
}

func (h *Handler) setVirployeeProfileStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json body")
		return
	}
	switch strings.ToLower(strings.TrimSpace(body.Status)) {
	case "active":
		h.restoreVirployeeProfile(w, r)
	case "archived":
		h.archiveVirployeeProfile(w, r)
	case "trashed", "trash":
		h.trashVirployeeProfile(w, r)
	default:
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid virployee profile status")
	}
}

func (h *Handler) archiveVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.ArchiveProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, virployeeProfileFromProfile(profile))
}

func (h *Handler) trashVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.TrashProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, virployeeProfileFromProfile(profile))
}

func (h *Handler) restoreVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	profile, err := h.uc.RestoreProfile(r.Context(), r.PathValue("profile_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, virployeeProfileFromProfile(profile))
}

func (h *Handler) purgeVirployeeProfile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
		return
	}
	if err := h.uc.PurgeProfile(r.Context(), r.PathValue("profile_id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listVirployeeProfileVersions(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeVirployeeProfilesRead, scopeVirployeeProfilesAdmin, scopeRuntimeAdmin) {
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

type virployeeProfileRequest struct {
	ProfileID            string         `json:"profile_id"`
	ProfileKey           string         `json:"profile_key"`
	FamilyID             string         `json:"family_id"`
	VersionLabel         string         `json:"version_label"`
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	SystemPrompt         string         `json:"system_prompt"`
	MaxAutonomy          string         `json:"max_autonomy"`
	DefaultCapabilityIDs []string       `json:"default_capability_ids"`
	AllowedCapabilities  []string       `json:"allowed_capabilities"`
	AllowedTools         []string       `json:"allowed_tools"`
	MemoryPolicy         map[string]any `json:"memory_policy"`
	LLMConfig            map[string]any `json:"llm_config"`
	Enabled              *bool          `json:"enabled"`
}

func virployeeProfileRequestToProfile(body virployeeProfileRequest, current Profile) Profile {
	profile := current
	if profile.ProfileID == "" {
		profile.ProfileID = virployeeProfileKey(body)
	}
	if body.ProfileKey != "" {
		profile.ProfileID = strings.TrimSpace(body.ProfileKey)
	} else if body.ProfileID != "" {
		if _, err := uuid.Parse(strings.TrimSpace(body.ProfileID)); err != nil {
			profile.ProfileID = strings.TrimSpace(body.ProfileID)
		}
	}
	if body.Name != "" {
		profile.Name = body.Name
	}
	if body.FamilyID != "" {
		profile.FamilyID = body.FamilyID
	}
	if profile.FamilyID == "" {
		profile.FamilyID = familyIDFromProfileKey(profile.ProfileID)
	}
	if body.VersionLabel != "" {
		profile.VersionLabel = body.VersionLabel
	}
	if profile.VersionLabel == "" {
		profile.VersionLabel = "v1"
	}
	profile.Description = body.Description
	if body.SystemPrompt != "" {
		profile.SystemPrompt = body.SystemPrompt
	}
	if body.MaxAutonomy != "" {
		profile.MaxAutonomy = body.MaxAutonomy
	}
	if body.AllowedTools != nil {
		profile.AllowedTools = body.AllowedTools
	}
	if body.DefaultCapabilityIDs != nil {
		profile.AllowedCapabilities = body.DefaultCapabilityIDs
	} else if body.AllowedCapabilities != nil {
		profile.AllowedCapabilities = body.AllowedCapabilities
	}
	if body.MemoryPolicy != nil {
		profile.MemoryPolicy = body.MemoryPolicy
	}
	if body.LLMConfig != nil {
		profile.LLMConfig = body.LLMConfig
	}
	profile.Enabled = true
	if current.ProfileID != "" {
		profile.Enabled = current.Enabled
	}
	if body.Enabled != nil {
		profile.Enabled = *body.Enabled
	}
	return profile
}

func virployeeProfileKey(body virployeeProfileRequest) string {
	for _, value := range []string{body.ProfileKey, body.ProfileID} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := uuid.Parse(value); err == nil {
			continue
		}
		return value
	}
	familyID := strings.TrimSpace(body.FamilyID)
	if familyID == "" {
		familyID = "employee." + keySegment(body.Name)
	}
	version := strings.TrimSpace(body.VersionLabel)
	if version == "" {
		version = "v1"
	}
	return strings.Trim(familyID+"."+version, ".")
}

func familyIDFromProfileKey(profileKey string) string {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return ""
	}
	index := strings.LastIndex(profileKey, ".")
	if index <= 0 {
		return profileKey
	}
	return profileKey[:index]
}

func keySegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDot := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			builder.WriteRune(r)
			lastDot = false
			continue
		}
		if !lastDot {
			builder.WriteByte('.')
			lastDot = true
		}
	}
	out := strings.Trim(builder.String(), ".")
	if out == "" {
		return "profile"
	}
	return out
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
