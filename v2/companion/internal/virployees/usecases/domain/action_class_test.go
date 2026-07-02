package domain

import "testing"

func TestActionClassDefinitions(t *testing.T) {
	definitions := ActionClassDefinitions()
	if len(definitions) != 6 {
		t.Fatalf("expected 6 action class definitions, got %d", len(definitions))
	}
	if definitions[0].Class != ActionClassObserve || definitions[len(definitions)-1].Class != ActionClassWriteHigh {
		t.Fatalf("unexpected action class order: %+v", definitions)
	}
	for _, definition := range definitions {
		if definition.Name == "" || definition.Description == "" {
			t.Fatalf("definition must include name and description: %+v", definition)
		}
		if !validAutonomy(definition.RequiredAutonomy) {
			t.Fatalf("definition has invalid required autonomy: %+v", definition)
		}
		if _, ok := ActionClassDefinitionFor(definition.Class); !ok {
			t.Fatalf("definition lookup failed for %s", definition.Class)
		}
	}
}

func TestActionClassDefinitionsReturnsCopy(t *testing.T) {
	definitions := ActionClassDefinitions()
	definitions[0].Name = "changed"

	got, ok := ActionClassDefinitionFor(ActionClassObserve)
	if !ok {
		t.Fatal("expected observe definition")
	}
	if got.Name == "changed" {
		t.Fatal("ActionClassDefinitions must return a copy")
	}
}

func TestActionClassDefinitionForUnknown(t *testing.T) {
	if _, ok := ActionClassDefinitionFor(ActionClass("unknown")); ok {
		t.Fatal("expected unknown action class to be missing")
	}
}

func TestAutonomyAllowsAction(t *testing.T) {
	tests := []struct {
		name  string
		level AutonomyLevel
		class ActionClass
		want  bool
	}{
		{name: "A0 observe", level: AutonomyA0, class: ActionClassObserve, want: true},
		{name: "A0 recommend", level: AutonomyA0, class: ActionClassRecommend, want: false},
		{name: "A1 recommend", level: AutonomyA1, class: ActionClassRecommend, want: true},
		{name: "A1 draft", level: AutonomyA1, class: ActionClassDraft, want: false},
		{name: "A2 draft", level: AutonomyA2, class: ActionClassDraft, want: true},
		{name: "A2 write low", level: AutonomyA2, class: ActionClassWriteLow, want: false},
		{name: "A3 write low", level: AutonomyA3, class: ActionClassWriteLow, want: true},
		{name: "A3 write medium", level: AutonomyA3, class: ActionClassWriteMedium, want: false},
		{name: "A4 write medium", level: AutonomyA4, class: ActionClassWriteMedium, want: true},
		{name: "A5 write high disabled", level: AutonomyA5, class: ActionClassWriteHigh, want: false},
		{name: "unknown action class", level: AutonomyA5, class: ActionClass("unknown"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.level.AllowsAction(tc.class); got != tc.want {
				t.Fatalf("AllowsAction(%s, %s) = %t, want %t", tc.level, tc.class, got, tc.want)
			}
		})
	}
}

func TestActionClassApprovalFlags(t *testing.T) {
	medium, ok := ActionClassDefinitionFor(ActionClassWriteMedium)
	if !ok {
		t.Fatal("expected write_medium definition")
	}
	if !medium.RequiresApproval || !medium.Enabled {
		t.Fatalf("expected write_medium to be enabled and require approval: %+v", medium)
	}

	high, ok := ActionClassDefinitionFor(ActionClassWriteHigh)
	if !ok {
		t.Fatal("expected write_high definition")
	}
	if !high.RequiresApproval || high.Enabled {
		t.Fatalf("expected write_high to require approval and be disabled: %+v", high)
	}
}

func TestEvaluateAutonomyAllowed(t *testing.T) {
	decision := EvaluateAutonomy(AutonomyA3, ActionClassWriteLow)
	if !decision.Allowed {
		t.Fatalf("expected allowed decision: %+v", decision)
	}
	if decision.RequiresApproval {
		t.Fatalf("expected no approval requirement: %+v", decision)
	}
	if decision.Autonomy != AutonomyA3 || decision.ActionClass != ActionClassWriteLow || decision.RequiredAutonomy != AutonomyA3 {
		t.Fatalf("unexpected decision context: %+v", decision)
	}
	if decision.Reason == "" {
		t.Fatal("expected decision reason")
	}
}

func TestEvaluateAutonomyBlockedByLevel(t *testing.T) {
	decision := EvaluateAutonomy(AutonomyA2, ActionClassWriteLow)
	if decision.Allowed {
		t.Fatalf("expected blocked decision: %+v", decision)
	}
	if decision.RequiresApproval {
		t.Fatalf("expected no approval requirement when blocked by autonomy: %+v", decision)
	}
	if decision.RequiredAutonomy != AutonomyA3 {
		t.Fatalf("expected required autonomy A3, got %+v", decision)
	}
	if decision.Reason == "" {
		t.Fatal("expected decision reason")
	}
}

func TestEvaluateAutonomyRequiresApproval(t *testing.T) {
	decision := EvaluateAutonomy(AutonomyA4, ActionClassWriteMedium)
	if !decision.Allowed {
		t.Fatalf("expected allowed decision: %+v", decision)
	}
	if !decision.RequiresApproval {
		t.Fatalf("expected approval requirement: %+v", decision)
	}
	if decision.RequiredAutonomy != AutonomyA4 {
		t.Fatalf("expected required autonomy A4, got %+v", decision)
	}
}

func TestEvaluateAutonomyDisabledActionClass(t *testing.T) {
	decision := EvaluateAutonomy(AutonomyA5, ActionClassWriteHigh)
	if decision.Allowed {
		t.Fatalf("expected disabled action class to be blocked: %+v", decision)
	}
	if !decision.RequiresApproval {
		t.Fatalf("expected disabled high risk class to keep approval metadata: %+v", decision)
	}
	if decision.RequiredAutonomy != AutonomyA5 {
		t.Fatalf("expected required autonomy A5, got %+v", decision)
	}
	if decision.Reason == "" {
		t.Fatal("expected decision reason")
	}
}

func TestEvaluateAutonomyUnknownInputs(t *testing.T) {
	unknownClass := EvaluateAutonomy(AutonomyA5, ActionClass("unknown"))
	if unknownClass.Allowed || unknownClass.RequiredAutonomy != "" || unknownClass.Reason == "" {
		t.Fatalf("expected unknown action class to be blocked with reason: %+v", unknownClass)
	}

	unknownAutonomy := EvaluateAutonomy(AutonomyLevel("A9"), ActionClassObserve)
	if unknownAutonomy.Allowed || unknownAutonomy.RequiredAutonomy != AutonomyA0 || unknownAutonomy.Reason == "" {
		t.Fatalf("expected unknown autonomy to be blocked with context: %+v", unknownAutonomy)
	}
}
