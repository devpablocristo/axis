package domain

import "testing"

func TestAutonomyDefinitions(t *testing.T) {
	definitions := AutonomyDefinitions()
	if len(definitions) != 6 {
		t.Fatalf("expected 6 autonomy definitions, got %d", len(definitions))
	}
	if definitions[0].Level != AutonomyA0 || definitions[len(definitions)-1].Level != AutonomyA5 {
		t.Fatalf("unexpected autonomy order: %+v", definitions)
	}
	for _, definition := range definitions {
		if definition.Name == "" || definition.Description == "" {
			t.Fatalf("definition must include name and description: %+v", definition)
		}
		if _, ok := AutonomyDefinitionFor(definition.Level); !ok {
			t.Fatalf("definition lookup failed for %s", definition.Level)
		}
	}
}

func TestAutonomyDefinitionsReturnsCopy(t *testing.T) {
	definitions := AutonomyDefinitions()
	definitions[0].Name = "changed"

	got, ok := AutonomyDefinitionFor(AutonomyA0)
	if !ok {
		t.Fatal("expected A0 definition")
	}
	if got.Name == "changed" {
		t.Fatal("AutonomyDefinitions must return a copy")
	}
}

func TestAutonomyDefinitionForUnknown(t *testing.T) {
	if _, ok := AutonomyDefinitionFor(AutonomyLevel("A9")); ok {
		t.Fatal("expected unknown autonomy level to be missing")
	}
}

func TestAutonomyAllowsIsCumulative(t *testing.T) {
	if !AutonomyA3.Allows(AutonomyA0) {
		t.Fatal("expected A3 to allow A0")
	}
	if !AutonomyA3.Allows(AutonomyA1) {
		t.Fatal("expected A3 to allow A1")
	}
	if !AutonomyA3.Allows(AutonomyA2) {
		t.Fatal("expected A3 to allow A2")
	}
	if !AutonomyA3.Allows(AutonomyA3) {
		t.Fatal("expected A3 to allow A3")
	}
	if AutonomyA3.Allows(AutonomyA4) {
		t.Fatal("expected A3 not to allow A4")
	}
}

func TestAutonomyAllowsRejectsUnknownLevels(t *testing.T) {
	if AutonomyLevel("A9").Allows(AutonomyA1) {
		t.Fatal("expected unknown autonomy level not to allow known level")
	}
	if AutonomyA1.Allows(AutonomyLevel("A9")) {
		t.Fatal("expected known autonomy level not to allow unknown level")
	}
}

func TestAutonomyRank(t *testing.T) {
	rank, ok := AutonomyA2.Rank()
	if !ok {
		t.Fatal("expected A2 rank")
	}
	if rank != 2 {
		t.Fatalf("expected A2 rank 2, got %d", rank)
	}
}
