package domain

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
)

const StatusActive = "active"

type User struct {
	ID        string
	Email     string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type EnsureInput struct {
	ID    string
	Email string
	Name  string
}

func NormalizeEnsureInput(in EnsureInput) (EnsureInput, error) {
	out := EnsureInput{
		ID:    strings.TrimSpace(in.ID),
		Email: strings.TrimSpace(in.Email),
		Name:  strings.TrimSpace(in.Name),
	}
	if out.ID == "" {
		return EnsureInput{}, domainerr.Validation("principal_id is required")
	}
	if out.Email == "" {
		out.Email = out.ID
	}
	if out.Name == "" {
		out.Name = out.Email
	}
	return out, nil
}
