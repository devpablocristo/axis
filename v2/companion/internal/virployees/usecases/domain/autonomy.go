package domain

import (
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

type AutonomyLevel string

const (
	AutonomyA0 AutonomyLevel = "A0"
	AutonomyA1 AutonomyLevel = "A1"
	AutonomyA2 AutonomyLevel = "A2"
	AutonomyA3 AutonomyLevel = "A3"
	AutonomyA4 AutonomyLevel = "A4"
	AutonomyA5 AutonomyLevel = "A5"
)

type AutonomyDefinition struct {
	Level       AutonomyLevel
	Name        string
	Description string
}

var autonomyDefinitions = []AutonomyDefinition{
	{
		Level:       AutonomyA0,
		Name:        "Conversation",
		Description: "Can hold conversation and read contextual information, without recommending or preparing actions.",
	},
	{
		Level:       AutonomyA1,
		Name:        "Recommendation",
		Description: "Can read, analyze and recommend actions.",
	},
	{
		Level:       AutonomyA2,
		Name:        "Draft",
		Description: "Can prepare plans or executable drafts, without external side effects.",
	},
	{
		Level:       AutonomyA3,
		Name:        "Limited execution",
		Description: "Can execute low-risk writes that are reversible, idempotent and scoped to the tenant.",
	},
	{
		Level:       AutonomyA4,
		Name:        "Governed execution",
		Description: "Can attempt medium-risk actions only with prior approval or a controlled playbook.",
	},
	{
		Level:       AutonomyA5,
		Name:        "Broad autonomy",
		Description: "Reserved for broad multi-product autonomy; not enabled by default.",
	},
}

func AutonomyDefinitions() []AutonomyDefinition {
	out := make([]AutonomyDefinition, len(autonomyDefinitions))
	copy(out, autonomyDefinitions)
	return out
}

func AutonomyDefinitionFor(level AutonomyLevel) (AutonomyDefinition, bool) {
	for _, definition := range autonomyDefinitions {
		if definition.Level == level {
			return definition, true
		}
	}
	return AutonomyDefinition{}, false
}

func (level AutonomyLevel) Rank() (int, bool) {
	for rank, definition := range autonomyDefinitions {
		if definition.Level == level {
			return rank, true
		}
	}
	return 0, false
}

func (level AutonomyLevel) Allows(required AutonomyLevel) bool {
	levelRank, ok := level.Rank()
	if !ok {
		return false
	}
	requiredRank, ok := required.Rank()
	if !ok {
		return false
	}
	return levelRank >= requiredRank
}

func normalizeAutonomy(raw string) (AutonomyLevel, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AutonomyA1, nil
	}
	level := AutonomyLevel(raw)
	if !validAutonomy(level) {
		return "", domainerr.Validation("autonomy must be one of A0, A1, A2, A3, A4, A5")
	}
	return level, nil
}

func validAutonomy(level AutonomyLevel) bool {
	_, ok := AutonomyDefinitionFor(level)
	return ok
}
