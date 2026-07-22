package extractor

import (
	"context"
	"testing"
)

func TestOSRunnerRejectsExecutablesOutsideAllowlist(t *testing.T) {
	if _, err := (OSRunner{}).Run(context.Background(), t.TempDir(), "sh", "-c", "true"); err != ErrUnsupported {
		t.Fatalf("expected dynamic command to be rejected, got %v", err)
	}
}
