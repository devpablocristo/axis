package domain

import (
	"regexp"
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

type Capability struct {
	ID               uuid.UUID
	TenantID         string
	CapabilityKey    string
	Name             string
	Description      string
	RequiredAutonomy virployeedomain.AutonomyLevel

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type CreateInput struct {
	CapabilityKey    string
	Name             string
	Description      string
	RequiredAutonomy string
}

type UpdateInput struct {
	Name             string
	Description      string
	RequiredAutonomy string
}

type NormalizedCreateInput struct {
	CapabilityKey    string
	Name             string
	Description      string
	RequiredAutonomy virployeedomain.AutonomyLevel
}

type NormalizedUpdateInput struct {
	Name             string
	Description      string
	RequiredAutonomy virployeedomain.AutonomyLevel
}

func (c Capability) State() State {
	switch {
	case c.TrashedAt != nil:
		return StateTrashed
	case c.ArchivedAt != nil:
		return StateArchived
	default:
		return StateActive
	}
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	key := strings.TrimSpace(in.CapabilityKey)
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	requiredAutonomy, err := normalizeRequiredAutonomy(in.RequiredAutonomy)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	if !validCapabilityKey(key) {
		return NormalizedCreateInput{}, domainerr.Validation("capability_key must use domain.resource.action with lowercase letters only")
	}
	if name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	return NormalizedCreateInput{
		CapabilityKey:    key,
		Name:             name,
		Description:      description,
		RequiredAutonomy: requiredAutonomy,
	}, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	requiredAutonomy, err := normalizeRequiredAutonomy(in.RequiredAutonomy)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	if name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	return NormalizedUpdateInput{
		Name:             name,
		Description:      description,
		RequiredAutonomy: requiredAutonomy,
	}, nil
}

var capabilityKeyPattern = regexp.MustCompile(`^[a-zñ]+\.[a-zñ]+\.[a-zñ]+$`)

func validCapabilityKey(value string) bool {
	return capabilityKeyPattern.MatchString(value)
}

func normalizeRequiredAutonomy(raw string) (virployeedomain.AutonomyLevel, error) {
	value := virployeedomain.AutonomyLevel(strings.TrimSpace(raw))
	if value == "" {
		return "", domainerr.Validation("required_autonomy is required")
	}
	_, ok := virployeedomain.AutonomyDefinitionFor(value)
	if !ok {
		return "", domainerr.Validation("required_autonomy must be one of A0, A1, A2, A3, A4, A5")
	}
	return value, nil
}
