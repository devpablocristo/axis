package learning

import (
	"strings"
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

func validInput() CreateInput {
	return CreateInput{
		VirployeeID:    uuid.New(),
		CapabilityKey:  "calendar.events.create",
		Title:          "Cómo agendar una reunión",
		Content:        "1. Confirmar título, fecha, hora e invitados. 2. Pasar por el gate.",
		SourceTraceIDs: []string{"trace-1", " ", "trace-2"},
	}
}

func TestNormalizeCreateInputDefaultsAndHashes(t *testing.T) {
	normalized, err := NormalizeCreateInput(validInput())
	if err != nil {
		t.Fatalf("NormalizeCreateInput: %v", err)
	}
	if normalized.ProposedBy != ProposedByAnalyzer {
		t.Fatalf("expected default proposer analyzer, got %q", normalized.ProposedBy)
	}
	if normalized.ContentHash == "" {
		t.Fatal("expected a content hash")
	}
	if len(normalized.SourceTraceIDs) != 2 {
		t.Fatalf("expected blank trace ids dropped, got %+v", normalized.SourceTraceIDs)
	}
	if normalized.Evidence == nil {
		t.Fatal("expected evidence to default to an empty map")
	}
}

func TestNormalizeCreateInputRejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateInput)
	}{
		{"missing virployee", func(in *CreateInput) { in.VirployeeID = uuid.Nil }},
		{"invalid capability key", func(in *CreateInput) { in.CapabilityKey = "not-a-key" }},
		{"empty title", func(in *CreateInput) { in.Title = "  " }},
		{"oversized title", func(in *CreateInput) { in.Title = strings.Repeat("a", 201) }},
		{"empty content", func(in *CreateInput) { in.Content = "" }},
		{"oversized content", func(in *CreateInput) { in.Content = strings.Repeat("a", 20001) }},
		{"unknown proposer", func(in *CreateInput) { in.ProposedBy = "robot" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := validInput()
			tc.mutate(&input)
			if _, err := NormalizeCreateInput(input); !domainerr.IsValidation(err) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestNormalizeStatusFilter(t *testing.T) {
	if status, err := NormalizeStatusFilter(""); err != nil || status != StatusPending {
		t.Fatalf("empty filter must default to pending, got %q %v", status, err)
	}
	if status, err := NormalizeStatusFilter(" Accepted "); err != nil || status != StatusAccepted {
		t.Fatalf("expected accepted, got %q %v", status, err)
	}
	if _, err := NormalizeStatusFilter("expired"); !domainerr.IsValidation(err) {
		t.Fatalf("expected validation error for unknown status, got %v", err)
	}
}
