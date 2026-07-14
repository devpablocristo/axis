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

	// Governance contract (Fase 1). Fail-safe defaults treat an unconfigured
	// capability as maximally governed.
	RiskClass             string // low | medium | high
	SideEffectClass       string // read | write
	RequiresNexusApproval bool
	EvidenceRequired      bool
	RollbackCapabilityKey string // optional capability_key that undoes this one; "" = none

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

// GovernanceInput carries the optional governance-contract fields shared by
// create and update. RequiresNexusApproval is a pointer so an omitted value can
// default fail-safe (approval required) rather than to Go's false zero value.
type GovernanceInput struct {
	RiskClass             string
	SideEffectClass       string
	RequiresNexusApproval *bool
	EvidenceRequired      bool
	RollbackCapabilityKey string
}

type CreateInput struct {
	CapabilityKey    string
	Name             string
	Description      string
	RequiredAutonomy string
	Governance       GovernanceInput
}

type UpdateInput struct {
	Name             string
	Description      string
	RequiredAutonomy string
	Governance       GovernanceInput
}

type NormalizedGovernance struct {
	RiskClass             string
	SideEffectClass       string
	RequiresNexusApproval bool
	EvidenceRequired      bool
	RollbackCapabilityKey string
}

type NormalizedCreateInput struct {
	CapabilityKey    string
	Name             string
	Description      string
	RequiredAutonomy virployeedomain.AutonomyLevel
	Governance       NormalizedGovernance
}

type NormalizedUpdateInput struct {
	Name             string
	Description      string
	RequiredAutonomy virployeedomain.AutonomyLevel
	Governance       NormalizedGovernance
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
	governance, err := normalizeGovernance(in.Governance)
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	return NormalizedCreateInput{
		CapabilityKey:    key,
		Name:             name,
		Description:      description,
		RequiredAutonomy: requiredAutonomy,
		Governance:       governance,
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
	governance, err := normalizeGovernance(in.Governance)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	return NormalizedUpdateInput{
		Name:             name,
		Description:      description,
		RequiredAutonomy: requiredAutonomy,
		Governance:       governance,
	}, nil
}

// normalizeGovernance applies fail-safe defaults: unset risk is high, unset
// side effect is write, and approval is required unless explicitly disabled.
func normalizeGovernance(in GovernanceInput) (NormalizedGovernance, error) {
	riskClass, err := normalizeChoice("risk_class", in.RiskClass, "high", []string{"low", "medium", "high"})
	if err != nil {
		return NormalizedGovernance{}, err
	}
	sideEffectClass, err := normalizeChoice("side_effect_class", in.SideEffectClass, "write", []string{"read", "write"})
	if err != nil {
		return NormalizedGovernance{}, err
	}
	requiresApproval := true
	if in.RequiresNexusApproval != nil {
		requiresApproval = *in.RequiresNexusApproval
	}
	rollback := strings.TrimSpace(in.RollbackCapabilityKey)
	if rollback != "" && !validCapabilityKey(rollback) {
		return NormalizedGovernance{}, domainerr.Validation("rollback_capability_key must use domain.resource.action with lowercase letters only")
	}
	return NormalizedGovernance{
		RiskClass:             riskClass,
		SideEffectClass:       sideEffectClass,
		RequiresNexusApproval: requiresApproval,
		EvidenceRequired:      in.EvidenceRequired,
		RollbackCapabilityKey: rollback,
	}, nil
}

func normalizeChoice(field, raw, def string, allowed []string) (string, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		value = def
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", domainerr.Validation(field + " must be one of " + strings.Join(allowed, ", "))
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
