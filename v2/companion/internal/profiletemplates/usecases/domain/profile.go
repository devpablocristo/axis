package domain

import (
	"strings"
	"time"

	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type State string

const (
	StateActive   State = "active"
	StateArchived State = "archived"
	StateTrashed  State = "trashed"
)

type ProfileTemplate struct {
	ID           uuid.UUID
	OrgID        string
	Name         string
	Description  string
	SystemPrompt string
	MaxAutonomy  virployeedomain.AutonomyLevel

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type CreateInput struct {
	Name         string
	Description  string
	SystemPrompt string
	MaxAutonomy  string
}

type UpdateInput struct {
	Name         string
	Description  string
	SystemPrompt string
	MaxAutonomy  string
}

type NormalizedCreateInput struct {
	Name         string
	Description  string
	SystemPrompt string
	MaxAutonomy  virployeedomain.AutonomyLevel
}

type NormalizedUpdateInput struct {
	Name         string
	Description  string
	SystemPrompt string
	MaxAutonomy  virployeedomain.AutonomyLevel
}

func (p ProfileTemplate) State() State {
	switch {
	case p.TrashedAt != nil:
		return StateTrashed
	case p.ArchivedAt != nil:
		return StateArchived
	default:
		return StateActive
	}
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	systemPrompt := strings.TrimSpace(in.SystemPrompt)
	maxAutonomy, err := normalizeMaxAutonomy(in.MaxAutonomy)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	if name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	if systemPrompt == "" {
		return NormalizedCreateInput{}, domainerr.Validation("system_prompt is required")
	}
	return NormalizedCreateInput{
		Name:         name,
		Description:  description,
		SystemPrompt: systemPrompt,
		MaxAutonomy:  maxAutonomy,
	}, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	systemPrompt := strings.TrimSpace(in.SystemPrompt)
	maxAutonomy, err := normalizeMaxAutonomy(in.MaxAutonomy)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	if name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	if systemPrompt == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("system_prompt is required")
	}
	return NormalizedUpdateInput{
		Name:         name,
		Description:  description,
		SystemPrompt: systemPrompt,
		MaxAutonomy:  maxAutonomy,
	}, nil
}

func normalizeMaxAutonomy(raw string) (virployeedomain.AutonomyLevel, error) {
	value := virployeedomain.AutonomyLevel(strings.TrimSpace(raw))
	if value == "" {
		return "", domainerr.Validation("max_autonomy is required")
	}
	if _, ok := virployeedomain.AutonomyDefinitionFor(value); !ok {
		return "", domainerr.Validation("max_autonomy must be one of A0, A1, A2, A3, A4, A5")
	}
	return value, nil
}
