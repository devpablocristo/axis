package domain

import (
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type State string

const (
	StateActive   State = "active"
	StateArchived State = "archived"
	StateTrashed  State = "trashed"
)

type Virployee struct {
	ID               uuid.UUID
	Name             string
	Role             string
	Description      string
	SupervisorUserID uuid.UUID

	CreatedAt time.Time
	UpdatedAt time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type CreateInput struct {
	Name             string
	Role             string
	Description      string
	SupervisorUserID string
}

type UpdateInput struct {
	Name             string
	Role             string
	Description      string
	SupervisorUserID string
}

type NormalizedCreateInput struct {
	Name             string
	Role             string
	Description      string
	SupervisorUserID uuid.UUID
}

type NormalizedUpdateInput struct {
	Name             string
	Role             string
	Description      string
	SupervisorUserID uuid.UUID
}

func (v Virployee) State() State {
	switch {
	case v.TrashedAt != nil:
		return StateTrashed
	case v.ArchivedAt != nil:
		return StateArchived
	default:
		return StateActive
	}
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	supervisorID, err := parseRequiredUUID(in.SupervisorUserID, "supervisor_user_id")
	if err != nil {
		return NormalizedCreateInput{}, err
	}
	out := NormalizedCreateInput{
		Name:             strings.TrimSpace(in.Name),
		Role:             strings.TrimSpace(in.Role),
		Description:      strings.TrimSpace(in.Description),
		SupervisorUserID: supervisorID,
	}
	if out.Name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	if out.Role == "" {
		return NormalizedCreateInput{}, domainerr.Validation("role is required")
	}
	return out, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	supervisorID, err := parseRequiredUUID(in.SupervisorUserID, "supervisor_user_id")
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	out := NormalizedUpdateInput{
		Name:             strings.TrimSpace(in.Name),
		Role:             strings.TrimSpace(in.Role),
		Description:      strings.TrimSpace(in.Description),
		SupervisorUserID: supervisorID,
	}
	if out.Name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	if out.Role == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("role is required")
	}
	return out, nil
}

func parseRequiredUUID(raw, field string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, domainerr.Validation(field + " is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation(field + " must be a valid UUID")
	}
	return id, nil
}
