package processrunner

import (
	"context"
	"testing"

	"github.com/devpablocristo/artifact-worker-v2/internal/extractor"
)

func TestOSRunnerRejectsExecutablesOutsideAllowlist(t *testing.T) {
	if _, err := (Adapter{}).Run(context.Background(), t.TempDir(), "sh", "-c", "true"); err != extractor.ErrUnsupported {
		t.Fatalf("expected dynamic command to be rejected, got %v", err)
	}
}
