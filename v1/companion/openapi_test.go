package companion_test

import (
	"os"
	"testing"

	"go.yaml.in/yaml/v2"
)

func TestOpenAPIYAMLParses(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.yaml must be valid YAML: %v", err)
	}
	if doc["openapi"] == "" {
		t.Fatal("openapi.yaml missing openapi version")
	}
}
