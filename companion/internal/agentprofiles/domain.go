package agentprofiles

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("agent profile not found")
	ErrValidation = errors.New("agent profile validation failed")
)

type Profile struct {
	ID                  uuid.UUID      `json:"id,omitempty"`
	ProfileID           string         `json:"profile_id"`
	FamilyID            string         `json:"family_id"`
	VersionLabel        string         `json:"version_label"`
	Name                string         `json:"name"`
	Description         string         `json:"description,omitempty"`
	SystemPrompt        string         `json:"system_prompt"`
	MaxAutonomy         string         `json:"max_autonomy"`
	AllowedTools        []string       `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string       `json:"allowed_capabilities,omitempty"`
	MemoryPolicy        map[string]any `json:"memory_policy,omitempty"`
	LLMConfig           map[string]any `json:"llm_config,omitempty"`
	Enabled             bool           `json:"enabled"`
	ArchivedAt          *time.Time     `json:"archived_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at,omitempty"`
	UpdatedAt           time.Time      `json:"updated_at,omitempty"`
}

type Version struct {
	ID                  uuid.UUID      `json:"id,omitempty"`
	AgentProfileID      uuid.UUID      `json:"agent_profile_id,omitempty"`
	ProfileID           string         `json:"profile_id"`
	FamilyID            string         `json:"family_id"`
	VersionLabel        string         `json:"version_label"`
	Name                string         `json:"name"`
	Description         string         `json:"description,omitempty"`
	SystemPrompt        string         `json:"system_prompt"`
	MaxAutonomy         string         `json:"max_autonomy"`
	AllowedTools        []string       `json:"allowed_tools,omitempty"`
	AllowedCapabilities []string       `json:"allowed_capabilities,omitempty"`
	MemoryPolicy        map[string]any `json:"memory_policy,omitempty"`
	LLMConfig           map[string]any `json:"llm_config,omitempty"`
	Enabled             bool           `json:"enabled"`
	ArchivedAt          *time.Time     `json:"archived_at,omitempty"`
	OriginalCreatedAt   time.Time      `json:"original_created_at,omitempty"`
	OriginalUpdatedAt   time.Time      `json:"original_updated_at,omitempty"`
	SavedAt             time.Time      `json:"saved_at,omitempty"`
}

func normalizeProfile(profile Profile) Profile {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.FamilyID = strings.TrimSpace(profile.FamilyID)
	profile.VersionLabel = strings.TrimSpace(profile.VersionLabel)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Description = strings.TrimSpace(profile.Description)
	profile.SystemPrompt = strings.TrimSpace(profile.SystemPrompt)
	profile.MaxAutonomy = strings.TrimSpace(profile.MaxAutonomy)
	if profile.MaxAutonomy == "" {
		profile.MaxAutonomy = "A1"
	}
	if profile.FamilyID == "" || profile.VersionLabel == "" {
		familyID, versionLabel := splitProfileID(profile.ProfileID)
		if profile.FamilyID == "" {
			profile.FamilyID = familyID
		}
		if profile.VersionLabel == "" {
			profile.VersionLabel = versionLabel
		}
	}
	profile.AllowedTools = normalizeList(profile.AllowedTools)
	profile.AllowedCapabilities = normalizeList(profile.AllowedCapabilities)
	if profile.MemoryPolicy == nil {
		profile.MemoryPolicy = map[string]any{}
	}
	if profile.LLMConfig == nil {
		profile.LLMConfig = map[string]any{}
	}
	return profile
}

func validateProfile(profile Profile) error {
	if profile.ProfileID == "" || profile.FamilyID == "" || profile.Name == "" || profile.SystemPrompt == "" {
		return ErrValidation
	}
	switch profile.MaxAutonomy {
	case "A0", "A1", "A2", "A3", "A4", "A5":
	default:
		return ErrValidation
	}
	return nil
}

func splitProfileID(profileID string) (string, string) {
	profileID = strings.TrimSpace(profileID)
	parts := strings.Split(profileID, ".")
	if len(parts) < 2 {
		return profileID, ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if len(last) >= 2 && strings.HasPrefix(strings.ToLower(last), "v") {
		return strings.Join(parts[:len(parts)-1], "."), last
	}
	return profileID, ""
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
