package runtime

import "testing"

func TestNewProviderVertexDoesNotRequireADCAtStartup(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/tmp/axis-missing-adc.json")

	provider, err := NewProvider(ProviderConfig{
		Provider:      "vertex",
		VertexProject: "axis-ci-smoke",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider == nil {
		t.Fatal("NewProvider() returned nil provider")
	}
}
