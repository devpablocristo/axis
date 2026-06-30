package jobroles

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("job role not found")
	ErrValidation = errors.New("job role validation failed")
	ErrConflict   = errors.New("job role conflict")
)

type LifecycleView string

const (
	LifecycleActive   LifecycleView = "active"
	LifecycleArchived LifecycleView = "archived"
	LifecycleAll      LifecycleView = "all"
)

type Responsibility struct {
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	ExpectedOutcome string `json:"expected_outcome,omitempty"`
	Priority        int    `json:"priority,omitempty"`
}

type SuccessCriterion struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	TargetValue string `json:"target_value,omitempty"`
	Priority    int    `json:"priority,omitempty"`
}

type SuccessCriteria []SuccessCriterion

func (criteria *SuccessCriteria) UnmarshalJSON(data []byte) error {
	var structured []SuccessCriterion
	if err := json.Unmarshal(data, &structured); err == nil {
		*criteria = structured
		return nil
	}
	var legacy []string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	out := make([]SuccessCriterion, 0, len(legacy))
	for _, value := range legacy {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, SuccessCriterion{Title: value})
	}
	*criteria = out
	return nil
}

type JobRole struct {
	ID                        uuid.UUID        `json:"id,omitempty"`
	JobRoleID                 string           `json:"job_role_id"`
	JobRoleKey                string           `json:"job_role_key,omitempty"`
	TenantID                  string           `json:"tenant_id,omitempty"`
	OrgID                     string           `json:"org_id,omitempty"`
	ProductSurface            string           `json:"product_surface,omitempty"`
	Name                      string           `json:"name"`
	Slug                      string           `json:"slug"`
	Description               string           `json:"description,omitempty"`
	Mission                   string           `json:"mission,omitempty"`
	Responsibilities          []Responsibility `json:"responsibilities,omitempty"`
	RecommendedCapabilityIDs  []string         `json:"recommended_capability_ids,omitempty"`
	RecommendedCapabilities   []string         `json:"recommended_capabilities,omitempty"`
	DefaultAutonomy           string           `json:"default_autonomy,omitempty"`
	DefaultAutonomyLevel      string           `json:"default_autonomy_level"`
	DefaultPermissionBundleID string           `json:"default_permission_bundle_id,omitempty"`
	SuccessCriteria           SuccessCriteria  `json:"success_criteria,omitempty"`
	DefaultSLAPolicy          map[string]any   `json:"default_sla_policy,omitempty"`
	DefaultMemoryPolicy       map[string]any   `json:"default_memory_policy,omitempty"`
	Status                    string           `json:"status"`
	Metadata                  map[string]any   `json:"metadata,omitempty"`
	CreatedBy                 string           `json:"created_by,omitempty"`
	CreatedAt                 time.Time        `json:"created_at,omitempty"`
	UpdatedAt                 time.Time        `json:"updated_at,omitempty"`
	ArchivedAt                *time.Time       `json:"archived_at,omitempty"`
	Version                   int64            `json:"version"`
}

type Version struct {
	ID             uuid.UUID `json:"id,omitempty"`
	JobRoleID      string    `json:"job_role_id"`
	TenantID       string    `json:"tenant_id,omitempty"`
	OrgID          string    `json:"org_id"`
	ProductSurface string    `json:"product_surface"`
	Version        int64     `json:"version"`
	Action         string    `json:"action"`
	ChangedBy      string    `json:"changed_by,omitempty"`
	Role           JobRole   `json:"role"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

func normalizeLifecycleView(value string, includeArchived bool) LifecycleView {
	switch LifecycleView(strings.ToLower(strings.TrimSpace(value))) {
	case LifecycleArchived:
		return LifecycleArchived
	case LifecycleAll:
		return LifecycleAll
	case LifecycleActive:
		return LifecycleActive
	default:
		if includeArchived {
			return LifecycleAll
		}
		return LifecycleActive
	}
}

func normalizeJobRole(role JobRole) JobRole {
	role.JobRoleID = strings.TrimSpace(role.JobRoleID)
	role.JobRoleKey = strings.TrimSpace(role.JobRoleKey)
	role.TenantID = strings.TrimSpace(role.TenantID)
	role.OrgID = strings.TrimSpace(role.OrgID)
	role.ProductSurface = strings.TrimSpace(role.ProductSurface)
	if role.ProductSurface == "" {
		role.ProductSurface = "axis-console"
	}
	role.Name = strings.TrimSpace(role.Name)
	role.Slug = normalizeSlug(role.Slug)
	if role.Slug == "" {
		role.Slug = normalizeSlug(role.Name)
	}
	role.Description = strings.TrimSpace(role.Description)
	role.Mission = strings.TrimSpace(role.Mission)
	role.Responsibilities = normalizeResponsibilities(role.Responsibilities)
	role.SuccessCriteria = normalizeSuccessCriteria(role.SuccessCriteria)
	role.RecommendedCapabilityIDs = normalizeList(role.RecommendedCapabilityIDs)
	role.RecommendedCapabilities = normalizeList(role.RecommendedCapabilities)
	role.DefaultAutonomy = strings.TrimSpace(role.DefaultAutonomy)
	role.DefaultAutonomyLevel = strings.TrimSpace(role.DefaultAutonomyLevel)
	if role.DefaultAutonomy == "" && role.DefaultAutonomyLevel != "" {
		role.DefaultAutonomy = role.DefaultAutonomyLevel
	}
	if role.DefaultAutonomyLevel == "" && role.DefaultAutonomy != "" {
		role.DefaultAutonomyLevel = role.DefaultAutonomy
	}
	if role.DefaultAutonomy == "" {
		role.DefaultAutonomy = "A2"
		role.DefaultAutonomyLevel = "A2"
	}
	role.DefaultPermissionBundleID = strings.TrimSpace(role.DefaultPermissionBundleID)
	if role.DefaultSLAPolicy == nil {
		role.DefaultSLAPolicy = map[string]any{}
	}
	if role.DefaultMemoryPolicy == nil {
		role.DefaultMemoryPolicy = map[string]any{}
	}
	role.Status = strings.ToLower(strings.TrimSpace(role.Status))
	if role.Status == "" {
		role.Status = "active"
	}
	if role.Metadata == nil {
		role.Metadata = map[string]any{}
	}
	role.CreatedBy = strings.TrimSpace(role.CreatedBy)
	return role
}

func validateJobRole(role JobRole) error {
	if role.OrgID == "" || role.ProductSurface == "" || role.Name == "" || role.Slug == "" {
		return ErrValidation
	}
	switch role.DefaultAutonomyLevel {
	case "A0", "A1", "A2", "A3", "A4", "A5":
	default:
		return ErrValidation
	}
	switch role.Status {
	case "active", "archived":
	default:
		return ErrValidation
	}
	for _, responsibility := range role.Responsibilities {
		if strings.TrimSpace(responsibility.Title) == "" {
			return ErrValidation
		}
	}
	for _, criterion := range role.SuccessCriteria {
		if strings.TrimSpace(criterion.Title) == "" {
			return ErrValidation
		}
	}
	for _, capabilityID := range role.RecommendedCapabilityIDs {
		if _, err := uuid.Parse(capabilityID); err != nil {
			return ErrValidation
		}
	}
	return nil
}

func normalizeResponsibilities(values []Responsibility) []Responsibility {
	out := make([]Responsibility, 0, len(values))
	for _, value := range values {
		value.Title = strings.TrimSpace(value.Title)
		value.Description = strings.TrimSpace(value.Description)
		value.ExpectedOutcome = strings.TrimSpace(value.ExpectedOutcome)
		if value.Title == "" && value.Description == "" && value.ExpectedOutcome == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func normalizeSuccessCriteria(values SuccessCriteria) SuccessCriteria {
	out := make(SuccessCriteria, 0, len(values))
	for _, value := range values {
		value.Title = strings.TrimSpace(value.Title)
		value.Description = strings.TrimSpace(value.Description)
		value.TargetValue = strings.TrimSpace(value.TargetValue)
		if value.Title == "" && value.Description == "" && value.TargetValue == "" {
			continue
		}
		out = append(out, value)
	}
	return out
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

var slugDisallowed = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugDisallowed.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
