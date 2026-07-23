package extractor

import (
	"context"
	"testing"
)

type profileStub struct {
	profile string
	parts   []Part
}

func (stub *profileStub) Extract(_ context.Context, _, _, profile string) ([]Part, error) {
	stub.profile = profile
	return stub.parts, nil
}

func TestServiceOwnsValidationAndManifestAttribution(t *testing.T) {
	profiles := &profileStub{parts: []Part{{Kind: "text", Text: "result"}}}
	service := NewService(profiles)
	parts, err := service.Extract(context.Background(), t.TempDir(), "artifact.bin", Metadata{
		Profile: "neutral",
		Manifest: Manifest{
			DocumentID: "document-1",
			Name:       "artifact.bin",
			SHA256:     "abc",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if profiles.profile != "neutral" || len(parts) != 1 {
		t.Fatalf("unexpected profile dispatch: %q %+v", profiles.profile, parts)
	}
	if parts[0].DocumentID != "document-1" || parts[0].SHA256 != "abc" || parts[0].Name != "artifact.bin" {
		t.Fatalf("application attribution missing: %+v", parts[0])
	}
}

func TestServiceFailsClosedWithoutProfileAdapter(t *testing.T) {
	_, err := NewService(nil).Extract(context.Background(), t.TempDir(), "artifact.bin", Metadata{Profile: "neutral"})
	if err != ErrInvalidRequest {
		t.Fatalf("expected invalid request without adapter, got %v", err)
	}
}
