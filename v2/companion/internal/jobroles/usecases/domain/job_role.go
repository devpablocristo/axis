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
	ID       uuid.UUID
	TenantID string
	Name     string
	Slug     string
	Mission  string

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type CreateInput struct {
	Name    string
	Slug    string
	Mission string
}

type UpdateInput struct {
	Name    string
	Slug    string
	Mission string
}

type NormalizedCreateInput struct {
	Name    string
	Slug    string
	Mission string
}

type NormalizedUpdateInput struct {
	Name    string
	Slug    string
	Mission string
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
	out := NormalizedCreateInput{
		Name:    name,
		Slug:    slug,
		Mission: strings.TrimSpace(in.Mission),
	}
	if out.Name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	if out.Slug == "" {
		return NormalizedCreateInput{}, domainerr.Validation("slug is required")
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
