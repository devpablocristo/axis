package domain

import (
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

func TestNormalizeEnsureInput(t *testing.T) {
	got, err := NormalizeEnsureInput(EnsureInput{ID: " user-a ", Email: " ", Name: ""})
	if err != nil {
		t.Fatalf("NormalizeEnsureInput: %v", err)
	}
	if got.ID != "user-a" || got.Email != "user-a" || got.Name != "user-a" {
		t.Fatalf("unexpected normalized input: %+v", got)
	}
}

func TestNormalizeEnsureInputRequiresID(t *testing.T) {
	if _, err := NormalizeEnsureInput(EnsureInput{}); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation, got %v", err)
	}
}
