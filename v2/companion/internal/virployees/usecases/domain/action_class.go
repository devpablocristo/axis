package domain

import "fmt"

type ActionClass string

const (
	ActionClassObserve     ActionClass = "observe"
	ActionClassRecommend   ActionClass = "recommend"
	ActionClassDraft       ActionClass = "draft"
	ActionClassWriteLow    ActionClass = "write_low"
	ActionClassWriteMedium ActionClass = "write_medium"
	ActionClassWriteHigh   ActionClass = "write_high"
)

type ActionClassDefinition struct {
	Class            ActionClass
	Name             string
	Description      string
	RequiredAutonomy AutonomyLevel
	RequiresApproval bool
	Enabled          bool
}

type AutonomyDecision struct {
	Allowed          bool
	RequiresApproval bool
	Autonomy         AutonomyLevel
	ActionClass      ActionClass
	RequiredAutonomy AutonomyLevel
	Reason           string
}

var actionClassDefinitions = []ActionClassDefinition{
	{
		Class:            ActionClassObserve,
		Name:             "Observe",
		Description:      "Read context and hold conversation without recommending, drafting or executing actions.",
		RequiredAutonomy: AutonomyA0,
		Enabled:          true,
	},
	{
		Class:            ActionClassRecommend,
		Name:             "Recommend",
		Description:      "Analyze context and recommend actions without preparing executable output.",
		RequiredAutonomy: AutonomyA1,
		Enabled:          true,
	},
	{
		Class:            ActionClassDraft,
		Name:             "Draft",
		Description:      "Prepare plans or executable drafts without external side effects.",
		RequiredAutonomy: AutonomyA2,
		Enabled:          true,
	},
	{
		Class:            ActionClassWriteLow,
		Name:             "Low-risk write",
		Description:      "Execute low-risk writes that are reversible, idempotent and scoped to the tenant.",
		RequiredAutonomy: AutonomyA3,
		Enabled:          true,
	},
	{
		Class:            ActionClassWriteMedium,
		Name:             "Medium-risk write",
		Description:      "Attempt medium-risk writes only through approval or a controlled playbook.",
		RequiredAutonomy: AutonomyA4,
		RequiresApproval: true,
		Enabled:          true,
	},
	{
		Class:            ActionClassWriteHigh,
		Name:             "High-risk write",
		Description:      "Reserved for high-risk or broad-impact actions; not enabled by default.",
		RequiredAutonomy: AutonomyA5,
		RequiresApproval: true,
		Enabled:          false,
	},
}

func ActionClassDefinitions() []ActionClassDefinition {
	out := make([]ActionClassDefinition, len(actionClassDefinitions))
	copy(out, actionClassDefinitions)
	return out
}

func ActionClassDefinitionFor(class ActionClass) (ActionClassDefinition, bool) {
	for _, definition := range actionClassDefinitions {
		if definition.Class == class {
			return definition, true
		}
	}
	return ActionClassDefinition{}, false
}

func (level AutonomyLevel) AllowsAction(class ActionClass) bool {
	return EvaluateAutonomy(level, class).Allowed
}

func EvaluateAutonomy(level AutonomyLevel, class ActionClass) AutonomyDecision {
	decision := AutonomyDecision{
		Autonomy:    level,
		ActionClass: class,
	}
	definition, ok := ActionClassDefinitionFor(class)
	if !ok {
		decision.Reason = fmt.Sprintf("action class %q is unknown", class)
		return decision
	}
	decision.RequiredAutonomy = definition.RequiredAutonomy
	decision.RequiresApproval = definition.RequiresApproval
	if !definition.Enabled {
		decision.Reason = fmt.Sprintf("%s is disabled", class)
		return decision
	}
	if !validAutonomy(level) {
		decision.Reason = fmt.Sprintf("autonomy %q is unknown", level)
		return decision
	}
	if !level.Allows(definition.RequiredAutonomy) {
		decision.Reason = fmt.Sprintf("%s does not allow %s; required %s", level, class, definition.RequiredAutonomy)
		return decision
	}
	decision.Allowed = true
	if definition.RequiresApproval {
		decision.Reason = fmt.Sprintf("%s allows %s with approval; required %s", level, class, definition.RequiredAutonomy)
	} else {
		decision.Reason = fmt.Sprintf("%s allows %s; required %s", level, class, definition.RequiredAutonomy)
	}
	return decision
}
