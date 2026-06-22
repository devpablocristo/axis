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
	ErrConflict   = errors.New("agent profile conflict")
)

type LifecycleView string

const (
	LifecycleActive   LifecycleView = "active"
	LifecycleArchived LifecycleView = "archived"
	LifecycleTrash    LifecycleView = "trash"
	LifecycleAll      LifecycleView = "all"
	LifecycleNonTrash LifecycleView = "non_trash"
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
	TrashedAt           *time.Time     `json:"trashed_at,omitempty"`
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
	TrashedAt           *time.Time     `json:"trashed_at,omitempty"`
	OriginalCreatedAt   time.Time      `json:"original_created_at,omitempty"`
	OriginalUpdatedAt   time.Time      `json:"original_updated_at,omitempty"`
	SavedAt             time.Time      `json:"saved_at,omitempty"`
}

func normalizeLifecycleView(value string, includeArchived bool) LifecycleView {
	switch LifecycleView(strings.ToLower(strings.TrimSpace(value))) {
	case LifecycleArchived:
		return LifecycleArchived
	case LifecycleTrash:
		return LifecycleTrash
	case LifecycleAll:
		return LifecycleAll
	case LifecycleActive:
		return LifecycleActive
	default:
		if includeArchived {
			return LifecycleNonTrash
		}
		return LifecycleActive
	}
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
	if profile.ProfileID == "" || profile.FamilyID == "" || profile.VersionLabel == "" || profile.Name == "" || profile.SystemPrompt == "" {
		return ErrValidation
	}
	switch profile.MaxAutonomy {
	case "A0", "A1", "A2", "A3", "A4", "A5":
	default:
		return ErrValidation
	}
	return nil
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
