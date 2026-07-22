package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type State string

const (
	StateActive   State = "active"
	StateArchived State = "archived"
	StateTrashed  State = "trashed"
)

type JobRole struct {
	ID               uuid.UUID
	OrgID            string
	Name             string
	Slug             string
	Mission          string
	Responsibilities []Responsibility
	SuccessCriteria  []SuccessCriterion

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type Responsibility struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expected_outcome"`
	Priority        int    `json:"priority"`
}

type SuccessCriterion struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TargetValue string `json:"target_value"`
	Priority    int    `json:"priority"`
}

type CreateInput struct {
	Name             string
	Slug             string
	Mission          string
	Responsibilities []Responsibility
	SuccessCriteria  []SuccessCriterion
}

type UpdateInput struct {
	Name             string
	Slug             string
	Mission          string
	Responsibilities []Responsibility
	SuccessCriteria  []SuccessCriterion
}

type NormalizedCreateInput struct {
	Name             string
	Slug             string
	Mission          string
	Responsibilities []Responsibility
	SuccessCriteria  []SuccessCriterion
}

type NormalizedUpdateInput struct {
	Name             string
	Slug             string
	Mission          string
	Responsibilities []Responsibility
	SuccessCriteria  []SuccessCriterion
}

func (r JobRole) State() State {
	switch {
	case r.TrashedAt != nil:
		return StateTrashed
	case r.ArchivedAt != nil:
		return StateArchived
	default:
		return StateActive
	}
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	name := strings.TrimSpace(in.Name)
	rawSlug := strings.TrimSpace(in.Slug)
	slug := NormalizeSlug(rawSlug)
	if rawSlug == "" {
		slug = NormalizeSlug(name)
	}
	responsibilities, err := normalizeResponsibilities(in.Responsibilities)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	successCriteria, err := normalizeSuccessCriteria(in.SuccessCriteria)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	out := NormalizedCreateInput{
		Name:             name,
		Slug:             slug,
		Mission:          strings.TrimSpace(in.Mission),
		Responsibilities: responsibilities,
		SuccessCriteria:  successCriteria,
	}
	if out.Name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	if out.Slug == "" {
		return NormalizedCreateInput{}, domainerr.Validation("slug is required")
	}
	return out, nil
}

func normalizeResponsibilities(items []Responsibility) ([]Responsibility, error) {
	out := make([]Responsibility, 0, len(items))
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Description = strings.TrimSpace(item.Description)
		item.ExpectedOutcome = strings.TrimSpace(item.ExpectedOutcome)
		if item.Title == "" {
			return nil, domainerr.Validation("responsibilities[].title is required")
		}
		if item.Priority < 0 {
			return nil, domainerr.Validation("responsibilities[].priority must be non-negative")
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeSuccessCriteria(items []SuccessCriterion) ([]SuccessCriterion, error) {
	out := make([]SuccessCriterion, 0, len(items))
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Description = strings.TrimSpace(item.Description)
		item.TargetValue = strings.TrimSpace(item.TargetValue)
		if item.Title == "" {
			return nil, domainerr.Validation("success_criteria[].title is required")
		}
		if item.Priority < 0 {
			return nil, domainerr.Validation("success_criteria[].priority must be non-negative")
		}
		out = append(out, item)
	}
	return out, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	normalized, err := NormalizeCreateInput(CreateInput(in))
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	return NormalizedUpdateInput(normalized), nil
}

var slugDisallowed = regexp.MustCompile(`[^a-z0-9]+`)

func NormalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugDisallowed.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}
